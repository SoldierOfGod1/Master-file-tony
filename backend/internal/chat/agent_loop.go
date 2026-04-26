package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AgentClient runs a tool-use loop against Anthropic's Messages
// API. Phase A3 of the agent-orchestrator plan: this is the
// non-CLI path that handles ops-type prompts (customer lookups,
// system status, data queries) without spinning up Claude Code as
// a subprocess.
//
// Architecture:
//   user prompt
//     -> ClassifyIntent picks customer_lookup / system_status /
//        data_query / code_task / unclear
//     -> code_task / unclear with no signals fall through to the
//        existing CLI Executor in executor.go
//     -> the rest land here. Each turn:
//          1. POST /v1/messages with the current message history
//             plus the tool catalogue.
//          2. If the response has tool_use blocks, invoke each via
//             ToolCatalogue.Find(name).Run, append the result as a
//             tool_result message, loop.
//          3. If the response has only text blocks (no tool_use),
//             return the concatenated text — that's the final answer.
//
// Hand-rolled rather than using the official SDK because the Go
// SDK adds a 12MB+ dep tree for a feature surface we use 5% of;
// the Messages JSON is stable and small.
type AgentClient struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
	cat     *ToolCatalogue
	// MaxTurns caps the tool-use loop so a runaway model doesn't
	// burn unbounded tokens. Default 8 — enough for any reasonable
	// ops chain (lookup customer → check payments → check sims →
	// check incidents) with headroom.
	MaxTurns int
}

// AgentConfig builds an AgentClient. APIKey + Model are required;
// BaseURL defaults to api.anthropic.com.
type AgentConfig struct {
	APIKey   string
	Model    string
	BaseURL  string
	Catalogue *ToolCatalogue
}

func NewAgentClient(cfg AgentConfig) *AgentClient {
	c := &AgentClient{
		apiKey:   cfg.APIKey,
		model:    cfg.Model,
		baseURL:  cfg.BaseURL,
		cat:      cfg.Catalogue,
		MaxTurns: 8,
		client:   &http.Client{Timeout: 5 * time.Minute},
	}
	if c.baseURL == "" {
		c.baseURL = "https://api.anthropic.com"
	}
	if c.model == "" {
		// Default to a fast, cheap, multi-turn model for ops
		// queries. Override via chat config if needed.
		c.model = "claude-haiku-4-5-20251001"
	}
	return c
}

// AgentTurn is one round-trip event in the loop. Streamed back via
// the event bus so the UI can show "looking up customer / checking
// payments / ..." progress instead of staring at an empty screen.
type AgentTurn struct {
	Kind        string         `json:"kind"`        // "thinking" | "tool_call" | "tool_result" | "final"
	ToolName    string         `json:"tool_name,omitempty"`
	ToolInput   any            `json:"tool_input,omitempty"`
	ToolResult  any            `json:"tool_result,omitempty"`
	Text        string         `json:"text,omitempty"`
	Error       string         `json:"error,omitempty"`
	At          time.Time      `json:"at"`
}

