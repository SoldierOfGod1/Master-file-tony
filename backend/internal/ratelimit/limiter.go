// Package ratelimit is a small in-process token-bucket limiter used
// to keep upstream calls from runaway loops.
//
// Why not golang.org/x/time/rate? Honest answer: this codebase has
// avoided third-party deps where the std-lib gets us 95% there. A
// 50-line token bucket is plenty for "stop a chatty page from
// hammering ClickHouse" — we don't need leaky-bucket nuance.
//
// Each Limiter is bound to one named upstream (e.g. "clickhouse",
// "axiom-api"). Two configuration knobs:
//
//	burst       — how many requests can fire back-to-back before
//	              the limiter starts gating (think: how big the
//	              page's initial fan-out is allowed to be)
//	perSecond   — sustained rate in requests/second
//
// Acquire blocks until a token is available or ctx is cancelled.
// The structured Wait() returns an error so callers can degrade
// (return cached/empty) instead of stalling.
package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Limiter is a single named token bucket. Safe for concurrent use.
type Limiter struct {
	name      string
	mu        sync.Mutex
	tokens    float64
	max       float64
	refill    float64 // tokens per second
	last      time.Time
}

// New builds a Limiter that allows `burst` immediate calls and
// refills at `perSecond` rps thereafter. Refills cap at burst.
//
// burst must be >= 1; perSecond must be > 0. Misconfiguration
// returns a Limiter whose Wait always errors so the surface fails
// loudly rather than silently allowing everything.
func New(name string, burst int, perSecond float64) *Limiter {
	if burst < 1 || perSecond <= 0 {
		return &Limiter{name: name}
	}
	return &Limiter{
		name:   name,
		tokens: float64(burst),
		max:    float64(burst),
		refill: perSecond,
		last:   time.Now(),
	}
}

// Wait blocks until a token is available or ctx is cancelled.
// Returns immediately when a token is in the bucket.
func (l *Limiter) Wait(ctx context.Context) error {
	if l == nil || l.refill == 0 {
		return errors.New("ratelimit: limiter not configured")
	}
	for {
		wait, ok := l.tryAcquire()
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("ratelimit %s: %w", l.name, ctx.Err())
		case <-time.After(wait):
			// loop and try again
		}
	}
}

// Allow is the non-blocking version. Returns true when a token was
// taken, false when the caller should back off / serve cached / 429.
func (l *Limiter) Allow() bool {
	if l == nil || l.refill == 0 {
		return false
	}
	_, ok := l.tryAcquire()
	return ok
}

// tryAcquire takes a token if the bucket has one. When empty it
// returns the duration until the next token would be available.
func (l *Limiter) tryAcquire() (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(l.last).Seconds()
	if elapsed > 0 {
		l.tokens += elapsed * l.refill
		if l.tokens > l.max {
			l.tokens = l.max
		}
		l.last = now
	}
	if l.tokens >= 1 {
		l.tokens -= 1
		return 0, true
	}
	// Tokens needed: 1 - tokens. At refill rate, that's
	// (1 - tokens) / refill seconds away.
	deficit := 1 - l.tokens
	wait := time.Duration(deficit/l.refill*float64(time.Second)) + time.Millisecond
	return wait, false
}

// Snapshot returns a coarse view of the bucket state. Useful for
// admin dashboards / debug logs. Don't drive logic off this — by
// the time you read it, another caller may have changed it.
type Snapshot struct {
	Name           string  `json:"name"`
	TokensAvail    float64 `json:"tokens_avail"`
	BurstCap       float64 `json:"burst_cap"`
	RefillPerSec   float64 `json:"refill_per_sec"`
}

func (l *Limiter) Snapshot() Snapshot {
	if l == nil {
		return Snapshot{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return Snapshot{
		Name:         l.name,
		TokensAvail:  l.tokens,
		BurstCap:     l.max,
		RefillPerSec: l.refill,
	}
}
