package chat

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newApprovalDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE approvals (
		id TEXT PRIMARY KEY,
		status TEXT NOT NULL DEFAULT 'pending',
		created_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		t.Fatal(err)
	}
	return db
}

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestApprovalPoll_AlreadyResolved_ReturnsImmediately(t *testing.T) {
	db := newApprovalDB(t)
	_, _ = db.Exec(`INSERT INTO approvals (id, status) VALUES ('a-1', 'approved')`)
	poll := buildApprovalPoll(db, quietLog())
	start := time.Now()
	status, resolved := poll(context.Background(), "a-1")
	if !resolved || status != "approved" {
		t.Errorf("expected approved/resolved, got %q/%v", status, resolved)
	}
	if time.Since(start) > 1*time.Second {
		t.Errorf("expected immediate return, took %v", time.Since(start))
	}
}

func TestApprovalPoll_PendingThenApproved(t *testing.T) {
	db := newApprovalDB(t)
	_, _ = db.Exec(`INSERT INTO approvals (id, status) VALUES ('a-2', 'pending')`)
	poll := buildApprovalPoll(db, quietLog())
	// Flip to approved after 3 seconds — within the 60s window but
	// well after the 2s tick so we exercise at least one iteration.
	go func() {
		time.Sleep(3 * time.Second)
		_, _ = db.Exec(`UPDATE approvals SET status='approved' WHERE id='a-2'`)
	}()
	status, resolved := poll(context.Background(), "a-2")
	if !resolved || status != "approved" {
		t.Errorf("expected approved/resolved, got %q/%v", status, resolved)
	}
}

func TestApprovalPoll_Rejected(t *testing.T) {
	db := newApprovalDB(t)
	_, _ = db.Exec(`INSERT INTO approvals (id, status) VALUES ('a-3', 'rejected')`)
	poll := buildApprovalPoll(db, quietLog())
	status, resolved := poll(context.Background(), "a-3")
	if !resolved || status != "rejected" {
		t.Errorf("expected rejected/resolved, got %q/%v", status, resolved)
	}
}

func TestApprovalPoll_ContextCancelled(t *testing.T) {
	db := newApprovalDB(t)
	_, _ = db.Exec(`INSERT INTO approvals (id, status) VALUES ('a-4', 'pending')`)
	poll := buildApprovalPoll(db, quietLog())
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(500 * time.Millisecond)
		cancel()
	}()
	status, resolved := poll(ctx, "a-4")
	if resolved {
		t.Errorf("expected unresolved on context cancel, got %q", status)
	}
}

func TestApprovalPoll_NilDBOrEmptyID(t *testing.T) {
	poll := buildApprovalPoll(nil, quietLog())
	if _, ok := poll(context.Background(), "any"); ok {
		t.Error("nil db should report not-resolved")
	}
	db := newApprovalDB(t)
	poll = buildApprovalPoll(db, quietLog())
	if _, ok := poll(context.Background(), ""); ok {
		t.Error("empty id should report not-resolved")
	}
}

func TestIsResolved(t *testing.T) {
	cases := map[string]bool{
		"pending":  false,
		"approved": true,
		"rejected": true,
		"snoozed":  false,
		"expired":  false,
		"":         false,
	}
	for in, want := range cases {
		if got := isResolved(in); got != want {
			t.Errorf("isResolved(%q) = %v, want %v", in, got, want)
		}
	}
}
