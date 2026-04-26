package chat

import (
	"testing"
	"time"
)

func TestActiveConversations_BeginEnd(t *testing.T) {
	a := NewActiveConversations()
	if _, ok := a.Get("conv-1"); ok {
		t.Error("expected empty registry to return false")
	}
	a.Begin("conv-1", "agent", "alice")
	st, ok := a.Get("conv-1")
	if !ok {
		t.Fatal("expected conv-1 to be active")
	}
	if st.Path != "agent" || st.UserID != "alice" {
		t.Errorf("state mismatch: %+v", st)
	}
	if st.StartedAt.IsZero() {
		t.Errorf("started_at not set")
	}
	a.End("conv-1")
	if _, ok := a.Get("conv-1"); ok {
		t.Error("expected End to clear the entry")
	}
}

func TestActiveConversations_BeginIdempotent(t *testing.T) {
	a := NewActiveConversations()
	a.Begin("conv-1", "cli", "")
	first, _ := a.Get("conv-1")
	time.Sleep(2 * time.Millisecond)
	a.Begin("conv-1", "agent", "alice")
	second, _ := a.Get("conv-1")
	if !second.StartedAt.After(first.StartedAt) {
		t.Errorf("second Begin should refresh started_at, got first=%v second=%v", first.StartedAt, second.StartedAt)
	}
	if second.Path != "agent" || second.UserID != "alice" {
		t.Errorf("second Begin should overwrite path/user: %+v", second)
	}
}

func TestActiveConversations_NilSafety(t *testing.T) {
	var a *ActiveConversations
	a.Begin("x", "agent", "u") // must not panic
	a.End("x")
	if _, ok := a.Get("x"); ok {
		t.Error("nil registry should return false")
	}
	if got := a.All(); got != nil {
		t.Errorf("nil registry should return nil, got %v", got)
	}
}

func TestActiveConversations_All(t *testing.T) {
	a := NewActiveConversations()
	a.Begin("conv-1", "agent", "alice")
	a.Begin("conv-2", "cli", "bob")
	all := a.All()
	if len(all) != 2 {
		t.Errorf("expected 2 active, got %d", len(all))
	}
	a.End("conv-1")
	all = a.All()
	if len(all) != 1 || all[0].ConversationID != "conv-2" {
		t.Errorf("expected only conv-2 after End, got %v", all)
	}
}

func TestActiveConversations_EmptyConvIDIgnored(t *testing.T) {
	a := NewActiveConversations()
	a.Begin("", "agent", "alice")
	if got := a.All(); len(got) != 0 {
		t.Errorf("empty conv id should be a no-op, got %v", got)
	}
}
