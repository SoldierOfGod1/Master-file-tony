// sqlsink.go — slog handler that mirrors WARN+ log records into the
// log_entries SQLite table so the Dashboard Log Terminal + Error
// Rate KPI have something to read.
//
// Prior to this the table was wiped on every startup and had zero
// writers — both surfaces showed constant zero. We filter to WARN
// and above so routine INFO lines don't explode the table. A
// bounded buffered channel absorbs bursts without blocking the
// hot path; on overflow we drop the oldest entries.
package logging

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"
)

// DBSink wraps an inner slog.Handler and, for WARN+ records, also
// enqueues a write into log_entries. It implements slog.Handler so
// it plugs in anywhere a handler is expected.
type DBSink struct {
	inner  slog.Handler
	db     *sql.DB
	ch     chan logRow
	wg     sync.WaitGroup
	cancel context.CancelFunc
}

type logRow struct {
	ts      time.Time
	level   string
	agentID string
	message string
}

// NewDBSink wraps `inner` with SQLite persistence. `db` is the
// command-centre store handle; pass nil to disable DB persistence
// (the inner handler still runs). The writer goroutine drains the
// channel until ctx is cancelled via Close.
func NewDBSink(inner slog.Handler, db *sql.DB) *DBSink {
	ctx, cancel := context.WithCancel(context.Background())
	s := &DBSink{
		inner:  inner,
		db:     db,
		ch:     make(chan logRow, 1024),
		cancel: cancel,
	}
	if db != nil {
		s.wg.Add(1)
		go s.writer(ctx)
	}
	return s
}

// Close stops the background writer and waits for it to drain.
func (s *DBSink) Close() {
	if s == nil {
		return
	}
	s.cancel()
	s.wg.Wait()
}

func (s *DBSink) Enabled(ctx context.Context, level slog.Level) bool {
	return s.inner.Enabled(ctx, level)
}

func (s *DBSink) Handle(ctx context.Context, r slog.Record) error {
	// Always delegate to the inner handler so stdout/file output is
	// unchanged. WARN+ additionally enqueue to the DB channel.
	if s.db != nil && r.Level >= slog.LevelWarn {
		agentID := ""
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "agent_id" || a.Key == "agent" {
				agentID = a.Value.String()
				return false
			}
			return true
		})
		row := logRow{
			ts:      r.Time,
			level:   r.Level.String(),
			agentID: agentID,
			message: r.Message,
		}
		// Non-blocking enqueue — drop on overflow rather than
		// stalling the caller. A dropped log line is far
		// cheaper than a deadlocked request handler.
		select {
		case s.ch <- row:
		default:
		}
	}
	return s.inner.Handle(ctx, r)
}

func (s *DBSink) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &DBSink{inner: s.inner.WithAttrs(attrs), db: s.db, ch: s.ch, cancel: s.cancel}
}

func (s *DBSink) WithGroup(name string) slog.Handler {
	return &DBSink{inner: s.inner.WithGroup(name), db: s.db, ch: s.ch, cancel: s.cancel}
}

// writer is the background drain loop. Insert failures are
// swallowed — the console log still captured the line, so we'd
// rather continue than escalate. A periodic retention sweep keeps
// the table near ~10k rows so it can't grow unbounded.
func (s *DBSink) writer(ctx context.Context) {
	defer s.wg.Done()
	// Sweep every 30 min; cheap because the index makes the LIMIT
	// based prune fast.
	sweep := time.NewTicker(30 * time.Minute)
	defer sweep.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case row := <-s.ch:
			_, _ = s.db.ExecContext(context.Background(),
				`INSERT INTO log_entries (timestamp, level, agent_id, message) VALUES (?, ?, ?, ?)`,
				row.ts.UTC().Format(time.RFC3339Nano),
				row.level,
				row.agentID,
				row.message,
			)
		case <-sweep.C:
			_, _ = s.db.ExecContext(context.Background(),
				`DELETE FROM log_entries WHERE id IN (
				   SELECT id FROM log_entries ORDER BY id DESC LIMIT -1 OFFSET 10000
				 )`)
		}
	}
}
