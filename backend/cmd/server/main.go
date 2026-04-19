package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/SoldierOfGod1/command-centre/internal/chat"
	"github.com/SoldierOfGod1/command-centre/internal/clickup"
	"github.com/SoldierOfGod1/command-centre/internal/config"
	"github.com/SoldierOfGod1/command-centre/internal/customer"
	"github.com/SoldierOfGod1/command-centre/internal/discord"
	"github.com/SoldierOfGod1/command-centre/internal/event"
	"github.com/SoldierOfGod1/command-centre/internal/logging"
	"github.com/SoldierOfGod1/command-centre/internal/server"
	"github.com/SoldierOfGod1/command-centre/internal/skills"
	"github.com/SoldierOfGod1/command-centre/internal/store"
	"github.com/SoldierOfGod1/command-centre/internal/sync"
	"github.com/SoldierOfGod1/command-centre/internal/ws"
)

func main() {
	cfg, err := config.Load("config.toml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	log := logging.NewLogger(cfg.Logging.Level, cfg.Logging.ServiceName)
	log.Info("SOLDIER OF GOD — Command Centre starting",
		"version", "1.1.0",
		"port", cfg.Server.Port,
	)

	db, err := store.New(cfg.Database.Path, log)
	if err != nil {
		log.Error("database init failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.SeedIfEmpty(); err != nil {
		log.Error("seed failed", "error", err)
		os.Exit(1)
	}
	if err := db.SeedProjectsIfEmpty(); err != nil {
		log.Error("project seed failed", "error", err)
		os.Exit(1)
	}

	// Mirror TOML-configured ClickUp settings into app_settings on first run
	// so the Settings page can read/update them after that. TOML values act
	// as defaults only; once the user saves via the UI, the DB copy wins.
	seedClickUpSettings(db, cfg.ClickUp, log)

	// Seed Axiom defaults so the legacy single-connection form still works
	// for anyone who hasn't moved to the new multi-connection UI yet.
	// The multi-connection seed (below) is the authoritative source.
	seedAxiomDefaults(db, log)

	// Seed the multi-connection registry: the 4 Postgres clusters + the
	// ClickHouse entry. Idempotent — skips any id that's already present,
	// and migrates legacy axiom.* settings into a connection row so a
	// previously-saved password isn't lost.
	if err := db.SeedDefaultConnections(); err != nil {
		log.Warn("seed db connections", "error", err)
	}

	bus := event.NewBus()
	hub := ws.NewHub(log, bus)
	go hub.Run()

	// Chat system. Executor records token usage into cost_records on every
	// successful run so the Dashboard KPI tiles have real data to show.
	executor := chat.NewExecutorWithDB(log, bus, db.DB)
	queueMgr := chat.NewQueueManager(executor, db.DB, log)

	// Customer 360 — pgx pool manager for Axiom lookups.
	customerMgr := customer.NewManager(db)
	defer customerMgr.Close()

	// ClickUp sync engine — owns the 60s poller goroutine + inline push path.
	syncEngine := sync.New(db, log, bus)
	ctx, cancelSync := context.WithCancel(context.Background())
	defer cancelSync()
	go syncEngine.Run(ctx)

	// One-shot: make sure the configured list has our 10 project statuses.
	go ensureClickUpStatuses(db, log)

	// #3 — MCP health monitor. Ticks every 60s, probes every enabled
	// server in parallel, keeps latest status in memory for /api/v1/mcp/health.
	mcpHealth := skills.NewHealthMonitor(log, 60*time.Second)
	projectDir, _ := os.Getwd()
	go mcpHealth.Run(ctx, projectDir)

	api := &server.API{
		DB:          db.DB,
		Store:       db,
		Log:         log,
		Bus:         bus,
		Hub:         hub,
		QueueMgr:    queueMgr,
		ClickUp:     cfg.ClickUp,
		SyncEngine:  syncEngine,
		CustomerMgr: customerMgr,
		MCPHealth:   mcpHealth,
		StartTime:   time.Now(),
	}

	// Start Discord bot if configured
	go startDiscordBot(db.DB, queueMgr, log, bus)

	router := server.NewRouter(api, hub, cfg.Frontend.StaticDir)

	if err := server.Run(
		router,
		cfg.Server.Host,
		cfg.Server.Port,
		cfg.Server.ReadTimeoutDuration(),
		cfg.Server.WriteTimeoutDuration(),
		log,
	); err != nil {
		log.Error("server error", "error", err)
		os.Exit(1)
	}
}

func startDiscordBot(db *sql.DB, qm *chat.QueueManager, log *slog.Logger, bus *event.Bus) {
	// Read Discord config from database
	var token, userID string
	var pinTimeout int
	err := db.QueryRow("SELECT discord_token, discord_user_id, pin_timeout_minutes FROM chat_config WHERE id=1").
		Scan(&token, &userID, &pinTimeout)
	if err != nil || token == "" {
		log.Info("Discord bot not configured — skipping")
		return
	}

	timeout := time.Duration(pinTimeout) * time.Minute
	bot, err := discord.NewBot(token, userID, qm, db, log, bus, timeout)
	if err != nil {
		log.Error("Discord bot failed to start", "error", err)
		return
	}

	log.Info("Discord bot started", "user_id", userID)
	if err := bot.Start(); err != nil {
		log.Error("Discord bot error", "error", err)
	}
}

// seedClickUpSettings copies TOML defaults into the DB-backed settings store
// the first time through. Once the user saves via the Settings page, those
// DB values are authoritative.
func seedClickUpSettings(s *store.Store, cfg config.ClickUpConfig, log *slog.Logger) {
	defaults := map[string]string{
		store.SettingClickUpToken:       cfg.APIToken,
		store.SettingClickUpWorkspaceID: cfg.WorkspaceID,
		store.SettingClickUpListID:      cfg.ListID,
	}
	for key, val := range defaults {
		if val == "" {
			continue
		}
		existing, _ := s.GetSetting(key)
		if existing != "" {
			continue
		}
		if err := s.SetSetting(key, val); err != nil {
			log.Warn("seed clickup setting", "key", key, "error", err)
		}
	}
}

// seedAxiomDefaults writes the confirmed-by-user default Axiom connection
// bits on first boot so the Settings form renders with host/port/user/db
// already populated. Password stays blank — that's the user's job. Any
// existing value is left untouched.
func seedAxiomDefaults(s *store.Store, log *slog.Logger) {
	defaults := map[string]string{
		store.SettingAxiomHost:     "bss-psql-sit-01.rain.network",
		store.SettingAxiomPort:     "5432",
		store.SettingAxiomDatabase: "postgresdb",
		store.SettingAxiomUser:     "baptista",
		store.SettingAxiomSSLMode:  "disable",
	}
	for key, val := range defaults {
		existing, _ := s.GetSetting(key)
		if existing != "" {
			continue
		}
		if err := s.SetSetting(key, val); err != nil {
			log.Warn("seed axiom default", "key", key, "error", err)
		}
	}
}

// ensureClickUpStatuses reads the current token + list id out of settings
// and pushes the canonical 10-status pipeline into the ClickUp list so that
// subsequent pushes don't get rejected with "status not found".
// Runs off the hot path — failures are logged, not fatal.
func ensureClickUpStatuses(s *store.Store, log *slog.Logger) {
	all, err := s.GetAllSettings()
	if err != nil {
		log.Warn("ensure statuses: read settings", "error", err)
		return
	}
	token := all[store.SettingClickUpToken]
	listID := all[store.SettingClickUpListID]
	if token == "" || listID == "" {
		log.Info("clickup not configured — skipping status bootstrap")
		return
	}
	client := clickup.New(token)
	if err := client.EnsureListStatuses(listID, clickup.ProjectStatuses); err != nil {
		log.Warn("ensure clickup statuses", "error", err)
		return
	}
	log.Info("clickup statuses ensured", "list_id", listID, "count", len(clickup.ProjectStatuses))
}
