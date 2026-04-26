package customer

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// newAuditTestDB spins up an in-memory SQLite, creates the
// imsi_lookup_audit table, and hands back a handle. Mirrors the
// migration in store/migrations.go — kept inline here because
// importing the store package from a test creates an import cycle.
func newAuditTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE imsi_lookup_audit (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		at TEXT NOT NULL DEFAULT (datetime('now')),
		individual_id TEXT NOT NULL,
		source TEXT NOT NULL,
		winning_phase TEXT NOT NULL,
		imsi_count INTEGER NOT NULL DEFAULT 0,
		response_code INTEGER NOT NULL DEFAULT 200,
		reason TEXT NOT NULL DEFAULT '',
		incident_id TEXT
	)`)
	if err != nil {
		t.Fatalf("create audit table: %v", err)
	}
	return db
}

func TestWriteIMSIAudit_HappyPath(t *testing.T) {
	db := newAuditTestDB(t)
	if err := writeIMSIAudit(context.Background(), db, "ind-123", "cdr_usage", "product_path", 3); err != nil {
		t.Fatalf("write: %v", err)
	}
	var (
		individualID, source, phase string
		count                       int
	)
	err := db.QueryRow(`SELECT individual_id, source, winning_phase, imsi_count FROM imsi_lookup_audit`).
		Scan(&individualID, &source, &phase, &count)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if individualID != "ind-123" || source != "cdr_usage" || phase != "product_path" || count != 3 {
		t.Errorf("row mismatch: got (%s, %s, %s, %d)", individualID, source, phase, count)
	}
}

func TestWriteIMSIAudit_NilDBIsNoOp(t *testing.T) {
	// Nil DB must not panic and must not error — callers like the
	// resolveIMSIs deferred audit can run before the manager is
	// fully initialised in tests.
	if err := writeIMSIAudit(context.Background(), nil, "ind-x", "src", "phase", 0); err != nil {
		t.Errorf("nil db should be silent no-op, got: %v", err)
	}
}

func TestWriteIMSIAudit_EmptyIndividualIsNoOp(t *testing.T) {
	db := newAuditTestDB(t)
	// Empty individual_id is a sentinel — don't pollute the audit log
	// with rows that can't be tied back to a customer.
	if err := writeIMSIAudit(context.Background(), db, "", "src", "phase", 0); err != nil {
		t.Errorf("empty id should be silent no-op, got: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM imsi_lookup_audit`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 rows for empty individual_id, got %d", count)
	}
}

func TestWriteIMSIAudit_PhaseLabels(t *testing.T) {
	db := newAuditTestDB(t)
	// All five cascade phases plus "exhausted" must round-trip.
	// Mirrors the labels written by resolveIMSIs's deferred audit.
	phases := []string{"override", "product_path", "view_account", "view_msisdn", "view_subscriber", "exhausted"}
	for i, p := range phases {
		if err := writeIMSIAudit(context.Background(), db, "ind", "src", p, i); err != nil {
			t.Fatalf("write phase %q: %v", p, err)
		}
	}
	var got int
	if err := db.QueryRow(`SELECT COUNT(DISTINCT winning_phase) FROM imsi_lookup_audit`).Scan(&got); err != nil {
		t.Fatalf("count distinct: %v", err)
	}
	if got != len(phases) {
		t.Errorf("expected %d distinct phases, got %d", len(phases), got)
	}
}

func TestWriteIMSIAudit_IncidentIDFromContext(t *testing.T) {
	db := newAuditTestDB(t)
	ctx := WithIncidentID(context.Background(), "INC-1234")
	if err := writeIMSIAudit(ctx, db, "ind-1", "cdr_usage", "product_path", 2); err != nil {
		t.Fatalf("write: %v", err)
	}
	var got string
	if err := db.QueryRow(`SELECT incident_id FROM imsi_lookup_audit WHERE individual_id = 'ind-1'`).Scan(&got); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got != "INC-1234" {
		t.Errorf("expected incident_id INC-1234, got %q", got)
	}
}

func TestWriteIMSIAudit_NoIncidentInContext(t *testing.T) {
	db := newAuditTestDB(t)
	if err := writeIMSIAudit(context.Background(), db, "ind-2", "usage", "view_account", 1); err != nil {
		t.Fatalf("write: %v", err)
	}
	var got string
	if err := db.QueryRow(`SELECT COALESCE(incident_id,'') FROM imsi_lookup_audit WHERE individual_id = 'ind-2'`).Scan(&got); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty incident_id when not set, got %q", got)
	}
}

func TestIncidentIDFromContext(t *testing.T) {
	if got := IncidentIDFromContext(context.Background()); got != "" {
		t.Errorf("expected empty when unset, got %q", got)
	}
	ctx := WithIncidentID(context.Background(), "INC-1")
	if got := IncidentIDFromContext(ctx); got != "INC-1" {
		t.Errorf("expected INC-1, got %q", got)
	}
	// Empty id should NOT poison the context with a zero-value entry.
	ctx2 := WithIncidentID(context.Background(), "")
	if got := IncidentIDFromContext(ctx2); got != "" {
		t.Errorf("empty id should be a no-op, got %q", got)
	}
}
