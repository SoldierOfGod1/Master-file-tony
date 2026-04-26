package chat

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func newAutoSummaryDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE conversations (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL DEFAULT '',
		project_dir TEXT NOT NULL DEFAULT '',
		source TEXT NOT NULL DEFAULT 'ui',
		status TEXT NOT NULL DEFAULT 'active',
		user_id TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		conversation_id TEXT NOT NULL,
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		source TEXT NOT NULL DEFAULT 'ui',
		metadata TEXT DEFAULT '{}',
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

func seedConv(t *testing.T, db *sql.DB, convID, userID string, msgs ...convMessage) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO conversations (id, user_id) VALUES (?, ?)`, convID, userID); err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		if _, err := db.Exec(`INSERT INTO messages (conversation_id, role, content) VALUES (?, ?, ?)`, convID, m.Role, m.Content); err != nil {
			t.Fatal(err)
		}
	}
}

func quietSummaryLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestAutoSummariser_DeterministicHappyPath(t *testing.T) {
	db := newAutoSummaryDB(t)
	seedConv(t, db, "c-1", "alice",
		convMessage{Role: "user", Content: "Why was Khumalo's payment declined?"},
		convMessage{Role: "assistant", Content: "Decline reason 51 — insufficient funds. Plan needs renewal."},
	)
	s := NewAutoSummariser(db, quietSummaryLog(), nil)
	s.SummariseAndStore(context.Background(), "c-1")

	got := LoadRecentMemory(db, "alice")
	if len(got) != 1 {
		t.Fatalf("expected 1 memory entry, got %d", len(got))
	}
	if !strings.Contains(got[0].Body, "Khumalo") || !strings.Contains(got[0].Body, "Decline reason") {
		t.Errorf("summary missed key terms: %q", got[0].Body)
	}
}

func TestAutoSummariser_AnonymousConvSkipped(t *testing.T) {
	db := newAutoSummaryDB(t)
	seedConv(t, db, "c-anon", "",
		convMessage{Role: "user", Content: "any prompt"},
		convMessage{Role: "assistant", Content: "any reply"},
	)
	s := NewAutoSummariser(db, quietSummaryLog(), nil)
	s.SummariseAndStore(context.Background(), "c-anon")

	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM agent_memory`).Scan(&count)
	if count != 0 {
		t.Errorf("anonymous conv should not write memory, got %d rows", count)
	}
}

func TestAutoSummariser_EmptyConvNoOp(t *testing.T) {
	db := newAutoSummaryDB(t)
	seedConv(t, db, "c-empty", "alice")
	s := NewAutoSummariser(db, quietSummaryLog(), nil)
	s.SummariseAndStore(context.Background(), "c-empty")

	got := LoadRecentMemory(db, "alice")
	if len(got) != 0 {
		t.Errorf("empty conv should not write memory, got %v", got)
	}
}

func TestAutoSummariser_NilSafety(t *testing.T) {
	var s *AutoSummariser
	s.SummariseAndStore(context.Background(), "c-x") // must not panic
}

func TestAutoSummariseDeterministic_TruncatesLong(t *testing.T) {
	long := strings.Repeat("x", 500)
	s := &AutoSummariser{}
	body := s.summariseDeterministic([]convMessage{
		{Role: "user", Content: long},
		{Role: "assistant", Content: long},
	})
	// "Asked: " + 120 + "…" + ". Concluded: " + 120 + "…" — well
	// under 300 chars even with the framing prefix.
	if len(body) > 300 {
		t.Errorf("deterministic summary too long: %d chars", len(body))
	}
}

func TestAutoSummariseDeterministic_RolesIgnoredOtherThanUserAssistant(t *testing.T) {
	s := &AutoSummariser{}
	body := s.summariseDeterministic([]convMessage{
		{Role: "system", Content: "system-only message"},
	})
	if body != "" {
		t.Errorf("system-only conv should produce empty summary, got %q", body)
	}
}
