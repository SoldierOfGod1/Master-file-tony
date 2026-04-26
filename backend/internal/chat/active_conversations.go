package chat

import (
	"sync"
	"time"
)

// ActiveConversation is a lightweight tracker of which conversations
// have an in-flight prompt. Phase C1 of the agent-orchestrator plan.
//
// Why a separate tracker (not loops.go's loopTracker)? The existing
// tracker is keyed by projectDir for the Loop Operator panel. Phase
// A3 added the agent path, which doesn't go through QueueManager
// (and so doesn't touch loopTracker at all). We need a single source
// of truth keyed by conversation_id that BOTH paths populate.
//
// On WebSocket reconnect or page reload, the frontend asks
// `GET /api/v1/conversations/{id}/active`; if streaming=true the UI
// shows "agent still working — newer chunks will resume here" while
// the WebSocket reattaches to the chat.stream topic. Chunks emitted
// while the client was offline are unrecoverable, but new ones
// continue to flow.
type ActiveConversation struct {
	ConversationID string    `json:"conversation_id"`
	Path           string    `json:"path"` // "cli" | "agent"
	UserID         string    `json:"user_id,omitempty"`
	StartedAt      time.Time `json:"started_at"`
}

// ActiveConversations is the registry. Singleton wired in main.go,
// populated by the chat handlers via Begin/End.
type ActiveConversations struct {
	mu     sync.RWMutex
	active map[string]ActiveConversation
}

func NewActiveConversations() *ActiveConversations {
	return &ActiveConversations{active: make(map[string]ActiveConversation)}
}

// Begin marks a conversation as in-flight. Idempotent: a second call
// for the same conversation overwrites StartedAt with the latest run
// (most recent matters for "how long has this been working").
func (a *ActiveConversations) Begin(convID, path, userID string) {
	if a == nil || convID == "" {
		return
	}
	a.mu.Lock()
	a.active[convID] = ActiveConversation{
		ConversationID: convID,
		Path:           path,
		UserID:         userID,
		StartedAt:      time.Now().UTC(),
	}
	a.mu.Unlock()
}

// End clears the streaming marker. Safe to call from defer in the
// chat handlers; double-end is a no-op.
func (a *ActiveConversations) End(convID string) {
	if a == nil || convID == "" {
		return
	}
	a.mu.Lock()
	delete(a.active, convID)
	a.mu.Unlock()
}

// Get returns the current state for a conversation, or zero+false
// when nothing is in flight. The caller (the HTTP handler) is the
// one that serialises the bool into a JSON 'streaming' field.
func (a *ActiveConversations) Get(convID string) (ActiveConversation, bool) {
	if a == nil {
		return ActiveConversation{}, false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	v, ok := a.active[convID]
	return v, ok
}

// All snapshot of every active conversation. Used by the loops UI
// when rendering "what's the agent doing right now across all
// conversations". Returns a copy so the caller can iterate safely.
func (a *ActiveConversations) All() []ActiveConversation {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]ActiveConversation, 0, len(a.active))
	for _, v := range a.active {
		out = append(out, v)
	}
	return out
}
