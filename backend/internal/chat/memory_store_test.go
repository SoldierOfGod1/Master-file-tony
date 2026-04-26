package chat

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func newMemoryDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
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

func TestWriteMemory_HappyPath(t *testing.T) {
	db := newMemoryDB(t)
	id, err := WriteMemory(db, "alice", "preference", "prefers concise answers")
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if id <= 0 {
		t.Errorf("expected positive id, got %d", id)
	}
	got := LoadRecentMemory(db, "alice")
	if len(got) != 1 || got[0].Body != "prefers concise answers" || got[0].Kind != "preference" {
		t.Errorf("unexpected: %+v", got)
	}
}

func TestWriteMemory_AnonymousRefused(t *testing.T) {
	db := newMemoryDB(t)
	if _, err := WriteMemory(db, "", "note", "x"); err == nil {
		t.Error("expected error for anonymous user")
	}
}

func TestWriteMemory_EmptyBodyRefused(t *testing.T) {
	db := newMemoryDB(t)
	if _, err := WriteMemory(db, "alice", "note", "  \t  "); err == nil {
		t.Error("expected error for whitespace body")
	}
}

func TestWriteMemory_LargeBodyTruncated(t *testing.T) {
	db := newMemoryDB(t)
	huge := strings.Repeat("x", 5000)
	if _, err := WriteMemory(db, "alice", "note", huge); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := LoadRecentMemory(db, "alice")
	if len(got) != 1 || len(got[0].Body) != 2048 {
		t.Errorf("expected truncation to 2048, got len %d", len(got[0].Body))
	}
}

func TestLoadRecentMemory_OrderedNewestFirst(t *testing.T) {
	db := newMemoryDB(t)
	// 12 entries with strictly monotonic minute-level timestamps so
	// the lexical ordering of created_at matches insertion order.
	for i := 0; i < 12; i++ {
		mins := 10 + i // 10..21, all valid minutes within an hour
		_, err := db.Exec(
			`INSERT INTO agent_memory (user_id, kind, body, created_at) VALUES (?, ?, ?, ?)`,
			"alice", "note", "entry-"+string(rune('A'+i)),
			fmt.Sprintf("2026-04-26 10:%02d:00", mins),
		)
		if err != nil {
			t.Fatal(err)
		}
	}
	got := LoadRecentMemory(db, "alice")
	if len(got) != MemoryLimit {
		t.Errorf("expected %d entries, got %d", MemoryLimit, len(got))
	}
	// Newest = entry-L (i=11, minute=21). Oldest in window = entry-C (i=2).
	if got[0].Body != "entry-L" {
		t.Errorf("expected entry-L first (newest), got %q", got[0].Body)
	}
	if got[len(got)-1].Body != "entry-C" {
		t.Errorf("expected entry-C last (oldest in 10-row window), got %q", got[len(got)-1].Body)
	}
}

func TestLoadRecentMemory_PerUserIsolation(t *testing.T) {
	db := newMemoryDB(t)
	_, _ = WriteMemory(db, "alice", "note", "alice secret")
	_, _ = WriteMemory(db, "bob", "note", "bob secret")
	got := LoadRecentMemory(db, "alice")
	if len(got) != 1 || got[0].Body != "alice secret" {
		t.Errorf("alice leaked or missing: %+v", got)
	}
	got = LoadRecentMemory(db, "bob")
	if len(got) != 1 || got[0].Body != "bob secret" {
		t.Errorf("bob leaked or missing: %+v", got)
	}
}

func TestLoadRecentMemory_NilOrEmptyUser(t *testing.T) {
	db := newMemoryDB(t)
	if got := LoadRecentMemory(nil, "alice"); got != nil {
		t.Error("nil db should return nil")
	}
	if got := LoadRecentMemory(db, ""); got != nil {
		t.Error("empty user should return nil")
	}
}

func TestFormatForPrompt(t *testing.T) {
	got := FormatForPrompt(nil)
	if got != "" {
		t.Errorf("empty entries should produce empty string, got %q", got)
	}
	entries := []MemoryEntry{
		{Kind: "preference", Body: "prefers concise"},
		{Kind: "incident_context", Body: "P1 axiom 2026-04-24"},
	}
	out := FormatForPrompt(entries)
	if !strings.Contains(out, "[preference] prefers concise") {
		t.Errorf("missing first entry: %q", out)
	}
	if !strings.Contains(out, "[incident_context] P1 axiom") {
		t.Errorf("missing second entry: %q", out)
	}
	if !strings.Contains(out, "Recall") {
		t.Errorf("missing header: %q", out)
	}
}
