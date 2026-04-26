package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/SoldierOfGod1/command-centre/internal/event"
)

// Dispatcher is the Phase A3 hybrid executor: it routes a chat
// prompt either through the existing CLI Executor (for code
// tasks, unclear prompts, or when no API key is set) or through
// the new AgentClient tool-use loop (for ops-type prompts where
// the answer is composable from existing /api/v1/* endpoints).
//
// Wire diagram:
//
//   prompt ─▶ ClassifyIntent
//                │
//                ├─ code_task | unclear (low confidence)
//                │     └─▶ Executor (claude -p subprocess)
//                │
//                └─ customer_lookup | system_status | data_query
//                    │
//                    ├─ API key present
//                    │    └─▶ AgentClient.Run → tool catalogue
//                    │
//                    └─ no API key
//                         └─▶ Executor (CLI fallback)
//
// Both paths publish StreamEvents to the same event bus topic
// (chat.stream / chat.complete / chat.error) so the existing
// frontend WebSocket consumer renders them identically.
type Dispatcher struct {
	log *slog.Logger
	bus *event.Bus

	cli       *Executor
	agent     *AgentClient
	catalogue *ToolCatalogue
	budget    *BudgetGate // Phase B3: per-user weekly cap
	memDB     *sql.DB     // Phase D1: per-user agent_memory store

	// AgentEnabledIntents is the set of intents the API path
	// handles. Anything else (including unclear prompts) goes
	// to CLI. Default: customer_lookup, system_status, data_query.
	AgentEnabledIntents map[Intent]bool
	// MinAgentConfidence: below this, fall through to CLI even
	// when the intent is in the enabled set.
	MinAgentConfidence float64
}

// DispatcherConfig wires the existing executor (CLI path) plus
// the agent loop (API path). API key is read from env at build
// time; missing => agent disabled, all traffic goes to CLI.
type DispatcherConfig struct {
	Logger    *slog.Logger
	Bus       *event.Bus
	Executor  *Executor
	Catalogue *ToolCatalogue
	APIKey    string  // ANTHROPIC_API_KEY
	Model     string  // optional override
	APIBaseURL string // optional override (testing)
	DB        *sql.DB // optional, used for Phase B3 budget gate
}

func NewDispatcher(cfg DispatcherConfig) *Dispatcher {
	d := &Dispatcher{
		log:                cfg.Logger,
		bus:                cfg.Bus,
		cli:                cfg.Executor,
		catalogue:          cfg.Catalogue,
		MinAgentConfidence: 0.6,
		AgentEnabledIntents: map[Intent]bool{
			IntentCustomerLookup: true,
			IntentSystemStatus:   true,
			IntentDataQuery:      true,
		},
	}
	if cfg.APIKey != "" && cfg.Catalogue != nil {
		d.agent = NewAgentClient(AgentConfig{
			APIKey:    cfg.APIKey,
			Model:     cfg.Model,
			BaseURL:   cfg.APIBaseURL,
			Catalogue: cfg.Catalogue,
		})
		if cfg.Logger != nil {
			cfg.Logger.Info("chat dispatcher: agent path enabled",
				"model", d.agent.model, "tools", len(cfg.Catalogue.All()))
		}
	}
	if cfg.DB != nil {
		d.budget = NewBudgetGate(cfg.DB, cfg.Logger)
		d.memDB = cfg.DB
	}
	return d
}

// NewDispatcherFromEnv reads ANTHROPIC_API_KEY (and optionally
// RAIN_AGENT_MODEL) from env and constructs the dispatcher.
// Missing key => agent path off, CLI handles everything.
// db is optional; nil disables Phase B3 budget gating + Phase D1
// agent memory.
func NewDispatcherFromEnv(log *slog.Logger, bus *event.Bus, cli *Executor, db *sql.DB, baseURL string) *Dispatcher {
	cat := NewToolCatalogueWithDB(baseURL, db)
	return NewDispatcher(DispatcherConfig{
		Logger:    log,
		Bus:       bus,
		Executor:  cli,
		Catalogue: cat,
		APIKey:    os.Getenv("ANTHROPIC_API_KEY"),
		Model:     os.Getenv("RAIN_AGENT_MODEL"),
		DB:        db,
	})
}

