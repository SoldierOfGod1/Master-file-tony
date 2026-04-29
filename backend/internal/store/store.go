package store

import (
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	DB  *sql.DB
	Log *slog.Logger
}

func New(dbPath string, log *slog.Logger) (*Store, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}

	// SQLite is in WAL mode (DSN above) so concurrent readers are
	// safe and a single writer can run alongside them. Capping the
	// pool at 1 — as we used to — turned every endpoint that touches
	// SQLite into a queue: with the DB monitor + chat buffer + alert
	// sink + sales poll + UI fetches all in flight at once, a single
	// 600s request tail blocked the whole server (see /api/v1/connections,
	// /platforms/databases, /platforms/incidents stacking up to 9-10
	// minute durations in the log).
	//
	// Allow up to 8 concurrent connections — well above the
	// busy-write contention threshold for our workload but small
	// enough that SQLite's lock contention stays bounded. Idle
	// connections are kept warm so the next request doesn't pay
	// the dial cost.
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxIdleTime(5 * time.Minute)

	s := &Store{DB: db, Log: log}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}

	return s, nil
}

func (s *Store) Close() error {
	return s.DB.Close()
}

func (s *Store) migrate() error {
	for _, m := range migrations {
		if _, err := s.DB.Exec(m); err != nil {
			// sqlite has no IF NOT EXISTS on ALTER TABLE ADD COLUMN; treat
			// "duplicate column" as a successful no-op so migrations stay
			// idempotent across restarts.
			msg := strings.ToLower(err.Error())
			if strings.Contains(msg, "duplicate column") {
				continue
			}
			s.Log.Error("migration failed", "error", err, "sql", m[:min(len(m), 80)])
			return err
		}
	}
	s.Log.Info("database migrations complete", "count", len(migrations))
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
