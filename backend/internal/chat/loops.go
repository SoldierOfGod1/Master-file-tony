package chat

import (
	"sync"
	"time"
)

// LoopState captures one queue worker's live status for the Loop Operator
// panel. Populated while a request is executing, zeroed when idle.
type LoopState struct {
	ProjectDir     string    `json:"project_dir"`
	ConversationID string    `json:"conversation_id,omitempty"`
	StartedAt      time.Time `json:"started_at,omitempty"`
	Paused         bool      `json:"paused"`
	Pending        int       `json:"pending"`
	Running        bool      `json:"running"`
}

// loopTracker is a concurrent snapshot of what each worker is doing.
// Kept private to the chat package; the QueueManager owns it.
type loopTracker struct {
	mu       sync.RWMutex
	active   map[string]*LoopState   // by project dir
	cancels  map[string]func()       // cancel hook for the currently-running CLI
	pauseCh  map[string]chan struct{}
}

func newLoopTracker() *loopTracker {
	return &loopTracker{
		active:  make(map[string]*LoopState),
		cancels: make(map[string]func()),
		pauseCh: make(map[string]chan struct{}),
	}
}

func (t *loopTracker) start(projectDir, convID string, cancel func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.active[projectDir]
	if st == nil {
		st = &LoopState{ProjectDir: projectDir}
		t.active[projectDir] = st
	}
	st.ConversationID = convID
	st.StartedAt = time.Now().UTC()
	st.Running = true
	t.cancels[projectDir] = cancel
}

func (t *loopTracker) finish(projectDir string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if st, ok := t.active[projectDir]; ok {
		st.ConversationID = ""
		st.StartedAt = time.Time{}
		st.Running = false
	}
	delete(t.cancels, projectDir)
}

func (t *loopTracker) setPending(projectDir string, n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.active[projectDir]
	if st == nil {
		st = &LoopState{ProjectDir: projectDir}
		t.active[projectDir] = st
	}
	st.Pending = n
}

func (t *loopTracker) setPaused(projectDir string, paused bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.active[projectDir]
	if st == nil {
		st = &LoopState{ProjectDir: projectDir}
		t.active[projectDir] = st
	}
	st.Paused = paused
	if _, ok := t.pauseCh[projectDir]; !ok {
		t.pauseCh[projectDir] = make(chan struct{}, 1)
	}
	if !paused {
		// Poke the pause channel so a blocked worker can resume.
		select {
		case t.pauseCh[projectDir] <- struct{}{}:
		default:
		}
	}
}

func (t *loopTracker) isPaused(projectDir string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if st, ok := t.active[projectDir]; ok {
		return st.Paused
	}
	return false
}

func (t *loopTracker) waitIfPaused(projectDir string) {
	for t.isPaused(projectDir) {
		t.mu.RLock()
		ch := t.pauseCh[projectDir]
		t.mu.RUnlock()
		if ch == nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
		}
	}
}

// kill fires the cancel hook for the currently-running CLI process in
// that project queue. Returns true if something was killed.
func (t *loopTracker) kill(projectDir string) bool {
	t.mu.Lock()
	cancel := t.cancels[projectDir]
	delete(t.cancels, projectDir)
	t.mu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

func (t *loopTracker) snapshot() []LoopState {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]LoopState, 0, len(t.active))
	for _, st := range t.active {
		out = append(out, *st)
	}
	return out
}