// Dispatch picks the right path for a request and streams
// progress to the bus. Falls back to CLI on any agent failure.
// Returns the final response text the same way Executor.Execute
// does, so callers don't need to special-case which path ran.
func (d *Dispatcher) Dispatch(ctx context.Context, req ExecuteRequest) (string, error) {
	classification := ClassifyIntent(req.Prompt)
	d.publishIntent(req.ConversationID, classification)

	useAgent := d.agent != nil &&
		d.AgentEnabledIntents[classification.Intent] &&
		classification.Confidence >= d.MinAgentConfidence

	// Phase B3 budget gate. Only checked when we'd actually run
	// the agent path — the CLI path doesn't burn Anthropic API
	// spend tracked here. A blocked verdict refuses the agent
	// run; the user sees a clear refusal so the operator can
	// intervene (raise the cap, or wait for the weekly reset).
	if useAgent && d.budget != nil {
		bs := d.budget.Check(req.UserID)
		if bs.Verdict == "blocked" {
			msg := fmt.Sprintf(
				"agent budget blocked: user %q has spent R%.2f / R%.2f this week (%.0f%%). "+
					"Falling back to CLI. To raise the cap, see /api/v1/budgets.",
				bs.UserID, bs.SpentZAR, bs.CapZAR, bs.PctSpent,
			)
			if d.log != nil {
				d.log.Warn("budget blocked agent path",
					"user", bs.UserID, "spent", bs.SpentZAR, "cap", bs.CapZAR)
			}
			d.bus.PublishJSON("chat.stream", StreamEvent{
				ConversationID: req.ConversationID,
				Type:           "stream",
				Content:        msg,
				Metadata:       map[string]any{"budget": bs, "kind": "budget_blocked"},
			})
			useAgent = false
		}
	}

	if !useAgent {
		if d.log != nil {
			d.log.Info("chat dispatcher → CLI",
				"conv", req.ConversationID,
				"intent", classification.Intent,
				"confidence", classification.Confidence,
				"agent_available", d.agent != nil)
		}
		return d.cli.ExecuteContext(ctx, req)
	}

	if d.log != nil {
		d.log.Info("chat dispatcher → API agent loop",
			"conv", req.ConversationID,
			"intent", classification.Intent,
			"confidence", classification.Confidence)
	}

	systemPrompt := buildAgentSystemPrompt(classification)
	if req.UserID != "" {
		systemPrompt += fmt.Sprintf(" The current user is %q.", req.UserID)
	} else {
		systemPrompt += " The current user is anonymous; refuse any action that mutates state."
	}
	// Phase D1 — inject the user's recent memory entries so the
	// agent recalls preferences, prior incident context, and
	// observed patterns across sessions. Non-anonymous only.
	if d.memDB != nil && req.UserID != "" {
		mem := LoadRecentMemory(d.memDB, req.UserID)
		systemPrompt += FormatForPrompt(mem)
		systemPrompt += "\nIf you learn something memorable about the user (a preference, an incident finding, a recurring pattern), call the `remember` tool to persist it. Be selective — only memorable things, not chit-chat."
	}
	// Phase D2 — surface the active incident_id so every tool
	// call the agent makes can quote it in its summary, every
	// approval it raises ties back, and the spend on this run is
	// attributable to the incident.
	if req.IncidentID != "" {
		systemPrompt += fmt.Sprintf(
			"\n\nActive incident: %q. Mention this id when you summarise findings or create approvals so we can correlate everything you do during this session with the incident timeline.",
			req.IncidentID,
		)
	}
	// Phase B3 — surface budget warning to the model so it can
	// be more economical on tool-use depth when the user is near
	// the cap. Doesn't change behaviour, just adds a hint.
	if d.budget != nil {
		bs := d.budget.Check(req.UserID)
		if bs.Verdict == "warn" {
			systemPrompt += fmt.Sprintf(
				" Note: this user is at %.0f%% of weekly spend cap (R%.2f/R%.2f). "+
					"Be economical — keep tool-use depth shallow.",
				bs.PctSpent, bs.SpentZAR, bs.CapZAR,
			)
		}
	}
	emit := func(t AgentTurn) {
		// Stream every turn to the bus in the same shape the
		// existing chat.stream consumer expects, plus a metadata
		// blob for the future Phase C2 audit pane.
		evt := StreamEvent{
			ConversationID: req.ConversationID,
			Type:           "stream",
			Content:        renderTurnLine(t),
			Metadata: map[string]any{
				"agent_turn": t,
				"intent":     classification.Intent,
			},
		}
		d.bus.PublishJSON("chat.stream", evt)
	}

	finalText, _, err := d.agent.Run(ctx, AgentRunOptions{
		SystemPrompt: systemPrompt,
		UserPrompt:   req.Prompt,
		UserID:       req.UserID,
		Emit:         emit,
	})
	if err != nil {
		// Agent path failed — fall back to CLI rather than 500ing
		// the user. Log the failure so we can tune.
		if d.log != nil {
			d.log.Warn("chat dispatcher: agent failed → falling back to CLI",
				"conv", req.ConversationID, "error", err)
		}
		fallback, cliErr := d.cli.ExecuteContext(ctx, req)
		if cliErr != nil {
			return "", fmt.Errorf("agent failed (%v) and CLI fallback also failed: %w", err, cliErr)
		}
		return fallback, nil
	}

	d.bus.PublishJSON("chat.complete", StreamEvent{
		ConversationID: req.ConversationID,
		Type:           "complete",
		Content:        finalText,
		Metadata:       map[string]any{"intent": classification.Intent},
	})
	return finalText, nil
}

