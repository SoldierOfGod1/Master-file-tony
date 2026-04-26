package chat

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/SoldierOfGod1/command-centre/internal/event"
)

// AgentSlug identifies one of the curated agent personas exposed via Discord.
// Empty string means "default orchestrator" (no system-prompt override).
type AgentSlug string

const (
	AgentOrchestrator AgentSlug = "orchestrator"
	AgentBackend      AgentSlug = "backend"
	AgentFrontend     AgentSlug = "frontend"
	AgentSecurity     AgentSlug = "security"
	AgentAIML         AgentSlug = "ai"
)

// agentMarkdownFile maps an AgentSlug to the file under `~/.claude/agents/`
// whose contents are passed via --append-system-prompt. The orchestrator has
// no override (default behaviour).
var agentMarkdownFile = map[AgentSlug]string{
	AgentBackend:  "agent-04-universal-backend.md",
	AgentFrontend: "agent-07-universal-frontend.md",
	AgentSecurity: "agent-03-quality-security.md",
	AgentAIML:     "agent-10-ai-ml-platform.md",
}

// ExecuteRequest describes a prompt to send to the Claude CLI.
type ExecuteRequest struct {
	ConversationID string
	Prompt         string
	ProjectDir     string
	HasPIN         bool      // if true, full tool access; if false, read-only
	Agent          AgentSlug // optional persona; empty = orchestrator default

	// UserID identifies the requesting user. Phase B1 of the
	// agent-orchestrator plan. Sources:
	//   - web UI: cookie/header (or 'anonymous' for now)
	//   - Discord bot: discord user_id
	//   - empty string = anonymous; the dispatcher's Write-tool
	//     gate refuses write operations from anonymous requests
	//     so safety doesn't depend on B1 fully landing first.
	UserID string
}

// StreamEvent is published on the event bus while the CLI is running.
type StreamEvent struct {
	ConversationID string         `json:"conversationId"`
	Type           string         `json:"type"`    // "stream", "complete", "error"
	Content        string         `json:"content"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

// Executor spawns `claude -p` as a subprocess and streams its output.
// The optional db handle is used for recording token usage into
// cost_records after each successful run — wired through NewExecutorWithDB.
type Executor struct {
	log *slog.Logger
	bus *event.Bus
	db  *sql.DB
}

// NewExecutor creates an Executor wired to the given logger and event bus.
// Usage-recording is disabled (no DB handle). For cost tracking use
// NewExecutorWithDB instead.
func NewExecutor(log *slog.Logger, bus *event.Bus) *Executor {
	return &Executor{log: log, bus: bus}
}

// NewExecutorWithDB is the preferred constructor when usage tracking is
// wanted (the dashboard's KPI + cost tabs pull from the cost_records table
// this Executor writes to).
func NewExecutorWithDB(log *slog.Logger, bus *event.Bus, db *sql.DB) *Executor {
	return &Executor{log: log, bus: bus, db: db}
}

// Execute runs the Claude CLI with the provided request and returns the full
// response text. It publishes StreamEvent messages on the event bus as output
// lines arrive.
func (e *Executor) Execute(req ExecuteRequest) (string, error) {
	return e.ExecuteContext(context.Background(), req)
}

// ExecuteContext is the context-aware variant the queue manager uses so it
// can kill an in-flight CLI process via the Loop Operator panel.
func (e *Executor) ExecuteContext(ctx context.Context, req ExecuteRequest) (string, error) {
	args := []string{"-p", req.Prompt, "--output-format", "text"}
	if !req.HasPIN {
		args = append(args, "--allowedTools", "Read,Glob,Grep,WebSearch,WebFetch")
	}

	// Persona routing: if the request targets a specific agent, locate the
	// agent markdown under the user's ~/.claude/agents directory and append
	// it as a system prompt so Claude adopts that role.
	if file, ok := agentMarkdownFile[req.Agent]; ok {
		if home, err := os.UserHomeDir(); err == nil {
			p := filepath.Join(home, ".claude", "agents", file)
			if data, err := os.ReadFile(p); err == nil {
				args = append(args, "--append-system-prompt", string(data))
			} else {
				e.log.Warn("agent file not readable — falling back to orchestrator",
					"agent", req.Agent, "path", p, "error", err)
			}
		}
	}

	// #7 — per-persona worktree isolation. Orchestrator (empty agent)
	// keeps running against the main working tree; every other persona
	// gets its own `.worktrees/<agent>` so concurrent /ask runs don't
	// fight over the same filesystem. Best-effort: falls back to
	// req.ProjectDir if the repo can't host a worktree.
	runDir := resolveWorktree(ctx, e.log, req.ProjectDir, req.Agent)

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = runDir

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	e.log.Info("executing claude CLI",
		"conversationId", req.ConversationID,
		"projectDir", req.ProjectDir,
		"runDir", runDir,
		"hasPIN", req.HasPIN,
		"agent", string(req.Agent),
	)

	if err := cmd.Start(); err != nil {
		errEvt := StreamEvent{
			ConversationID: req.ConversationID,
			Type:           "error",
			Content:        fmt.Sprintf("failed to start claude: %v", err),
		}
		e.bus.PublishJSON("chat.error", errEvt)
		return "", fmt.Errorf("failed to start claude: %w", err)
	}

	// Stream stdout line by line.
	var fullOutput strings.Builder
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		fullOutput.WriteString(line)
		fullOutput.WriteString("\n")

		evt := StreamEvent{
			ConversationID: req.ConversationID,
			Type:           "stream",
			Content:        line,
		}
		e.bus.PublishJSON("chat.stream", evt)
	}

	// Collect stderr for diagnostics.
	var stderrBuf strings.Builder
	stderrScanner := bufio.NewScanner(stderr)
	for stderrScanner.Scan() {
		stderrBuf.WriteString(stderrScanner.Text())
		stderrBuf.WriteString("\n")
	}

	if err := cmd.Wait(); err != nil {
		errContent := strings.TrimSpace(stderrBuf.String())
		if errContent == "" {
			errContent = err.Error()
		}
		errEvt := StreamEvent{
			ConversationID: req.ConversationID,
			Type:           "error",
			Content:        errContent,
		}
		e.bus.PublishJSON("chat.error", errEvt)
		return fullOutput.String(), fmt.Errorf("claude process exited with error: %w", err)
	}

	response := strings.TrimSpace(fullOutput.String())

	// ---- Usage tracking (#1 session cost tracker) ----
	// Scan both stdout + stderr for a usage marker. Newer Claude CLI
	// versions emit these to stderr; older ones sometimes leak them into
	// stdout. Either way, write one row to cost_records so the Dashboard
	// KPI tiles light up without needing a separate scrape loop.
	if e.db != nil {
		combined := response + "\n" + stderrBuf.String()
		if inTok, outTok, ok := ParseUsage(combined); ok {
			model := detectModelHint(combined)
			RecordUsage(e.db, e.log, UsageRecord{
				ConversationID: req.ConversationID,
				Model:          model,
				InputTokens:    inTok,
				OutputTokens:   outTok,
				AmountZAR:      EstimateCostZAR(model, inTok, outTok),
			})
		}
	}

	completeEvt := StreamEvent{
		ConversationID: req.ConversationID,
		Type:           "complete",
		Content:        response,
	}
	e.bus.PublishJSON("chat.complete", completeEvt)

	e.log.Info("claude CLI completed",
		"conversationId", req.ConversationID,
		"responseLen", len(response),
	)

	return response, nil
}