// Run executes the loop. emit is called once per significant step
// so the caller can stream into the event bus / WebSocket. Returns
// the final answer text plus the full turn history for audit.
func (a *AgentClient) Run(ctx context.Context, systemPrompt, userPrompt string, emit func(AgentTurn)) (string, []AgentTurn, error) {
	if a.apiKey == "" {
		return "", nil, fmt.Errorf("agent client has no API key")
	}
	if emit == nil {
		emit = func(AgentTurn) {}
	}

	messages := []anthropicMessage{
		{Role: "user", Content: []anthropicBlock{{Type: "text", Text: userPrompt}}},
	}
	turns := []AgentTurn{}

	for turn := 0; turn < a.MaxTurns; turn++ {
		req := anthropicRequest{
			Model:        a.model,
			MaxTokens:    4096,
			System:       systemPrompt,
			Messages:     messages,
			Tools:        nil,
		}
		if a.cat != nil {
			req.Tools = a.cat.Schema()
		}

		resp, err := a.callMessages(ctx, req)
		if err != nil {
			t := AgentTurn{Kind: "thinking", Error: err.Error(), At: time.Now()}
			turns = append(turns, t)
			emit(t)
			return "", turns, err
		}

		// Collect assistant blocks for the next round-trip.
		assistantBlocks := resp.Content
		messages = append(messages, anthropicMessage{Role: "assistant", Content: assistantBlocks})

		// Find tool_use blocks. If there are none, we're done.
		toolUses := []anthropicBlock{}
		var finalText strings.Builder
		for _, b := range assistantBlocks {
			switch b.Type {
			case "text":
				finalText.WriteString(b.Text)
				finalText.WriteString("\n")
			case "tool_use":
				toolUses = append(toolUses, b)
			}
		}

		if len(toolUses) == 0 {
			t := AgentTurn{Kind: "final", Text: strings.TrimSpace(finalText.String()), At: time.Now()}
			turns = append(turns, t)
			emit(t)
			return t.Text, turns, nil
		}

		// Execute each tool_use, gather tool_result blocks for the
		// next user message.
		toolResults := make([]anthropicBlock, 0, len(toolUses))
		for _, tu := range toolUses {
			callTurn := AgentTurn{
				Kind: "tool_call", ToolName: tu.Name, ToolInput: tu.Input, At: time.Now(),
			}
			turns = append(turns, callTurn)
			emit(callTurn)

			tool := a.cat.Find(tu.Name)
			if tool == nil {
				errStr := fmt.Sprintf("unknown tool %q", tu.Name)
				resTurn := AgentTurn{Kind: "tool_result", ToolName: tu.Name, Error: errStr, At: time.Now()}
				turns = append(turns, resTurn)
				emit(resTurn)
				toolResults = append(toolResults, anthropicBlock{
					Type:      "tool_result",
					ToolUseID: tu.ID,
					IsError:   true,
					Content:   errStr,
				})
				continue
			}

			rawInput, _ := json.Marshal(tu.Input)
			out, err := tool.Run(ctx, rawInput)
			if err != nil {
				errStr := err.Error()
				resTurn := AgentTurn{Kind: "tool_result", ToolName: tu.Name, ToolResult: out, Error: errStr, At: time.Now()}
				turns = append(turns, resTurn)
				emit(resTurn)
				// Surface both the error string and any partial body
				// the server returned so the model can decide what
				// to do next ("403 — RAIN_SUPPORT_L2 not set" =
				// model can ask user to confirm + retry).
				body, _ := json.Marshal(map[string]any{"error": errStr, "body": out})
				toolResults = append(toolResults, anthropicBlock{
					Type:      "tool_result",
					ToolUseID: tu.ID,
					IsError:   true,
					Content:   string(body),
				})
				continue
			}
			resTurn := AgentTurn{Kind: "tool_result", ToolName: tu.Name, ToolResult: out, At: time.Now()}
			turns = append(turns, resTurn)
			emit(resTurn)
			body, _ := json.Marshal(out)
			toolResults = append(toolResults, anthropicBlock{
				Type:      "tool_result",
				ToolUseID: tu.ID,
				Content:   string(body),
			})
		}

		messages = append(messages, anthropicMessage{Role: "user", Content: toolResults})
	}

	// Hit the loop cap without a final text. Return what we have +
	// a clear marker so the UI can flag it.
	t := AgentTurn{
		Kind: "final",
		Text: fmt.Sprintf("(agent loop hit max %d turns without converging)", a.MaxTurns),
		At:   time.Now(),
		Error: "max_turns_exceeded",
	}
	turns = append(turns, t)
	emit(t)
	return t.Text, turns, fmt.Errorf("max turns exceeded")
}

// ── Anthropic Messages API wire types ──────────────────────────

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []map[string]any   `json:"tools,omitempty"`
}

type anthropicMessage struct {
	Role    string            `json:"role"` // "user" | "assistant"
	Content []anthropicBlock  `json:"content"`
}

type anthropicBlock struct {
	Type string `json:"type"` // "text" | "tool_use" | "tool_result"

	// text
	Text string `json:"text,omitempty"`

	// tool_use
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Input any    `json:"input,omitempty"`

	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

type anthropicResponse struct {
	ID         string           `json:"id"`
	Model      string           `json:"model"`
	StopReason string           `json:"stop_reason"`
	Content    []anthropicBlock `json:"content"`
	Usage      anthropicUsage   `json:"usage"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func (a *AgentClient) callMessages(ctx context.Context, req anthropicRequest) (*anthropicResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("anthropic API %d: %s", resp.StatusCode, string(raw))
	}
	var out anthropicResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("anthropic response parse: %w", err)
	}
	return &out, nil
}
