package store

import (
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

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

	db.SetMaxOpenConns(1)

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
