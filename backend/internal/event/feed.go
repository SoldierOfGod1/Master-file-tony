// feed.go — activity-feed publisher. Call sites write one row per
// meaningful event (task created, customer looked up, service
// incident opened, etc) and a WebSocket frame fans out to every
// connected client so the Activity Feed tab updates live.
//
// Prior to this, feed_events had zero writers and the Dashboard
// "Activity Feed" card always showed zero. The table has been
// present in migrations since the beginning — the ghost was just
// that nothing ever populated it.
package event

import (
	"context"
	"database/sql"
	"log/slog"
	"time"
)

// FeedKind is a short stable identifier for the event type.
// Kept as a string so callers can pass a literal without an enum
// import, but the constants below document the accepted values.
type FeedKind string

const (
	FeedKindTask        FeedKind = "task"
	FeedKindCustomer    FeedKind = "customer"
	FeedKindChat        FeedKind = "chat"
	FeedKindIncident    FeedKind = "incident"
	FeedKindAlert       FeedKind = "alert"
	FeedKindRecommendation FeedKind = "recommendation"
	FeedKindSystem      FeedKind = "system"
)

// Publisher persists feed events into SQLite and republishes them
// on the in-process event bus so the WebSocket hub pushes to
// connected clients. It is intentionally fire-and-forget — a
// failed write never blocks the caller's main path.
type Publisher struct {
	db  *sql.DB
	bus *Bus
	log *slog.Logger
}

// NewPublisher wires the dependencies. Both db and bus may be nil —
// the publisher no-ops in that case, which keeps tests easy.
func NewPublisher(db *sql.DB, bus *Bus, log *slog.Logger) *Publisher {
	return &Publisher{db: db, bus: bus, log: log}
}

// Publish writes one row to feed_events and broadcasts a
// "feed.event" message on the bus. agentID is an optional
// attribution field (empty string is fine). message is a short,
// human-readable line that'll render verbatim on the Activity
// Feed page.
func (p *Publisher) Publish(ctx context.Context, kind FeedKind, agentID, message string) {
	if p == nil {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if p.db != nil {
		if _, err := p.db.ExecContext(ctx,
			`INSERT INTO feed_events (time, type, agent_id, message) VALUES (?, ?, ?, ?)`,
			now, string(kind), agentID, message,
		); err != nil && p.log != nil {
			// Intentionally debug — the feed must never pollute
			// error logs if SQLite is momentarily busy.
			p.log.Debug("feed publish", "error", err, "kind", kind)
		}
	}
	if p.bus != nil {
		p.bus.PublishJSON("feed.event", map[string]any{
			"time":     now,
			"type":     string(kind),
			"agent_id": agentID,
			"message":  message,
		})
	}
}
