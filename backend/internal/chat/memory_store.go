package chat

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// MemoryEntry is one row from agent_memory. Phase D1 of the
// agent-orchestrator plan. Closes the "agent forgets every
// session" gap from the 2026-04-26 article review.
type MemoryEntry struct {
	ID        int64     `json:"id"`
	UserID    string    `json:"user_id"`
	Kind      string    `json:"kind"` // 'preference' | 'incident_context' | 'pattern' | 'note'
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

// MemoryLimit is the max rows injected into the system prompt.
// Keeps prompt overhead bounded; older entries stay in the table
// for /api/v1/memory listings but the agent only sees the recent
// window. Empirically 10 is enough — anything older is usually
// either stale or already summarised by a newer entry.
const MemoryLimit = 10

// LoadRecentMemory returns the user's most recent N entries,
// newest first. Used by the dispatcher to enrich the system
// prompt at the start of every agent run. Empty user_id returns
// nil — anonymous sessions have no per-user memory by design.
func LoadRecentMemory(db *sql.DB, userID string) []MemoryEntry {
	if db == nil || userID == "" {
		return nil
	}
	rows, err := db.Query(
		`SELECT id, user_id, kind, body, created_at
		   FROM agent_memory
		  WHERE user_id = ?
		  ORDER BY created_at DESC
		  LIMIT ?`,
		userID, MemoryLimit,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []MemoryEntry
	for rows.Next() {
		var e MemoryEntry
		var ts string
		if err := rows.Scan(&e.ID, &e.UserID, &e.Kind, &e.Body, &ts); err != nil {
			continue
		}
		if t, perr := time.Parse(time.RFC3339, ts); perr == nil {
			e.CreatedAt = t
		} else if t, perr := time.Parse("2006-01-02 15:04:05", ts); perr == nil {
			e.CreatedAt = t
		}
		out = append(out, e)
	}
	return out
}

// WriteMemory persists one entry. Returns the inserted id. Bodies
// over 2KB are truncated; the model can split into multiple
// entries if it has more to say.
func WriteMemory(db *sql.DB, userID, kind, body string) (int64, error) {
	if db == nil {
		return 0, fmt.Errorf("memory store: nil db")
	}
	if userID == "" {
		return 0, fmt.Errorf("memory store: anonymous users cannot remember")
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return 0, fmt.Errorf("memory store: empty body")
	}
	if len(body) > 2048 {
		body = body[:2048]
	}
	if kind == "" {
		kind = "note"
	}
	res, err := db.Exec(
		`INSERT INTO agent_memory (user_id, kind, body) VALUES (?, ?, ?)`,
		userID, kind, body,
	)
	if err != nil {
		return 0, fmt.Errorf("agent_memory insert: %w", err)
	}
	id, _ := res.LastInsertId()
	return id, nil
}

// ListMemory returns every memory row for a user (no LIMIT) so
// the operator UI can paginate or scroll. Used by the Memory CRUD
// route. Empty user_id returns all rows across all users —
// operator-only, gated by RAIN_SUPPORT_L2 at the route level.
func ListMemory(db *sql.DB, userID string) []MemoryEntry {
	if db == nil {
		return nil
	}
	var rows *sql.Rows
	var err error
	if userID == "" {
		rows, err = db.Query(
			`SELECT id, user_id, kind, body, created_at FROM agent_memory ORDER BY created_at DESC LIMIT 500`,
		)
	} else {
		rows, err = db.Query(
			`SELECT id, user_id, kind, body, created_at FROM agent_memory WHERE user_id = ? ORDER BY created_at DESC LIMIT 500`,
			userID,
		)
	}
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []MemoryEntry
	for rows.Next() {
		var e MemoryEntry
		var ts string
		if err := rows.Scan(&e.ID, &e.UserID, &e.Kind, &e.Body, &ts); err != nil {
			continue
		}
		if t, perr := time.Parse(time.RFC3339, ts); perr == nil {
			e.CreatedAt = t
		} else if t, perr := time.Parse("2006-01-02 15:04:05", ts); perr == nil {
			e.CreatedAt = t
		}
		out = append(out, e)
	}
	return out
}

// DeleteMemory removes a single entry. Returns true on success
// (the row existed and was deleted). Used by the operator's
// memory-CRUD UI to scrub stale or wrong entries.
func DeleteMemory(db *sql.DB, id int64) (bool, error) {
	if db == nil {
		return false, fmt.Errorf("memory store: nil db")
	}
	res, err := db.Exec(`DELETE FROM agent_memory WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// FormatForPrompt renders memory entries as a short bulleted
// block ready to paste into the system prompt. Returns "" if no
// entries — no point in adding empty headers to every prompt.
//
// Format is deliberately tight (one bullet per entry, kind tag
// in brackets) so the model sees recall as a structured input
// rather than free-form text. Tested empirically: shorter is
// better for tool-use steering.
func FormatForPrompt(entries []MemoryEntry) string {
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nRecall (most recent first, oldest cropped):\n")
	for _, e := range entries {
		b.WriteString("  - [")
		b.WriteString(e.Kind)
		b.WriteString("] ")
		b.WriteString(e.Body)
		b.WriteString("\n")
	}
	return b.String()
}
