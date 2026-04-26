package chat

import (
	"database/sql"
	"io"
	"log/slog"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newBudgetTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE cost_records (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		date TEXT NOT NULL,
		model_name TEXT NOT NULL,
		amount_zar REAL NOT NULL,
		tokens_used INTEGER NOT NULL DEFAULT 0,
		conversation_id TEXT,
		input_tokens INTEGER NOT NULL DEFAULT 0,
		output_tokens INTEGER NOT NULL DEFAULT 0,
		user_id TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE user_budgets (
		user_id TEXT PRIMARY KEY,
		weekly_zar_cap REAL NOT NULL DEFAULT 0,
		updated_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestBudget_NoSpendNoCap_Default(t *testing.T) {
	db := newBudgetTestDB(t)
	gate := NewBudgetGate(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	st := gate.Check("alice")
	if st.UserID != "alice" {
		t.Errorf("user mismatch: %v", st)
	}
	if st.SpentZAR != 0 {
		t.Errorf("expected 0 spend, got %v", st.SpentZAR)
	}
	if st.CapZAR != DefaultWeeklyCapZAR {
		t.Errorf("expected default cap %v, got %v", DefaultWeeklyCapZAR, st.CapZAR)
	}
	if st.Verdict != "ok" {
		t.Errorf("expected ok, got %v", st.Verdict)
	}
}

func TestBudget_OverCap_Blocks(t *testing.T) {
	db := newBudgetTestDB(t)
	if err := SetCap(db, "alice", 10); err != nil {
		t.Fatal(err)
	}
	insertSpend(t, db, "alice", 12.50, time.Now().UTC())
	gate := NewBudgetGate(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	st := gate.Check("alice")
	if st.Verdict != "blocked" {
		t.Errorf("expected blocked at 125%% spend, got %v (%.2f%%)", st.Verdict, st.PctSpent)
	}
}

func TestBudget_AtEightyPercent_Warns(t *testing.T) {
	db := newBudgetTestDB(t)
	if err := SetCap(db, "alice", 10); err != nil {
		t.Fatal(err)
	}
	insertSpend(t, db, "alice", 8.00, time.Now().UTC())
	gate := NewBudgetGate(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	st := gate.Check("alice")
	if st.Verdict != "warn" {
		t.Errorf("expected warn at 80%%, got %v (%.2f%%)", st.Verdict, st.PctSpent)
	}
}

func TestBudget_AnonymousBucket(t *testing.T) {
	db := newBudgetTestDB(t)
	gate := NewBudgetGate(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	st := gate.Check("")
	if st.UserID != AnonymousUserID {
		t.Errorf("expected anonymous bucket, got %v", st.UserID)
	}
}

func TestBudget_PerUserIsolation(t *testing.T) {
	db := newBudgetTestDB(t)
	_ = SetCap(db, "alice", 10)
	_ = SetCap(db, "bob", 100)
	insertSpend(t, db, "alice", 9.50, time.Now().UTC())
	insertSpend(t, db, "bob", 9.50, time.Now().UTC())
	gate := NewBudgetGate(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if got := gate.Check("alice").Verdict; got != "warn" {
		t.Errorf("alice verdict: got %v", got)
	}
	if got := gate.Check("bob").Verdict; got != "ok" {
		t.Errorf("bob verdict: got %v", got)
	}
}

func TestBudget_OnlyCountsThisWeek(t *testing.T) {
	db := newBudgetTestDB(t)
	_ = SetCap(db, "alice", 10)
	// Spend from 30 days ago should not count.
	insertSpend(t, db, "alice", 50.00, time.Now().AddDate(0, 0, -30))
	// Today's tiny spend should be all that's counted.
	insertSpend(t, db, "alice", 1.00, time.Now())
	gate := NewBudgetGate(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	st := gate.Check("alice")
	if st.SpentZAR != 1.00 {
		t.Errorf("expected only this-week spend, got %v (week start %v)", st.SpentZAR, st.WeekStart)
	}
}

func TestBudget_CacheHit(t *testing.T) {
	db := newBudgetTestDB(t)
	_ = SetCap(db, "alice", 10)
	gate := NewBudgetGate(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	first := gate.Check("alice")
	// Mutate spend after the first read; second call should still
	// see the cached value.
	insertSpend(t, db, "alice", 999.00, time.Now())
	second := gate.Check("alice")
	if second.SpentZAR != first.SpentZAR {
		t.Errorf("cache miss: got %v, expected cached %v", second.SpentZAR, first.SpentZAR)
	}
	gate.Invalidate("alice")
	third := gate.Check("alice")
	if third.SpentZAR == first.SpentZAR {
		t.Errorf("invalidate had no effect")
	}
}

func TestStartOfWeekUTC(t *testing.T) {
	// Wed 2026-04-22 → Mon 2026-04-20.
	wed := time.Date(2026, 4, 22, 14, 30, 0, 0, time.UTC)
	got := startOfWeekUTC(wed)
	want := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("Wed: got %v, want %v", got, want)
	}
	// Sun 2026-04-26 → Mon 2026-04-20 (rolls back, not forward).
	sun := time.Date(2026, 4, 26, 23, 59, 0, 0, time.UTC)
	got = startOfWeekUTC(sun)
	if !got.Equal(want) {
		t.Errorf("Sun: got %v, want %v", got, want)
	}
}

func insertSpend(t *testing.T, db *sql.DB, userID string, zar float64, when time.Time) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO cost_records (date, model_name, amount_zar, tokens_used, user_id)
		 VALUES (?, ?, ?, ?, ?)`,
		when.UTC().Format("2006-01-02"), "haiku", zar, 1000, userID,
	)
	if err != nil {
		t.Fatal(err)
	}
}
