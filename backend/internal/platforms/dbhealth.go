package platforms

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/SoldierOfGod1/command-centre/internal/customer"
	"github.com/SoldierOfGod1/command-centre/internal/store"
)

// DatabaseHealth is a snapshot of one configured connection's
// health. Axiom-prefixed IDs get `priority = "p1"` and a shorter
// check interval. Other connections are best-effort.
type DatabaseHealth struct {
	ID              string    `json:"id"`
	Label           string    `json:"label"`
	Driver          string    `json:"driver"`
	Host            string    `json:"host"`
	Database        string    `json:"database"`
	Priority        string    `json:"priority"` // "p1" | "standard"
	Reachable       bool      `json:"reachable"`
	PingMS          int64     `json:"ping_ms"`
	QueryMS         int64     `json:"query_ms"`
	ActiveSessions  int       `json:"active_sessions"`
	Error           string    `json:"error,omitempty"`
	CheckedAt       time.Time `json:"checked_at"`
	LastSuccess     time.Time `json:"last_success,omitempty"`
	LastFailure     time.Time `json:"last_failure,omitempty"`
	FailureStreak   int       `json:"failure_streak"`
	IsAxiomRelated  bool      `json:"is_axiom"`
}

// DBMonitor runs the health loop over every configured Connection.
// Axiom-prefixed connections poll every 30s; the rest every 60s.
type DBMonitor struct {
	log     *slog.Logger
	mgr     *customer.Manager
	store   *store.Store
	latest  map[string]DatabaseHealth
	streak  map[string]int
	lastOK  map[string]time.Time
	lastErr map[string]time.Time
	mu      sync.RWMutex
	sink    AlertSink
}

// NewDBMonitor wires manager + store + optional alert sink.
func NewDBMonitor(log *slog.Logger, mgr *customer.Manager, s *store.Store) *DBMonitor {
	return &DBMonitor{
		log:     log,
		mgr:     mgr,
		store:   s,
		latest:  map[string]DatabaseHealth{},
		streak:  map[string]int{},
		lastOK:  map[string]time.Time{},
		lastErr: map[string]time.Time{},
	}
}

// SetAlertSink attaches an AlertSink — same interface the HTTP
// monitor uses, so DB alerts land in the same feed / incident table.
func (d *DBMonitor) SetAlertSink(s AlertSink) { d.sink = s }

// Run probes all configured connections on a 90s base tick. Axiom
// IDs run every tick (so 90s); non-Axiom every second tick (→ 180s).
// Previous 30s/60s cadence was too aggressive — each ping ran three
// queries against Axiom prod and could contribute to replication
// lag on a busy cluster.
func (d *DBMonitor) Run(ctx context.Context) {
	tick := 0
	d.probe(ctx, true) // first pass warms the cache including non-Axiom
	t := time.NewTicker(90 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick++
			d.probe(ctx, tick%2 == 0)
		}
	}
}

