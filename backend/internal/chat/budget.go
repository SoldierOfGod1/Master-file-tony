package chat

import (
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// BudgetState is what the dispatcher inspects before agent loops
// to decide warn / block / continue. Computed by SpendThisWeek +
// CapFor.
//
// Phase B3 of the agent-orchestrator plan. Pairs with Phase B1
// identity — without a user_id, requests fall into the global
// anonymous bucket which has its own cap (see global_anonymous).
type BudgetState struct {
	UserID    string  `json:"user_id"`
	WeekStart time.Time `json:"week_start"`
	SpentZAR  float64 `json:"spent_zar"`
	CapZAR    float64 `json:"cap_zar"`
	// PctSpent is 0..(100+); over 100 means we already blew the
	// cap. UI renders >=100 in red.
	PctSpent float64 `json:"pct_spent"`
	// Verdict is the dispatcher's gating decision:
	//   ok       — under 80%, agent runs normally
	//   warn     — at 80%-99%, agent runs but system prompt warns
	//   blocked  — at >= 100% of cap, refuse to run agent path
	Verdict string `json:"verdict"`
}

// AnonymousUserID is the bucket used when no user_id is associated
// with the request. Cap is configured under the same id in
// user_budgets so an operator can constrain anon usage explicitly.
const AnonymousUserID = "global_anonymous"

// DefaultWeeklyCapZAR is used when no row exists in user_budgets.
// Picks a deliberately conservative number so first-run after
// deploy can't run away with spend. Operator can raise per user.
const DefaultWeeklyCapZAR = 50.0

// BudgetGate is the cached, mutex-protected wrapper used by the
// dispatcher. SQLite reads are fast but per-request DB hits add
// up; cache budget state for 30 seconds.
type BudgetGate struct {
	db    *sql.DB
	log   *slog.Logger
	mu    sync.Mutex
	cache map[string]budgetCacheEntry
}

type budgetCacheEntry struct {
	state BudgetState
	at    time.Time
}

func NewBudgetGate(db *sql.DB, log *slog.Logger) *BudgetGate {
	return &BudgetGate{db: db, log: log, cache: map[string]budgetCacheEntry{}}
}

// Check returns the BudgetState for a user_id. Uses a 30s cache —
// the spend doesn't change minute-to-minute and recomputing on
// every prompt would hit cost_records repeatedly.
func (g *BudgetGate) Check(userID string) BudgetState {
	if userID == "" {
		userID = AnonymousUserID
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if e, ok := g.cache[userID]; ok && time.Since(e.at) < 30*time.Second {
		return e.state
	}
	state := computeBudget(g.db, userID)
	g.cache[userID] = budgetCacheEntry{state: state, at: time.Now()}
	return state
}

// Invalidate clears the cache for a user — call after writing a
// new cost_records row so the next agent run sees fresh spend.
func (g *BudgetGate) Invalidate(userID string) {
	if userID == "" {
		userID = AnonymousUserID
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.cache, userID)
}

// computeBudget runs the SQL queries for spend + cap. Public-ish
// (lowercase but tested via the gate) so tests can swap in a real
// in-memory DB.
func computeBudget(db *sql.DB, userID string) BudgetState {
	weekStart := startOfWeekUTC(time.Now())
	spent := sumSpendSince(db, userID, weekStart)
	cap := capForUser(db, userID)

	pct := 0.0
	if cap > 0 {
		pct = (spent / cap) * 100
	}
	verdict := "ok"
	switch {
	case cap <= 0:
		// No cap configured — treat as unlimited but flag in logs
		// so the operator notices the missing config.
		verdict = "ok"
	case pct >= 100:
		verdict = "blocked"
	case pct >= 80:
		verdict = "warn"
	}
	return BudgetState{
		UserID: userID, WeekStart: weekStart,
		SpentZAR: spent, CapZAR: cap,
		PctSpent: pct, Verdict: verdict,
	}
}

func sumSpendSince(db *sql.DB, userID string, since time.Time) float64 {
	if db == nil {
		return 0
	}
	dateStr := since.UTC().Format("2006-01-02")
	var sum sql.NullFloat64
	// cost_records.date is a YYYY-MM-DD string, so >= comparison
	// works lexically.
	_ = db.QueryRow(
		`SELECT COALESCE(SUM(amount_zar), 0) FROM cost_records
		 WHERE user_id = ? AND date >= ?`,
		userID, dateStr,
	).Scan(&sum)
	return sum.Float64
}

func capForUser(db *sql.DB, userID string) float64 {
	if db == nil {
		return DefaultWeeklyCapZAR
	}
	var cap sql.NullFloat64
	err := db.QueryRow(
		`SELECT weekly_zar_cap FROM user_budgets WHERE user_id = ?`,
		userID,
	).Scan(&cap)
	if err == sql.ErrNoRows {
		// No row for this user — fall through to global default.
		return DefaultWeeklyCapZAR
	}
	if err != nil || !cap.Valid {
		return DefaultWeeklyCapZAR
	}
	return cap.Float64
}

// SetCap upserts a per-user cap. Used by /api/v1/budgets routes
// (POST) and by tests. Pass userID="" or AnonymousUserID for the
// anonymous bucket.
func SetCap(db *sql.DB, userID string, capZAR float64) error {
	if userID == "" {
		userID = AnonymousUserID
	}
	_, err := db.Exec(
		`INSERT INTO user_budgets (user_id, weekly_zar_cap, updated_at)
		 VALUES (?, ?, datetime('now'))
		 ON CONFLICT(user_id) DO UPDATE SET
		   weekly_zar_cap = excluded.weekly_zar_cap,
		   updated_at = datetime('now')`,
		userID, capZAR,
	)
	if err != nil {
		return fmt.Errorf("set cap: %w", err)
	}
	return nil
}

// startOfWeekUTC returns Monday 00:00:00 UTC for the week of t.
// rain ops use ISO weeks (Mon-start) for reporting; aligning
// budget windows means a budget reset always happens at the same
// daily standup.
func startOfWeekUTC(t time.Time) time.Time {
	t = t.UTC()
	// time.Weekday: Sunday=0..Saturday=6. We want Monday=0..Sunday=6.
	dow := int(t.Weekday())
	if dow == 0 {
		dow = 7
	}
	mondayOffset := dow - 1
	t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return t.AddDate(0, 0, -mondayOffset)
}
