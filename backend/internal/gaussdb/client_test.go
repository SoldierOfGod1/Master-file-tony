package gaussdb

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

// TestPlaceholderSQLGuard documents the contract the CEO-review locked
// in: while the SQL constants in queries.go are still placeholders,
// the client refuses to serve any answer rather than returning
// plausible-but-wrong numbers from invalid SQL.
//
// When the operator pastes the four real queries AND flips
// PlaceholderSQL = false, this test will need to be updated (the
// PlaceholderSQL branch should never fire post-paste). That update is
// the deliberate signal that the placeholder went away — easier to
// notice in a diff than a removed sentinel buried in a constants file.
func TestPlaceholderSQLGuard(t *testing.T) {
	if !PlaceholderSQL {
		t.Skip("PlaceholderSQL is false — operator has pasted real queries; sentinel test no longer applies")
	}

	c := NewClient(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Available() must surface ErrPlaceholderSQL before checking the
	// store — even if no connection is registered yet, the sentinel
	// is the more actionable error.
	ok, err := c.Available()
	if ok {
		t.Fatalf("Available() returned ok=true while PlaceholderSQL=%v", PlaceholderSQL)
	}
	if !errors.Is(err, ErrPlaceholderSQL) {
		t.Fatalf("Available() error = %v, want ErrPlaceholderSQL", err)
	}

	// UsageSummary() must short-circuit on the same sentinel without
	// touching the pool — the test passes a nil store so any pool
	// access would panic, proving the guard fires first.
	_, err = c.UsageSummary(context.Background(), "+27744432221")
	if !errors.Is(err, ErrPlaceholderSQL) {
		t.Fatalf("UsageSummary() error = %v, want ErrPlaceholderSQL", err)
	}
}

// TestPreferredSourceDefaults asserts that the env-driven dispatcher
// in axiomapi_routes.go falls through to axiom-api when USAGE_SOURCE
// is unset or unrecognised. The function lives in the server package
// but its contract matters here: anyone reading queries.go who sees
// PlaceholderSQL=true should also know that USAGE_SOURCE=gaussdb
// returns 503 — they're paired guards.
//
// (Kept here as documentation; the actual function is unexported in
// server. Replace with a real call once we lift it into a shared
// helpers package.)
func TestPreferredSourceDefaults(t *testing.T) {
	t.Skip("documentation-only — preferredUsageSource lives in package server")
}
