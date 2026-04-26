package chat

import (
	"strings"
	"testing"
)

func TestRenderTurnLine_ToolCall(t *testing.T) {
	got := renderTurnLine(AgentTurn{Kind: "tool_call", ToolName: "customer_360", ToolInput: map[string]any{"mode": "email", "value": "x"}})
	if !strings.Contains(got, "customer_360") {
		t.Errorf("missing tool name: %q", got)
	}
	if !strings.Contains(got, "email") {
		t.Errorf("missing input: %q", got)
	}
}

func TestRenderTurnLine_ToolResultOK(t *testing.T) {
	got := renderTurnLine(AgentTurn{Kind: "tool_result", ToolName: "customer_360"})
	if !strings.HasPrefix(got, "← tool ok:") {
		t.Errorf("unexpected: %q", got)
	}
}

func TestRenderTurnLine_ToolResultError(t *testing.T) {
	got := renderTurnLine(AgentTurn{Kind: "tool_result", ToolName: "imsi_override_set", Error: "RAIN_SUPPORT_L2 not set"})
	if !strings.Contains(got, "tool error") {
		t.Errorf("expected error prefix: %q", got)
	}
	if !strings.Contains(got, "RAIN_SUPPORT_L2") {
		t.Errorf("error text missing: %q", got)
	}
}

func TestRenderTurnLine_FinalText(t *testing.T) {
	got := renderTurnLine(AgentTurn{Kind: "final", Text: "axiom is healthy"})
	if got != "axiom is healthy" {
		t.Errorf("expected just the text: %q", got)
	}
}

func TestBuildAgentSystemPrompt_CustomerLookupMentionsCustomer360(t *testing.T) {
	prompt := buildAgentSystemPrompt(IntentResult{Intent: IntentCustomerLookup})
	if !strings.Contains(prompt, "customer_360") {
		t.Errorf("expected steering toward customer_360: %q", prompt)
	}
}

func TestBuildAgentSystemPrompt_StatusMentionsHealth(t *testing.T) {
	prompt := buildAgentSystemPrompt(IntentResult{Intent: IntentSystemStatus})
	if !strings.Contains(prompt, "platform_health") {
		t.Errorf("expected steering toward platform_health: %q", prompt)
	}
}

func TestBuildAgentSystemPrompt_AlwaysHasBaseFraming(t *testing.T) {
	intents := []Intent{IntentCustomerLookup, IntentSystemStatus, IntentDataQuery, IntentCodeTask, IntentUnclear}
	for _, i := range intents {
		p := buildAgentSystemPrompt(IntentResult{Intent: i})
		if !strings.Contains(p, "Soldier of God") {
			t.Errorf("intent %q missing brand framing: %q", i, p)
		}
	}
}

func TestNewDispatcher_AgentDisabledWhenNoKey(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{
		Catalogue: NewToolCatalogue("http://localhost"),
		// APIKey deliberately empty
	})
	if d.agent != nil {
		t.Error("expected agent disabled when API key missing")
	}
}

func TestNewDispatcher_AgentEnabledWithKeyAndCatalogue(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{
		APIKey:    "sk-ant-test",
		Catalogue: NewToolCatalogue("http://localhost"),
	})
	if d.agent == nil {
		t.Error("expected agent enabled")
	}
}

func TestNewDispatcher_DefaultIntentSet(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{})
	must := []Intent{IntentCustomerLookup, IntentSystemStatus, IntentDataQuery}
	for _, i := range must {
		if !d.AgentEnabledIntents[i] {
			t.Errorf("expected %q enabled by default", i)
		}
	}
	mustNot := []Intent{IntentCodeTask, IntentUnclear}
	for _, i := range mustNot {
		if d.AgentEnabledIntents[i] {
			t.Errorf("expected %q NOT enabled by default", i)
		}
	}
}
