package chat

import (
	"context"
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
	APIKey    string // ANTHROPIC_API_KEY
	Model     string // optional override
	APIBaseURL string // optional override (testing)
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
	return d
}

// NewDispatcherFromEnv reads ANTHROPIC_API_KEY (and optionally
// RAIN_AGENT_MODEL) from env and constructs the dispatcher.
// Missing key => agent path off, CLI handles everything.
func NewDispatcherFromEnv(log *slog.Logger, bus *event.Bus, cli *Executor, baseURL string) *Dispatcher {
	cat := NewToolCatalogue(baseURL)
	return NewDispatcher(DispatcherConfig{
		Logger:    log,
		Bus:       bus,
		Executor:  cli,
		Catalogue: cat,
		APIKey:    os.Getenv("ANTHROPIC_API_KEY"),
		Model:     os.Getenv("RAIN_AGENT_MODEL"),
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

	finalText, _, err := d.agent.Run(ctx, systemPrompt, req.Prompt, emit)
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

