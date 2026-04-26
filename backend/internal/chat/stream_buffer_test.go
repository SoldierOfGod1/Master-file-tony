package chat

import (
	"fmt"
	"sync"
	"testing"
)

func TestStreamBuffer_AppendThenReplay(t *testing.T) {
	b := NewStreamBuffer()
	b.Append("c-1", []byte(`{"conversationId":"c-1","content":"first"}`))
	b.Append("c-1", []byte(`{"conversationId":"c-1","content":"second"}`))
	got := b.Replay("c-1")
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
	if string(got[0]) != `{"conversationId":"c-1","content":"first"}` {
		t.Errorf("oldest-first order wrong: %s", got[0])
	}
}

func TestStreamBuffer_PerConversationIsolation(t *testing.T) {
	b := NewStreamBuffer()
	b.Append("c-1", []byte(`{"conversationId":"c-1","content":"alice"}`))
	b.Append("c-2", []byte(`{"conversationId":"c-2","content":"bob"}`))
	if len(b.Replay("c-1")) != 1 || len(b.Replay("c-2")) != 1 {
		t.Errorf("conversations should not bleed into each other")
	}
}

func TestStreamBuffer_RingsAtCap(t *testing.T) {
	b := NewStreamBuffer()
	b.bufferSize = 5
	for i := 0; i < 12; i++ {
		b.Append("c-1", []byte(fmt.Sprintf(`{"conversationId":"c-1","content":"chunk-%d"}`, i)))
	}
	got := b.Replay("c-1")
	if len(got) != 5 {
		t.Fatalf("expected ring trimmed to cap=5, got %d", len(got))
	}
	// After 12 appends with cap=5, the buffer holds chunks 7..11.
	if string(got[0]) != `{"conversationId":"c-1","content":"chunk-7"}` {
		t.Errorf("oldest entry wrong after ring: %s", got[0])
	}
	if string(got[len(got)-1]) != `{"conversationId":"c-1","content":"chunk-11"}` {
		t.Errorf("newest entry wrong: %s", got[len(got)-1])
	}
}

func TestStreamBuffer_Forget(t *testing.T) {
	b := NewStreamBuffer()
	b.Append("c-1", []byte(`{"conversationId":"c-1"}`))
	b.Forget("c-1")
	if got := b.Replay("c-1"); got != nil {
		t.Errorf("expected nil after forget, got %v", got)
	}
	// Idempotent — second Forget is a no-op.
	b.Forget("c-1")
	b.Forget("non-existent")
}

func TestStreamBuffer_GlobalCapEvictsLRU(t *testing.T) {
	b := NewStreamBuffer()
	b.maxConversations = 3
	b.Append("c-1", []byte(`{"conversationId":"c-1"}`))
	b.Append("c-2", []byte(`{"conversationId":"c-2"}`))
	b.Append("c-3", []byte(`{"conversationId":"c-3"}`))
	// Touching c-1 again should keep it from being evicted next.
	b.Append("c-1", []byte(`{"conversationId":"c-1","second":true}`))
	// c-4 forces eviction of the least-recently touched (c-2).
	b.Append("c-4", []byte(`{"conversationId":"c-4"}`))
	if got := b.Replay("c-2"); got != nil {
		t.Errorf("expected c-2 evicted (oldest LRU), still present: %v", got)
	}
	if got := b.Replay("c-1"); len(got) != 2 {
		t.Errorf("c-1 should have been preserved (recent touch), got %d entries", len(got))
	}
}

func TestStreamBuffer_NilSafety(t *testing.T) {
	var b *StreamBuffer
	b.Append("c", []byte("x"))
	b.Forget("c")
	if got := b.Replay("c"); got != nil {
		t.Error("nil replay should be nil")
	}
}

func TestStreamBuffer_EmptyConversationIDIgnored(t *testing.T) {
	b := NewStreamBuffer()
	b.Append("", []byte(`{"conversationId":"","content":"x"}`))
	if got := b.Replay(""); got != nil {
		t.Errorf("empty convID should be ignored, got %v", got)
	}
}

func TestExtractConversationID(t *testing.T) {
	cases := map[string]string{
		`{"conversationId":"c-1","content":"x"}`:           "c-1",
		`{"type":"stream","conversationId":"abc-def-ghi"}`: "abc-def-ghi",
		`{"conversationId":""}`:                            "",
		`{"no":"id"}`:                                      "",
		``:                                                 "",
	}
	for in, want := range cases {
		if got := extractConversationID([]byte(in)); got != want {
			t.Errorf("extractConversationID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStreamBuffer_ConcurrentAppendSafe(t *testing.T) {
	b := NewStreamBuffer()
	const goroutines = 20
	const perGoroutine = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				b.Append("c-shared", []byte(fmt.Sprintf(`{"conversationId":"c-shared","g":%d,"i":%d}`, gid, i)))
			}
		}(g)
	}
	wg.Wait()
	// Race detector flags any concurrent write; the assertion is
	// just that we end up with a coherent ring at the cap.
	got := b.Replay("c-shared")
	if len(got) > defaultBufferSize {
		t.Errorf("ring exceeded buffer size: %d > %d", len(got), defaultBufferSize)
	}
}
