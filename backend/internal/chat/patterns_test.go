package chat

import (
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func newPatternsDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE conversations (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE agent_memory (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id TEXT NOT NULL,
		kind TEXT NOT NULL DEFAULT 'note',
		body TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedMemory(t *testing.T, db *sql.DB, user, kind, body string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO agent_memory (user_id, kind, body) VALUES (?, ?, ?)`,
		user, kind, body,
	); err != nil {
		t.Fatal(err)
	}
}

func seedConvForPatterns(t *testing.T, db *sql.DB, id, user string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO conversations (id, user_id) VALUES (?, ?)`,
		id, user,
	); err != nil {
		t.Fatal(err)
	}
}

func TestAggregatePatterns_NilDBSafe(t *testing.T) {
	got := AggregatePatterns(nil)
	if got.GeneratedAt.IsZero() {
		t.Error("GeneratedAt should always be populated")
	}
	if got.ConversationsByDay != nil || got.MemoryByKind != nil ||
		got.TopKeywordStems != nil {
		t.Error("nil db should yield empty slices")
	}
}

func TestActiveUsers7d_KAnonSuppressedBelowThree(t *testing.T) {
	db := newPatternsDB(t)
	// Two distinct users — below kAnonMin=3
	seedConvForPatterns(t, db, "c-1", "alice")
	seedConvForPatterns(t, db, "c-2", "bob")

	got := AggregatePatterns(db)
	if got.ActiveUsers7d != 0 {
		t.Errorf("expected suppressed count=0, got %d", got.ActiveUsers7d)
	}
	if !got.ActiveUsers7dSuppressed {
		t.Error("expected suppressed flag true when N < 3")
	}
}

func TestActiveUsers7d_PassesAtThreshold(t *testing.T) {
	db := newPatternsDB(t)
	for _, u := range []string{"alice", "bob", "carol"} {
		seedConvForPatterns(t, db, "c-"+u, u)
	}
	got := AggregatePatterns(db)
	if got.ActiveUsers7d != 3 {
		t.Errorf("expected 3, got %d", got.ActiveUsers7d)
	}
	if got.ActiveUsers7dSuppressed {
		t.Error("expected suppressed flag false at threshold")
	}
}

func TestTopKeywordStems_KAnonExcludesSingletonVocab(t *testing.T) {
	db := newPatternsDB(t)
	// "axiom" appears in 3 distinct users — should pass.
	seedMemory(t, db, "alice", "note", "axiom outage today")
	seedMemory(t, db, "bob", "note", "checking axiom incident")
	seedMemory(t, db, "carol", "note", "axiom recovery")
	// "rare-quirk-keyword" appears only for one user — must be
	// suppressed under k-anon.
	seedMemory(t, db, "alice", "note", "uniqueparticulartermxyz mention")

	got := AggregatePatterns(db)
	var foundAxiom, foundUnique bool
	for _, s := range got.TopKeywordStems {
		if s.Stem == "axiom" {
			foundAxiom = true
			if s.UserBuckets < kAnonMin {
				t.Errorf("axiom should have ≥ %d users, got %d", kAnonMin, s.UserBuckets)
			}
		}
		if s.Stem == "uniqueparticulartermxyz" {
			foundUnique = true
		}
	}
	if !foundAxiom {
		t.Error("axiom (3 users) should appear in top stems")
	}
	if foundUnique {
		t.Error("singleton-user stem must be suppressed under k-anon")
	}
}

func TestTopKeywordStems_StopWordsFiltered(t *testing.T) {
	db := newPatternsDB(t)
	for _, u := range []string{"a", "b", "c", "d"} {
		seedMemory(t, db, u, "note", "this user asked about that")
	}
	got := AggregatePatterns(db)
	for _, s := range got.TopKeywordStems {
		if stopWords[s.Stem] {
			t.Errorf("stop-word %q leaked into top stems", s.Stem)
		}
	}
}

func TestTokenise_DropsShortAndNonAlpha(t *testing.T) {
	in := "Axiom-prod 12345 ok? db123 outage."
	got := tokenise(in)
	want := map[string]bool{"axiom": true, "prod": true, "outage": true}
	if len(got) != len(want) {
		t.Errorf("expected 3 tokens, got %v", got)
	}
	for _, tok := range got {
		if !want[tok] {
			t.Errorf("unexpected token %q", tok)
		}
	}
}

func TestMemoryByKind_AggregatesAcrossUsers(t *testing.T) {
	db := newPatternsDB(t)
	seedMemory(t, db, "alice", "note", "x")
	seedMemory(t, db, "bob", "note", "y")
	seedMemory(t, db, "alice", "preference", "z")

	got := AggregatePatterns(db)
	if len(got.MemoryByKind) != 2 {
		t.Fatalf("expected 2 kinds, got %d", len(got.MemoryByKind))
	}
	// Sorted desc by count: note=2, preference=1.
	if got.MemoryByKind[0].Kind != "note" || got.MemoryByKind[0].Count != 2 {
		t.Errorf("expected note=2 first, got %+v", got.MemoryByKind[0])
	}
}

func TestConversationsByDay_LimitsTo30Days(t *testing.T) {
	db := newPatternsDB(t)
	// Today (default created_at).
	seedConvForPatterns(t, db, "c-today", "alice")
	// 60 days ago — out of window.
	if _, err := db.Exec(
		`INSERT INTO conversations (id, user_id, created_at) VALUES (?, ?, datetime('now', '-60 days'))`,
		"c-old", "bob",
	); err != nil {
		t.Fatal(err)
	}
	got := AggregatePatterns(db)
	if len(got.ConversationsByDay) != 1 {
		t.Errorf("expected 1 day in window, got %d", len(got.ConversationsByDay))
	}
	for _, d := range got.ConversationsByDay {
		if strings.Contains(d.Day, "1970") {
			t.Errorf("zero-day leaked: %v", d)
		}
	}
}
