package chat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestToolCatalogue_AllToolsRegistered(t *testing.T) {
	c := NewToolCatalogue("http://localhost:8080/api/v1")
	tools := c.All()
	want := []string{
		"customer_360", "axiom_peek", "axiom_search_columns",
		"platform_health", "platform_alerts", "incident_list",
		"imsi_audit_search", "imsi_override_get",
		"imsi_override_set", "approval_create",
		"remember", // Phase D1
	}
	if len(tools) != len(want) {
		t.Fatalf("expected %d tools, got %d", len(want), len(tools))
	}
	for i, n := range want {
		if tools[i].Name != n {
			t.Errorf("tool[%d]: expected %q, got %q", i, n, tools[i].Name)
		}
	}
}

func TestToolCatalogue_Schema_NoRunCallbackLeak(t *testing.T) {
	c := NewToolCatalogue("http://localhost:8080/api/v1")
	schema := c.Schema()
	if len(schema) != 11 {
		t.Fatalf("expected 11 schema entries, got %d", len(schema))
	}
	for i, s := range schema {
		if _, ok := s["Run"]; ok {
			t.Errorf("entry %d leaks Run callback", i)
		}
		for _, must := range []string{"name", "description", "input_schema"} {
			if _, ok := s[must]; !ok {
				t.Errorf("entry %d missing %q", i, must)
			}
		}
	}
}

func TestToolCatalogue_FindByName(t *testing.T) {
	c := NewToolCatalogue("http://localhost:8080/api/v1")
	if c.Find("customer_360") == nil {
		t.Error("find customer_360 missing")
	}
	if c.Find("nonexistent") != nil {
		t.Error("find on missing tool should return nil")
	}
}

func TestToolCatalogue_WriteFlagsCorrect(t *testing.T) {
	c := NewToolCatalogue("http://localhost:8080/api/v1")
	expected := map[string]bool{
		"customer_360":         false,
		"axiom_peek":           false,
		"axiom_search_columns": false,
		"platform_health":      false,
		"platform_alerts":      false,
		"incident_list":        false,
		"imsi_audit_search":    false,
		"imsi_override_get":    false,
		"imsi_override_set":    true,
		"approval_create":      true,
		// remember is intentionally non-Write — agent_memory is local
		// per-user state, not a destructive ops mutation, so the
		// approval gate doesn't fire on it.
		"remember": false,
	}
	for name, want := range expected {
		got := c.Find(name)
		if got == nil {
			t.Errorf("%s: not found", name)
			continue
		}
		if got.Write != want {
			t.Errorf("%s: write flag mismatch, want %v got %v", name, want, got.Write)
		}
	}
}

func TestToolCatalogue_RunGet_HTTPCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/customer" {
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
		if r.URL.Query().Get("mode") != "email" {
			t.Errorf("missing mode: %v", r.URL.Query())
		}
		if r.URL.Query().Get("value") != "alice@example.com" {
			t.Errorf("missing value: %v", r.URL.Query())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"identity":{"id":"x"}}`))
	}))
	defer srv.Close()

	c := NewToolCatalogue(srv.URL + "/api/v1")
	t360 := c.Find("customer_360")
	args := json.RawMessage(`{"mode":"email","value":"alice@example.com"}`)
	got, err := t360.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	m, ok := got.(map[string]any)
	if !ok || m["identity"] == nil {
		t.Errorf("unexpected response shape: %v", got)
	}
}

func TestToolCatalogue_RunPutPath_PathSubstitution(t *testing.T) {
	var receivedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/cust-123/imsi-override") {
			t.Errorf("path not substituted: %q", r.URL.Path)
		}
		receivedBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"imsis":["655..."],"count":1}`))
	}))
	defer srv.Close()

	c := NewToolCatalogue(srv.URL + "/api/v1")
	tool := c.Find("imsi_override_set")
	args := json.RawMessage(`{"customer_id":"cust-123","imsis":["655380004807362"]}`)
	if _, err := tool.Run(context.Background(), args); err != nil {
		t.Fatalf("run: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(receivedBody, &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	imsis, ok := body["imsis"].([]any)
	if !ok || len(imsis) != 1 || imsis[0] != "655380004807362" {
		t.Errorf("body imsis missing or wrong: %v", body)
	}
	if _, hasPathArg := body["customer_id"]; hasPathArg {
		t.Error("customer_id leaked into request body — should be path-only")
	}
}

func TestToolCatalogue_RunPostJSON_BodyShape(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"id":42}`))
	}))
	defer srv.Close()

	c := NewToolCatalogue(srv.URL + "/api/v1")
	tool := c.Find("approval_create")
	args := json.RawMessage(`{"title":"override IMSI","summary":"customer X","context":{"reason":"swap"}}`)
	if _, err := tool.Run(context.Background(), args); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got["title"] != "override IMSI" {
		t.Errorf("title mismatch: %v", got)
	}
	if ctx, _ := got["context"].(map[string]any); ctx["reason"] != "swap" {
		t.Errorf("context not forwarded: %v", got)
	}
}

func TestToolCatalogue_HTTPErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"RAIN_SUPPORT_L2 not set"}`))
	}))
	defer srv.Close()

	c := NewToolCatalogue(srv.URL + "/api/v1")
	tool := c.Find("imsi_override_set")
	args := json.RawMessage(`{"customer_id":"x","imsis":[]}`)
	body, err := tool.Run(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for HTTP 403, got nil")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error should mention status code: %v", err)
	}
	// Body should still be returned for the agent to see the error message.
	if m, _ := body.(map[string]any); m["error"] != "RAIN_SUPPORT_L2 not set" {
		t.Errorf("expected body to surface server error: %v", body)
	}
}

func TestToolCatalogue_DecodeArgs_EmptyOK(t *testing.T) {
	got, err := decodeArgs(nil)
	if err != nil {
		t.Errorf("nil args should be ok, got: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

func TestToolCatalogue_DecodeArgs_InvalidJSON(t *testing.T) {
	_, err := decodeArgs(json.RawMessage(`not json`))
	if err == nil {
		t.Error("expected error on bad json")
	}
}
