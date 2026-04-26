package platforms

import (
	"context"
	"database/sql"
	"sync"
	"time"
)

// SQLHistory persists check results into service_checks and rolls
// up 24h/7d/30d uptime on demand. One row per target per tick; at
// the default 60s cadence that's 1,440/day/target * 15 targets ≈
// 22k rows/day ≈ 650k/month. Acceptable for an internal tool.
// Garbage collection happens AT MOST once per hour per service —
// previously it ran on every INSERT, which doubled the write
// footprint and locked the table more than necessary.
type SQLHistory struct {
	db          *sql.DB
	mu          sync.Mutex
	nextGC      map[string]time.Time
	rollupCache map[string]rollupCacheEntry
}

// NewSQLHistory wraps a *sql.DB. No schema ddl here — relies on the
// migrations in internal/store/migrations.go.
func NewSQLHistory(db *sql.DB) *SQLHistory {
	return &SQLHistory{db: db, nextGC: map[string]time.Time{}}
}

// Record inserts one row. Non-blocking: errors are swallowed because
// history persistence must never fail the main check loop. Rolls a
// once-per-hour GC on the service's rows so the table doesn't grow
// past ~650k entries.
func (h *SQLHistory) Record(ctx context.Context, st Status) {
	if h == nil || h.db == nil {
		return
	}
	_, _ = h.db.ExecContext(ctx,
		`INSERT INTO service_checks (service_id, state, http_code, latency_ms, error, checked_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		st.ID, st.State, st.HTTPCode, st.LatencyMS, st.Error,
		st.CheckedAt.UTC().Format(time.RFC3339Nano),
	)
	h.maybeGC(ctx, st.ID)
}

// maybeGC runs the 30-day DELETE at most once per hour per service.
// The check is cheap (single map lookup + time compare) so it's
// safe to call on every Record.
func (h *SQLHistory) maybeGC(ctx context.Context, serviceID string) {
	h.mu.Lock()
	next, ok := h.nextGC[serviceID]
	now := time.Now()
	if ok && now.Before(next) {
		h.mu.Unlock()
		return
	}
	h.nextGC[serviceID] = now.Add(1 * time.Hour)
	h.mu.Unlock()
	cutoff := now.Add(-31 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	_, _ = h.db.ExecContext(ctx,
		`DELETE FROM service_checks WHERE service_id = ? AND checked_at < ?`,
		serviceID, cutoff,
	)
}

// rollupCache stores the last-computed uptime trio per service so
// we don't run 3×COUNT queries on every tick. 5-minute TTL — way
// longer than a tick, but short enough that a recent outage shows
// up in the UI within a reasonable window.
type rollupCacheEntry struct {
	at                  time.Time
	u24h, u7d, u30d     float64
}

var rollupCacheTTL = 5 * time.Minute

// Rollup returns uptime % across 24h / 7d / 30d windows. Cached
// 5 min per service. Missing data → 0.0 (not 100) so a brand-new
// service doesn't falsely report 100% uptime before it has samples.
func (h *SQLHistory) Rollup(ctx context.Context, serviceID string) (u24h, u7d, u30d float64) {
	if h == nil || h.db == nil {
		return 0, 0, 0
	}
	h.mu.Lock()
	if h.rollupCache == nil {
		h.rollupCache = map[string]rollupCacheEntry{}
	}
	if e, ok := h.rollupCache[serviceID]; ok && time.Since(e.at) < rollupCacheTTL {
		h.mu.Unlock()
		return e.u24h, e.u7d, e.u30d
	}
	h.mu.Unlock()

	now := time.Now().UTC()
	u24h = h.windowUptime(ctx, serviceID, now.Add(-24*time.Hour))
	u7d = h.windowUptime(ctx, serviceID, now.Add(-7*24*time.Hour))
	u30d = h.windowUptime(ctx, serviceID, now.Add(-30*24*time.Hour))

	h.mu.Lock()
	h.rollupCache[serviceID] = rollupCacheEntry{at: now, u24h: u24h, u7d: u7d, u30d: u30d}
	h.mu.Unlock()
	return
}

func (h *SQLHistory) windowUptime(ctx context.Context, serviceID string, since time.Time) float64 {
	var total, up int
	err := h.db.QueryRowContext(ctx, `
		SELECT
		  COUNT(*),
		  SUM(CASE WHEN state = 'up' THEN 1 ELSE 0 END)
		FROM service_checks
		WHERE service_id = ? AND checked_at >= ?`,
		serviceID, since.UTC().Format(time.RFC3339Nano),
	).Scan(&total, &up)
	if err != nil || total == 0 {
		return 0
	}
	return float64(up) / float64(total) * 100.0
}
