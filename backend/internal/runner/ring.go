package runner

import (
	"sync"
	"time"
)

// LogLine is one captured line of stdout or stderr from a dev server.
type LogLine struct {
	Time   time.Time `json:"time"`
	Stream string    `json:"stream"` // "stdout" | "stderr"
	Role   string    `json:"role"`   // "backend" | "frontend"
	Line   string    `json:"line"`
}

// ringBuffer is a bounded in-memory FIFO of LogLine values. When it
// fills up, older lines are overwritten. A dev server can be chatty;
// we cap at a few hundred lines per component to keep memory bounded.
type ringBuffer struct {
	mu    sync.Mutex
	data  []LogLine
	head  int
	size  int
	cap   int
}

func newRingBuffer(cap int) *ringBuffer {
	if cap <= 0 {
		cap = 500
	}
	return &ringBuffer{data: make([]LogLine, cap), cap: cap}
}

func (r *ringBuffer) add(l LogLine) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[r.head] = l
	r.head = (r.head + 1) % r.cap
	if r.size < r.cap {
		r.size++
	}
}

// tail returns the last `n` entries (oldest first). n <= 0 returns all.
func (r *ringBuffer) tail(n int) []LogLine {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.size == 0 {
		return nil
	}
	if n <= 0 || n > r.size {
		n = r.size
	}
	start := (r.head - r.size + r.cap) % r.cap
	skip := r.size - n
	if skip > 0 {
		start = (start + skip) % r.cap
	}
	out := make([]LogLine, n)
	for i := 0; i < n; i++ {
		out[i] = r.data[(start+i)%r.cap]
	}
	return out
}
