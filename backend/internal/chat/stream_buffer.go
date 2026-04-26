package chat

import (
	"sync"

	"github.com/SoldierOfGod1/command-centre/internal/event"
)

// StreamBuffer keeps a per-conversation ring of recent chat.stream
// events so a client that reconnects mid-run can replay the chunks
// it missed while offline. Phase C1 follow-up — closes the
// "agent still working but missed-while-offline chunks are gone"
// gap flagged after the resume indicator landed.
//
// Buffer is bounded per conversation (BufferSize) and across all
// conversations (MaxConversations) so a runaway agent or a long-
// lived browser tab can't OOM the server. Old conversations are
// evicted LRU when the cap is hit; full buffers ring around so
// the most-recent chunks always win.
type StreamBuffer struct {
	bufferSize       int
	maxConversations int
	mu               sync.Mutex
	rings            map[string]*convRing
	// touchSeq is a buffer-wide monotonic counter so LRU ordering
	// is total across conversations. Per-conv nextSeq alone collides
	// across new conversations (every first write is seq=1).
	touchSeq int64
}

const (
	defaultBufferSize       = 100
	defaultMaxConversations = 200
)

func NewStreamBuffer() *StreamBuffer {
	return &StreamBuffer{
		bufferSize:       defaultBufferSize,
		maxConversations: defaultMaxConversations,
		rings:            make(map[string]*convRing),
	}
}

// convRing is a fixed-capacity FIFO of StreamEvent payloads.
// Stores them as []byte (the JSON bytes the bus already prepares)
// so replay is just a Write — no re-marshal cost on the hot path.
type convRing struct {
	cap      int
	items    [][]byte
	// nextSeq is monotonically increasing across the conversation's
	// lifetime so the client can ack what it has and the buffer
	// can stop replaying once the client is caught up. Replay
	// always emits oldest-first so seqs arrive in order.
	nextSeq int64
	// lastTouched is used for LRU eviction when the global cap fires.
	lastTouched int64
}

// Append stores the event JSON for later replay. Called by the
// bus.Subscribe hook in main.go; the payload is whatever the
// dispatcher / executor emits to chat.stream.
func (b *StreamBuffer) Append(convID string, payload []byte) {
	if b == nil || convID == "" || len(payload) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	r, ok := b.rings[convID]
	if !ok {
		// Evict oldest conversation if we're at the cap. Cheap
		// linear scan — N is bounded at maxConversations and the
		// op runs once per new conversation.
		if len(b.rings) >= b.maxConversations {
			b.evictOldestLocked()
		}
		r = &convRing{cap: b.bufferSize, items: make([][]byte, 0, b.bufferSize)}
		b.rings[convID] = r
	}
	r.nextSeq++
	b.touchSeq++
	r.lastTouched = b.touchSeq
	if len(r.items) < r.cap {
		r.items = append(r.items, payload)
		return
	}
	// Full ring — shift left by one, keep newest at the tail.
	// Slice realloc would be cheaper with a circular pointer but
	// at cap=100 the shift is 800 byte-slices; not the hot path.
	copy(r.items, r.items[1:])
	r.items[len(r.items)-1] = payload
}

// Replay returns the buffered payloads for one conversation,
// oldest-first. The caller writes them straight to the WebSocket
// before unmuting the live stream. Returns nil if the conv has
// no buffer (no recent activity).
func (b *StreamBuffer) Replay(convID string) [][]byte {
	if b == nil || convID == "" {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	r, ok := b.rings[convID]
	if !ok {
		return nil
	}
	out := make([][]byte, len(r.items))
	copy(out, r.items)
	return out
}

// Forget releases the buffer for a conversation — called when the
// agent loop completes (chat.complete event) so an idle server
// doesn't hold dead chunks indefinitely. Idempotent.
func (b *StreamBuffer) Forget(convID string) {
	if b == nil || convID == "" {
		return
	}
	b.mu.Lock()
	delete(b.rings, convID)
	b.mu.Unlock()
}

// evictOldestLocked drops the ring whose lastTouched is oldest.
// Caller holds b.mu.
func (b *StreamBuffer) evictOldestLocked() {
	var oldestID string
	var oldestTouched int64 = -1
	for id, r := range b.rings {
		if oldestTouched == -1 || r.lastTouched < oldestTouched {
			oldestTouched = r.lastTouched
			oldestID = id
		}
	}
	if oldestID != "" {
		delete(b.rings, oldestID)
	}
}

// AttachToBus subscribes the buffer to the chat.stream and
// chat.complete topics. Called once at startup. The handler
// extracts the conversation_id from the StreamEvent payload —
// every chat.stream / chat.complete / chat.error event carries
// one. event.Handler is func(Event); we pull Payload off Event.
func (b *StreamBuffer) AttachToBus(bus *event.Bus) {
	if b == nil || bus == nil {
		return
	}
	bus.Subscribe("chat.stream", func(e event.Event) {
		convID := extractConversationID(e.Payload)
		b.Append(convID, e.Payload)
	})
	bus.Subscribe("chat.complete", func(e event.Event) {
		// One last append so the client gets the final marker on
		// replay, then forget — no point ringing chunks for a
		// finished conversation.
		convID := extractConversationID(e.Payload)
		b.Append(convID, e.Payload)
		b.Forget(convID)
	})
	bus.Subscribe("chat.error", func(e event.Event) {
		convID := extractConversationID(e.Payload)
		b.Append(convID, e.Payload)
		b.Forget(convID)
	})
}

// extractConversationID parses the conversation_id out of a JSON
// StreamEvent payload without unmarshalling the whole thing.
// The bus emits camelCase 'conversationId' (StreamEvent json tag);
// prefix-match avoids the cost of full JSON parsing on every event.
func extractConversationID(payload []byte) string {
	const key = `"conversationId":"`
	idx := indexOfBytes(payload, []byte(key))
	if idx < 0 {
		return ""
	}
	start := idx + len(key)
	for i := start; i < len(payload); i++ {
		if payload[i] == '"' {
			return string(payload[start:i])
		}
	}
	return ""
}

// indexOfBytes is bytes.Index inlined to avoid the import in this
// already-small file. Equivalent to strings.Index for byte slices.
func indexOfBytes(s, sub []byte) int {
	if len(sub) == 0 {
		return 0
	}
outer:
	for i := 0; i+len(sub) <= len(s); i++ {
		for j := 0; j < len(sub); j++ {
			if s[i+j] != sub[j] {
				continue outer
			}
		}
		return i
	}
	return -1
}
