package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeAnthropic spins up a test server that returns the canned
// responses one by one — lets us script a multi-turn tool-use
// loop without touching the real API.
type fakeAnthropic struct {
	responses []string
	idx       int
	captured  []anthropicRequest
}

func (f *fakeAnthropic) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		var req anthropicRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.captured = append(f.captured, req)
		if f.idx >= len(f.responses) {
			http.Error(w, `{"error":"out of canned responses"}`, 500)
			return
		}
		_, _ = w.Write([]byte(f.responses[f.idx]))
		f.idx++
	})
}

func TestAgentClient_TextOnly_ReturnsImmediately(t *testing.T) {
	fake := &fakeAnthropic{responses: []string{
		`{"id":"x","model":"haiku","stop_reason":"end_turn","content":[{"type":"text","text":"axiom is up"}],"usage":{"input_tokens":5,"output_tokens":3}}`,
	}}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	a := NewAgentClient(AgentConfig{APIKey: "test", Model: "haiku", BaseURL: srv.URL})
	got, turns, err := a.Run(context.Background(), "system", "is axiom up?", nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "axiom is up" {
		t.Errorf("expected text reply, got %q", got)
	}
	if len(turns) != 1 || turns[0].Kind != "final" {
		t.Errorf("expected 1 final turn, got %v", turns)
	}
	if fake.idx != 1 {
		t.Errorf("expected 1 API call, got %d", fake.idx)
	}
}

func TestAgentClient_OneToolThenText(t *testing.T) {
	// First Anthropic response: tool_use call to platform_health.
	// Second response: final text after seeing the tool result.
	fake := &fakeAnthropic{responses: []string{
		`{"id":"x","model":"h","stop_reason":"tool_use","content":[
			{"type":"text","text":"checking…"},
			{"type":"tool_use","id":"tu_1","name":"platform_health","input":{}}
		]}`,
		`{"id":"y","model":"h","stop_reason":"end_turn","content":[
			{"type":"text","text":"axiom looks healthy."}
		]}`,
	}}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	// Give the catalogue a server too — platform_health calls back
	// to /api/v1/platforms/services.
	healthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"axiom-prod","state":"up"}]}`))
	}))
	defer healthSrv.Close()

	cat := NewToolCatalogue(healthSrv.URL + "/api/v1")
	a := NewAgentClient(AgentConfig{APIKey: "test", Model: "h", BaseURL: srv.URL, Catalogue: cat})

	var streamed []AgentTurn
	got, turns, err := a.Run(context.Background(), "sys", "is axiom up?", func(t AgentTurn) {
		streamed = append(streamed, t)
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "axiom looks healthy." {
		t.Errorf("final text: got %q", got)
	}
	// Turns: tool_call + tool_result + final.
	if len(turns) != 3 {
		t.Errorf("expected 3 turns, got %d: %+v", len(turns), turns)
	}
	if turns[0].Kind != "tool_call" || turns[0].ToolName != "platform_health" {
		t.Errorf("turn 0 wrong: %+v", turns[0])
	}
	if turns[1].Kind != "tool_result" || turns[1].Error != "" {
		t.Errorf("turn 1 wrong: %+v", turns[1])
	}
	if turns[2].Kind != "final" {
		t.Errorf("turn 2 wrong: %+v", turns[2])
	}
	if len(streamed) != len(turns) {
		t.Errorf("emit fired %d times but %d turns recorded", len(streamed), len(turns))
	}
	// Round 2 sent back tool_result.
	if fake.idx != 2 {
		t.Errorf("expected 2 API calls, got %d", fake.idx)
	}
}

func TestAgentClient_UnknownToolGracefulError(t *testing.T) {
	fake := &fakeAnthropic{responses: []string{
		`{"id":"x","model":"h","stop_reason":"tool_use","content":[
			{"type":"tool_use","id":"tu_1","name":"made_up_tool","input":{}}
		]}`,
		`{"id":"y","model":"h","stop_reason":"end_turn","content":[
			{"type":"text","text":"sorry, I don't have that tool."}
		]}`,
	}}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()
	cat := NewToolCatalogue("http://localhost")
	a := NewAgentClient(AgentConfig{APIKey: "test", BaseURL: srv.URL, Catalogue: cat})
	got, turns, err := a.Run(context.Background(), "", "do something unsupported", nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(got, "don't have that tool") {
		t.Errorf("expected graceful final, got %q", got)
	}
	// First turn is tool_call, second tool_result with error.
	if len(turns) < 3 || turns[1].Error == "" {
		t.Errorf("expected error on tool_result turn, got %+v", turns)
	}
}

func TestAgentClient_MaxTurnsCap(t *testing.T) {
	// Always return tool_use — the loop should give up after MaxTurns.
	fake := &fakeAnthropic{}
	for i := 0; i < 20; i++ {
		fake.responses = append(fake.responses,
			`{"id":"x","model":"h","stop_reason":"tool_use","content":[
				{"type":"tool_use","id":"tu","name":"platform_health","input":{}}
			]}`)
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()
	healthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer healthSrv.Close()

	a := NewAgentClient(AgentConfig{APIKey: "x", BaseURL: srv.URL, Catalogue: NewToolCatalogue(healthSrv.URL + "/api/v1")})
	a.MaxTurns = 3
	_, turns, err := a.Run(context.Background(), "", "loop forever", nil)
	if err == nil {
		t.Error("expected max-turns error")
	}
	if !strings.Contains(err.Error(), "max turns") {
		t.Errorf("expected max-turns error, got %v", err)
	}
	// 3 turns of tool_call + tool_result = 6, plus one final = 7.
	if len(turns) < 6 {
		t.Errorf("expected at least 6 turns, got %d", len(turns))
	}
}

func TestAgentClient_NoAPIKeyFailsFast(t *testing.T) {
	a := NewAgentClient(AgentConfig{})
	_, _, err := a.Run(context.Background(), "", "anything", nil)
	if err == nil {
		t.Error("expected error when API key missing")
	}
}

func TestAgentClient_PropagatesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"type":"authentication_error"}}`))
	}))
	defer srv.Close()
	a := NewAgentClient(AgentConfig{APIKey: "bad", BaseURL: srv.URL})
	_, _, err := a.Run(context.Background(), "", "x", nil)
	if err == nil {
		t.Fatal("expected error from API 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should mention status, got %v", err)
	}
}