func (d *Dispatcher) publishIntent(convID string, c IntentResult) {
	if d.bus == nil {
		return
	}
	d.bus.PublishJSON("chat.stream", StreamEvent{
		ConversationID: convID,
		Type:           "stream",
		Content:        fmt.Sprintf("intent: %s (confidence %.2f) — %s", c.Intent, c.Confidence, c.Reason),
		Metadata:       map[string]any{"intent": c, "kind": "intent_classified"},
	})
}

// renderTurnLine produces a one-line summary the existing
// chat.stream consumer can show as a "thinking…" line.
func renderTurnLine(t AgentTurn) string {
	switch t.Kind {
	case "tool_call":
		raw, _ := json.Marshal(t.ToolInput)
		return fmt.Sprintf("→ tool: %s %s", t.ToolName, string(raw))
	case "tool_result":
		if t.Error != "" {
			return fmt.Sprintf("← tool error: %s — %s", t.ToolName, t.Error)
		}
		return fmt.Sprintf("← tool ok: %s", t.ToolName)
	case "final":
		return t.Text
	default:
		if t.Error != "" {
			return "agent error: " + t.Error
		}
		return ""
	}
}

// buildAgentSystemPrompt is intentionally short — the model
// already knows how to use tools; we just steer it toward the
// rain ops domain. Phase B will inject identity + role here too.
func buildAgentSystemPrompt(c IntentResult) string {
	prefix := "You are Soldier of God, the rain ops assistant. " +
		"You can call tools to look up customers, check platform health, " +
		"inspect Axiom data, and search audit logs. " +
		"Keep responses tight and answer-first — ops is in a hurry. " +
		"Cite the tool you used at the end of each answer."
	switch c.Intent {
	case IntentCustomerLookup:
		return prefix + " The user is asking about a specific customer; lead with `customer_360`."
	case IntentSystemStatus:
		return prefix + " The user wants platform status; lead with `platform_health` and `platform_alerts`."
	case IntentDataQuery:
		return prefix + " The user wants aggregated data; reach for `axiom_search_columns` first to find the right table."
	}
	return prefix
}

