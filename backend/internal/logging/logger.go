package logging

import (
	"database/sql"
	"log/slog"
	"os"
)

func NewLogger(level string, serviceName string) *slog.Logger {
	return buildLogger(level, serviceName, nil)
}

// NewLoggerWithDB is NewLogger + a DBSink that mirrors WARN+ records
// into log_entries. Pass nil db to disable DB persistence.
func NewLoggerWithDB(level string, serviceName string, db *sql.DB) (*slog.Logger, *DBSink) {
	if db == nil {
		return buildLogger(level, serviceName, nil), nil
	}
	inner := jsonHandler(level)
	sink := NewDBSink(inner, db)
	return slog.New(sink).With("service", serviceName), sink
}

func buildLogger(level, serviceName string, _ *sql.DB) *slog.Logger {
	return slog.New(jsonHandler(level)).With("service", serviceName)
}

func jsonHandler(level string) slog.Handler {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
}
