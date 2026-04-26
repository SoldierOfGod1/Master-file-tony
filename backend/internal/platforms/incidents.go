package platforms

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// Incident is an operational record created when severity >=
// Critical. It persists across restarts and has a small state
// machine: open → investigating (manual ack) → mitigated (auto on
// recovery) → resolved (manual close).
type Incident struct {
	ID          int64           `json:"id"`
	ServiceID   string          `json:"service_id"`
	Kind        string          `json:"kind"`
	Severity    Severity        `json:"severity"`
	Title       string          `json:"title"`
	Summary     string          `json:"summary,omitempty"`
	State       string          `json:"state"` // open | investigating | mitigated | resolved
	OpenedAt    time.Time       `json:"opened_at"`
	MitigatedAt *time.Time      `json:"mitigated_at,omitempty"`
	ResolvedAt  *time.Time      `json:"resolved_at,omitempty"`
	Timeline    []IncidentEvent `json:"timeline,omitempty"`
}

// IncidentEvent is one entry in an incident's timeline.
type IncidentEvent struct {
	ID      int64     `json:"id"`
	Kind    string    `json:"kind"`
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}

// StoredAlert is one row from service_alerts — what GET
// /api/v1/platforms/alerts returns.
type StoredAlert struct {
	ID         int64     `json:"id"`
	ServiceID  string    `json:"service_id"`
	Kind       string    `json:"kind"`
	Severity   Severity  `json:"severity"`
	Message    string    `json:"message"`
	Cause      string    `json:"cause,omitempty"`
	NextStep   string    `json:"next_step,omitempty"`
	State      string    `json:"state"`
	CreatedAt  time.Time `json:"created_at"`
	ResolvedAt time.Time `json:"resolved_at,omitempty"`
}

// SQLAlertSink persists every emitted alert and auto-creates
// incidents for severity >= Critical. Mitigation / resolution
// happens here too: a "recovered" alert for a service with an open
// incident automatically moves it to "mitigated".
type SQLAlertSink struct {
	db  *sql.DB
	log *slog.Logger
}

func NewSQLAlertSink(db *sql.DB, log *slog.Logger) *SQLAlertSink {
	return &SQLAlertSink{db: db, log: log}
}

// Emit writes the alert and handles incident side-effects.
func (s *SQLAlertSink) Emit(ctx context.Context, a Alert, prev *Status, cur Status) {
	if s == nil || s.db == nil {
		return
	}
	// Dedup: if an open alert of the same kind + service already
	// exists, don't spam — update its created_at so recent-failures
	// feeds still surface it. Otherwise INSERT.
	var existingID int64
	_ = s.db.QueryRowContext(ctx,
		`SELECT id FROM service_alerts WHERE service_id=? AND kind=? AND state='open' LIMIT 1`,
		a.ServiceID, a.Kind,
	).Scan(&existingID)

	nowStr := a.CreatedAt.UTC().Format(time.RFC3339Nano)
	if existingID > 0 {
		_, _ = s.db.ExecContext(ctx,
			`UPDATE service_alerts SET message=?, cause=?, next_step=?, severity=?, created_at=? WHERE id=?`,
			a.Message, a.Cause, a.NextStep, string(a.Severity), nowStr, existingID,
		)
	} else {
		_, _ = s.db.ExecContext(ctx,
			`INSERT INTO service_alerts
			   (service_id, kind, severity, message, cause, next_step, state, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, 'open', ?)`,
			a.ServiceID, a.Kind, string(a.Severity), a.Message, a.Cause, a.NextStep, nowStr,
		)
	}

	// Incident lifecycle.
	switch {
	case a.Kind == "recovered":
		s.markMitigated(ctx, a.ServiceID, a.CreatedAt)
		// Recovery also resolves any open alerts on the service.
		_, _ = s.db.ExecContext(ctx,
			`UPDATE service_alerts SET state='resolved', resolved_at=? WHERE service_id=? AND state='open'`,
			nowStr, a.ServiceID,
		)
	case severityWeight(a.Severity) >= severityWeight(SeverityCritical):
		s.openOrAppendIncident(ctx, a)
	}
}

func (s *SQLAlertSink) openOrAppendIncident(ctx context.Context, a Alert) {
	// One open incident per (service_id, kind). Reuse if one exists.
	var existingID int64
	_ = s.db.QueryRowContext(ctx,
		`SELECT id FROM service_incidents WHERE service_id=? AND kind=? AND state IN ('open','investigating') LIMIT 1`,
		a.ServiceID, a.Kind,
	).Scan(&existingID)

	at := a.CreatedAt.UTC().Format(time.RFC3339Nano)
	if existingID == 0 {
		res, err := s.db.ExecContext(ctx,
			`INSERT INTO service_incidents
			   (service_id, kind, severity, title, summary, state, opened_at)
			 VALUES (?, ?, ?, ?, ?, 'open', ?)`,
			a.ServiceID, a.Kind, string(a.Severity),
			a.Message, a.Cause, at,
		)
		if err != nil {
			if s.log != nil {
				s.log.Warn("incident insert", "error", err)
			}
			return
		}
		existingID, _ = res.LastInsertId()
		s.addEvent(ctx, existingID, "opened", fmt.Sprintf("incident opened (%s)", a.Severity), a.CreatedAt)
	} else {
		// Escalate severity if the new alert is higher.
		var currentSev string
		_ = s.db.QueryRowContext(ctx, `SELECT severity FROM service_incidents WHERE id=?`, existingID).Scan(&currentSev)
		if severityWeight(a.Severity) > severityWeight(Severity(currentSev)) {
			_, _ = s.db.ExecContext(ctx,
				`UPDATE service_incidents SET severity=? WHERE id=?`,
				string(a.Severity), existingID,
			)
			s.addEvent(ctx, existingID, "escalated",
				fmt.Sprintf("severity escalated %s → %s", currentSev, a.Severity), a.CreatedAt)
		}
		s.addEvent(ctx, existingID, "alert", a.Message, a.CreatedAt)
	}
}