// Snapshot returns the latest DB health slice — Axiom pinned first,
// then alphabetical by label.
func (d *DBMonitor) Snapshot() []DatabaseHealth {
	d.mu.RLock()
	defer d.mu.RUnlock()
	// Always emit a row per configured connection even if never
	// probed — drives the "unknown" tile on first paint.
	conns, err := d.store.ListConnections()
	if err != nil {
		return nil
	}
	out := make([]DatabaseHealth, 0, len(conns))
	for _, c := range conns {
		if h, ok := d.latest[c.ID]; ok {
			out = append(out, h)
			continue
		}
		out = append(out, DatabaseHealth{
			ID:             c.ID,
			Label:          c.Label,
			Driver:         c.Driver,
			Host:           c.Host,
			Database:       c.Database,
			Priority:       priorityFor(c.ID),
			IsAxiomRelated: isAxiomID(c.ID),
		})
	}
	// Sort: Axiom first, then alphabetical.
	// (Simple insertion sort — N is small.)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if cmpDB(out[j], out[i]) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// cmpDB returns true if a should sort before b: Axiom before
// non-Axiom; within same group, alphabetical by label.
func cmpDB(a, b DatabaseHealth) bool {
	if a.IsAxiomRelated != b.IsAxiomRelated {
		return a.IsAxiomRelated
	}
	return strings.ToLower(a.Label) < strings.ToLower(b.Label)
}

func priorityFor(id string) string {
	if isAxiomID(id) {
		return "p1"
	}
	return "standard"
}

// isAxiomID recognises connections that should run on the P1
// (every-tick) cadence. Originally just rain Axiom; expanded to
// cover Huawei GaussDB DWS which carries similar BSS-critical
// load. The struct field stays IsAxiomRelated for back-compat
// (frontend serialises it); see CRITICAL_DB_PREFIXES.
//
// CRITICAL_DB_PREFIXES — id-prefix list of connections that get:
//   - p1 priority (probed every 90s, not 180s)
//   - P1-severity escalation when down >= 3 ticks
//   - axiom-related labelling on the dashboard
var CRITICAL_DB_PREFIXES = []string{"axiom", "gaussdb"}

func isAxiomID(id string) bool {
	low := strings.ToLower(id)
	for _, p := range CRITICAL_DB_PREFIXES {
		if strings.HasPrefix(low, p) {
			return true
		}
	}
	return false
}

func (d *DBMonitor) probe(ctx context.Context, includeStandard bool) {
	conns, err := d.store.ListConnections()
	if err != nil {
		return
	}
	var wg sync.WaitGroup
	for _, c := range conns {
		axiom := isAxiomID(c.ID)
		if !includeStandard && !axiom {
			continue
		}
		if c.Driver != "postgres" || !c.Filled() {
			continue
		}
		wg.Add(1)
		go func(id, label, host, database string, axiom bool) {
			defer wg.Done()
			h := d.probeOne(ctx, id, label, host, database, axiom)
			d.mu.Lock()
			prev := d.latest[id]
			if h.Reachable {
				d.streak[id] = 0
				d.lastOK[id] = h.CheckedAt
			} else {
				d.streak[id]++
				d.lastErr[id] = h.CheckedAt
			}
			h.FailureStreak = d.streak[id]
			h.LastSuccess = d.lastOK[id]
			h.LastFailure = d.lastErr[id]
			d.latest[id] = h
			d.mu.Unlock()
			d.emitAlertsIfNeeded(ctx, h, prev)
		}(c.ID, c.Label, c.Host, c.Database, axiom)
	}
	wg.Wait()
}

func (d *DBMonitor) probeOne(ctx context.Context, id, label, host, database string, axiom bool) DatabaseHealth {
	h := DatabaseHealth{
		ID:             id,
		Label:          label,
		Driver:         "postgres",
		Host:           host,
		Database:       database,
		Priority:       priorityFor(id),
		IsAxiomRelated: axiom,
		CheckedAt:      time.Now().UTC(),
	}
	// 5s cap per probe — we never want to hold a snapshot on the
	// primary long enough to contribute to replication lag.
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	pool, _, err := d.mgr.PoolByIDReadOnlyWithDB(pctx, id, "")
	if err != nil {
		h.Error = err.Error()
		return h
	}
	pingStart := time.Now()
	if err := pool.Ping(pctx); err != nil {
		h.PingMS = time.Since(pingStart).Milliseconds()
		h.Error = err.Error()
		return h
	}
	h.PingMS = time.Since(pingStart).Milliseconds()
	h.Reachable = true
	// SELECT 1 round-trip latency — a better "alive" signal than
	// Ping alone because it exercises query planning. Dropped the
	// pg_stat_activity count: it's a full-process-table sweep on
	// Axiom and was the single biggest contributor to monitor-
	// induced replication lag.
	qStart := time.Now()
	var one int
	_ = pool.QueryRow(pctx, `SELECT 1`).Scan(&one)
	h.QueryMS = time.Since(qStart).Milliseconds()
	return h
}

func (d *DBMonitor) emitAlertsIfNeeded(ctx context.Context, cur, prev DatabaseHealth) {
	if d.sink == nil {
		return
	}
	// Reuse the AlertSink contract via a synthetic Target + Status.
	t := Target{
		ID: cur.ID, Name: cur.Label, Group: "database",
		Criticality: CriticalityTop, Environment: "internal",
	}
	st := Status{
		ID: cur.ID, Name: cur.Label, Group: "database",
		State: statusFromHealth(cur), LatencyMS: cur.QueryMS,
		CheckedAt: cur.CheckedAt, Error: cur.Error,
		FailureStreak: cur.FailureStreak,
	}
	// Recovery edge — DB came back after being unreachable.
	if cur.Reachable && !prev.Reachable && prev.ID != "" {
		d.sink.Emit(ctx, Alert{
			ServiceID: cur.ID, Kind: "recovered",
			Severity: SeverityInfo,
			Message:  cur.Label + " database recovered",
			CreatedAt: cur.CheckedAt,
		}, nil, st)
		return
	}
	if !cur.Reachable {
		sev := SeverityCritical
		if cur.IsAxiomRelated {
			sev = SeverityP1
		}
		kind := "db_unreachable"
		if cur.FailureStreak == 1 {
			kind = "db_first_failure"
			sev = SeverityWarning
		}
		d.sink.Emit(ctx, Alert{
			ServiceID: cur.ID, Kind: kind,
			Severity: sev,
			Message:  cur.Label + " database unreachable",
			Cause:    cur.Error,
			NextStep: "Inspect DB host, connection pool, replication, and dependent apps.",
			CreatedAt: cur.CheckedAt,
		}, nil, st)
		return
	}
	// Slow-query warning (Axiom only — P1 critical is the common
	// source of operator pain).
	if cur.IsAxiomRelated && cur.QueryMS >= 10_000 {
		d.sink.Emit(ctx, Alert{
			ServiceID: cur.ID, Kind: "db_slow_query",
			Severity: SeverityCritical,
			Message:  cur.Label + " SELECT 1 took ≥10s — Axiom slow path",
			Cause:    "Likely replication lag, saturated pool, or active-session storm.",
			NextStep: "Check pg_stat_replication + pg_stat_activity; consider failover.",
			CreatedAt: cur.CheckedAt,
		}, nil, st)
	}
	_ = t
}

func statusFromHealth(h DatabaseHealth) string {
	if h.Reachable {
		return "up"
	}
	return "down"
}