func (s *SQLAlertSink) markMitigated(ctx context.Context, serviceID string, at time.Time) {
	atStr := at.UTC().Format(time.RFC3339Nano)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM service_incidents WHERE service_id=? AND state IN ('open','investigating')`,
		serviceID,
	)
	if err != nil {
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		_, _ = s.db.ExecContext(ctx,
			`UPDATE service_incidents SET state='mitigated', mitigated_at=? WHERE id=?`,
			atStr, id,
		)
		s.addEvent(ctx, id, "mitigated", "service recovered", at)
	}
}

func (s *SQLAlertSink) addEvent(ctx context.Context, incidentID int64, kind, message string, at time.Time) {
	_, _ = s.db.ExecContext(ctx,
		`INSERT INTO service_incident_events (incident_id, kind, message, at) VALUES (?, ?, ?, ?)`,
		incidentID, kind, message, at.UTC().Format(time.RFC3339Nano),
	)
}

// ---- Query helpers used by the HTTP routes ----

// ListAlerts returns alerts ordered newest-first. State filter is
// optional ("" for all).
func ListAlerts(ctx context.Context, db *sql.DB, state string, limit int) ([]StoredAlert, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT id, service_id, kind, severity, message, cause, next_step, state, created_at, resolved_at
	        FROM service_alerts`
	args := []any{}
	if state != "" {
		q += ` WHERE state = ?`
		args = append(args, state)
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StoredAlert
	for rows.Next() {
		var a StoredAlert
		var created, resolved string
		var severity string
		if err := rows.Scan(&a.ID, &a.ServiceID, &a.Kind, &severity, &a.Message,
			&a.Cause, &a.NextStep, &a.State, &created, &resolved); err != nil {
			continue
		}
		a.Severity = Severity(severity)
		if t, err := time.Parse(time.RFC3339Nano, created); err == nil {
			a.CreatedAt = t
		}
		if resolved != "" {
			if t, err := time.Parse(time.RFC3339Nano, resolved); err == nil {
				a.ResolvedAt = t
			}
		}
		out = append(out, a)
	}
	return out, nil
}

// ListIncidents returns the latest N incidents with their timeline
// events inlined. Callers render directly.
func ListIncidents(ctx context.Context, db *sql.DB, limit int) ([]Incident, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, service_id, kind, severity, title, summary, state,
		       opened_at, mitigated_at, resolved_at
		  FROM service_incidents
		 ORDER BY opened_at DESC
		 LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Incident
	for rows.Next() {
		var inc Incident
		var severity string
		var opened, mitigated, resolved string
		if err := rows.Scan(&inc.ID, &inc.ServiceID, &inc.Kind, &severity,
			&inc.Title, &inc.Summary, &inc.State,
			&opened, &mitigated, &resolved); err != nil {
			continue
		}
		inc.Severity = Severity(severity)
		if t, err := time.Parse(time.RFC3339Nano, opened); err == nil {
			inc.OpenedAt = t
		}
		if mitigated != "" {
			if t, err := time.Parse(time.RFC3339Nano, mitigated); err == nil {
				inc.MitigatedAt = &t
			}
		}
		if resolved != "" {
			if t, err := time.Parse(time.RFC3339Nano, resolved); err == nil {
				inc.ResolvedAt = &t
			}
		}
		inc.Timeline = loadTimeline(ctx, db, inc.ID)
		out = append(out, inc)
	}
	return out, nil
}

func loadTimeline(ctx context.Context, db *sql.DB, incidentID int64) []IncidentEvent {
	rows, err := db.QueryContext(ctx,
		`SELECT id, kind, message, at FROM service_incident_events WHERE incident_id=? ORDER BY at ASC`,
		incidentID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []IncidentEvent
	for rows.Next() {
		var ev IncidentEvent
		var at string
		if err := rows.Scan(&ev.ID, &ev.Kind, &ev.Message, &at); err != nil {
			continue
		}
		if t, err := time.Parse(time.RFC3339Nano, at); err == nil {
			ev.At = t
		}
		out = append(out, ev)
	}
	return out
}

// AckIncident moves an open incident to "investigating" and records
// a timeline event. No-op on unknown id.
func AckIncident(ctx context.Context, db *sql.DB, id int64, note string) error {
	_, err := db.ExecContext(ctx,
		`UPDATE service_incidents SET state='investigating' WHERE id=? AND state='open'`,
		id,
	)
	if err != nil {
		return err
	}
	msg := "acknowledged — investigating"
	if note != "" {
		msg = msg + " · " + note
	}
	_, _ = db.ExecContext(ctx,
		`INSERT INTO service_incident_events (incident_id, kind, message, at) VALUES (?, 'ack', ?, ?)`,
		id, msg, time.Now().UTC().Format(time.RFC3339Nano),
	)
	return nil
}

// ResolveIncident closes an incident regardless of current state.
func ResolveIncident(ctx context.Context, db *sql.DB, id int64, note string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.ExecContext(ctx,
		`UPDATE service_incidents SET state='resolved', resolved_at=? WHERE id=?`,
		now, id,
	)
	if err != nil {
		return err
	}
	msg := "resolved"
	if note != "" {
		msg = msg + " · " + note
	}
	_, _ = db.ExecContext(ctx,
		`INSERT INTO service_incident_events (incident_id, kind, message, at) VALUES (?, 'resolved', ?, ?)`,
		id, msg, now,
	)
	return nil
}
