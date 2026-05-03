package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/SoldierOfGod1/command-centre/internal/athena"
	"github.com/SoldierOfGod1/command-centre/internal/axiomapi"
	"github.com/SoldierOfGod1/command-centre/internal/chat"
	"github.com/SoldierOfGod1/command-centre/internal/clickup"
	"github.com/SoldierOfGod1/command-centre/internal/config"
	"github.com/SoldierOfGod1/command-centre/internal/customer"
	"github.com/SoldierOfGod1/command-centre/internal/darknoc"
	"github.com/SoldierOfGod1/command-centre/internal/discord"
	"github.com/SoldierOfGod1/command-centre/internal/event"
	"github.com/SoldierOfGod1/command-centre/internal/gaussdb"
	"github.com/SoldierOfGod1/command-centre/internal/networkstate"
	"github.com/SoldierOfGod1/command-centre/internal/logging"
	"github.com/SoldierOfGod1/command-centre/internal/platforms"
	"github.com/SoldierOfGod1/command-centre/internal/runner"
	"github.com/SoldierOfGod1/command-centre/internal/sales"
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

	// Now that the DB handle exists, upgrade the logger to one that
	// mirrors WARN+ records into log_entries. The buffered sink is
	// fire-and-forget, so no meaningful latency is added to the
	// hot path. Existing `log.Info(...)` calls continue to go to
	// stdout via the inner JSON handler.
	if wrapped, sink := logging.NewLoggerWithDB(cfg.Logging.Level, cfg.Logging.ServiceName, db.DB); sink != nil {
		log = wrapped
		defer sink.Close()
	}

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

	// Activity-feed publisher: writes into feed_events and
	// broadcasts on the bus so the Activity Feed tab + Dashboard
	// card populate on real user actions.
	feedPub := event.NewPublisher(db.DB, bus, log)

	// Chat system. Executor records token usage into cost_records on every
	// successful run so the Dashboard KPI tiles have real data to show.
	executor := chat.NewExecutorWithDB(log, bus, db.DB)
	queueMgr := chat.NewQueueManager(executor, db.DB, log)

	// Phase A3 — hybrid agent dispatcher. Wraps the CLI executor
	// with a tool-use loop that runs against /api/v1 endpoints
	// when ANTHROPIC_API_KEY is set. No key => CLI handles
	// everything (existing behaviour). Tool catalogue calls back
	// through the local server, so audit + role gates apply
	// uniformly to both human-driven and agent-driven actions.
	agentBaseURL := strings.TrimSpace(os.Getenv("RAIN_API_BASE_URL"))
	if agentBaseURL == "" {
		agentBaseURL = "http://127.0.0.1:8080/api/v1"
	}
	dispatcher := chat.NewDispatcherFromEnv(log, bus, executor, db.DB, agentBaseURL)

	// Customer 360 — pgx pool manager for Axiom lookups.
	customerMgr := customer.NewManager(db)
	defer customerMgr.Close()

	// Athena CDR usage — optional. Reads ATHENA_* env vars; if
	// ATHENA_OUTPUT is empty the client is nil and the Customer 360
	// usage panel silently skips the CDR source. Default region
	// eu-west-1 matches rain's Athena workgroup. Queries cost money
	// per GB scanned, so UsageService caches 30 min per IMSI set.
	// Athena config: app_settings wins over env. Lets the Settings
	// page overwrite AWS region / S3 output location without a
	// config.toml or shell restart — on next backend restart the
	// values take effect.
	appSettings, _ := db.GetAllSettings()
	if appSettings == nil {
		appSettings = map[string]string{}
	}
	// If the user put AWS creds in app_settings, put them in env
	// so the SDK's default credential chain picks them up without
	// us having to write a custom credential provider.
	if v := appSettings["athena.aws_access_key_id"]; v != "" {
		_ = os.Setenv("AWS_ACCESS_KEY_ID", v)
	}
	if v := appSettings["athena.aws_secret_access_key"]; v != "" {
		_ = os.Setenv("AWS_SECRET_ACCESS_KEY", v)
	}
	// Session token is required when the access key is an ASIA-prefixed
	// temporary credential (SSO / AssumeRole). The AWS SDK's static
	// credential provider picks up all three from env.
	if v := appSettings["athena.aws_session_token"]; v != "" {
		_ = os.Setenv("AWS_SESSION_TOKEN", v)
	}
	athenaCfg := athena.ConfigFromSources(appSettings, os.Getenv)
	if athenaCfg.Enabled() {
		athClient, aerr := athena.New(context.Background(), athenaCfg)
		if aerr != nil {
			log.Warn("athena init failed — CDR usage panel disabled",
				"error", aerr, "region", athenaCfg.Region)
		} else {
			customerMgr.SetAthenaUsage(&athenaUsageAdapter{svc: athena.NewUsageService(athClient)})
			log.Info("athena usage enabled",
				"region", athenaCfg.Region,
				"database", athenaCfg.Database,
				"workgroup", athenaCfg.Workgroup)
		}
	} else {
		log.Info("athena usage disabled — set ATHENA_OUTPUT (+ AWS creds) to enable")
	}

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

	// Platform monitor — rain BSS middleware (Snowflake) + satellite apps +
	// DB health. SQLite-backed history drives 24h/7d/30d uptime rollups;
	// the same SQLAlertSink powers the /service page's alert feed and
	// auto-creates incidents on severity >= Critical.
	//
	// Kill-switch: PLATFORM_MONITOR_ENABLED=false disables every check
	// (HTTP + DB) but leaves the API routes up so the UI still renders
	// the last-known snapshot rather than an error.
	platformMon := platforms.NewMonitor(log, 60*time.Second, nil)
	sqlSink := platforms.NewSQLAlertSink(db.DB, log)
	// Email notifier: sends on every Critical/P1 alert when
	// RAIN_ALERT_SMTP_* env vars are set. Falls back to nil (no
	// email) when env is missing — safe for dev. Closes the
	// 2026-04-24 gap where Axiom went down and only the dashboard
	// knew. See backend/internal/platforms/email_sink.go.
	var emailSink platforms.AlertSink
	if e, err := platforms.NewEmailSinkFromEnv(log); err != nil {
		log.Info("email alerts disabled", "reason", err)
	} else {
		emailSink = e
		log.Info("email alerts ENABLED for Critical/P1 — recipients configured via RAIN_ALERT_TO")
	}
	alertSink := platforms.AlertSink(platforms.NewMultiSink(sqlSink, emailSink))
	platformMon.SetHistory(platforms.NewSQLHistory(db.DB))
	platformMon.SetAlertSink(alertSink)
	dbMon := platforms.NewDBMonitor(log, customerMgr, db)
	dbMon.SetAlertSink(alertSink)
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("PLATFORM_MONITOR_ENABLED"))); v == "false" || v == "0" || v == "no" {
		log.Warn("platform monitor DISABLED via PLATFORM_MONITOR_ENABLED — snapshots frozen at last-known")
	} else {
		go platformMon.Run(ctx)
		go dbMon.Run(ctx)
	}

	// Projects Runner — launches local dev servers (frontend + backend)
	// from the Projects page. StopAll on shutdown so Command Centre never
	// leaves an orphan npm/go process holding a port.
	runMgr := runner.NewManager(log, bus)
	defer runMgr.StopAll()

	// rain Sales — background poller serves the dashboard snapshot.
	// Runs on ctx so it exits cleanly on shutdown. Kill-switch:
	// set SALES_POLL_ENABLED=false to skip the auto-poll entirely;
	// the /api/v1/sales/refresh endpoint (+ UI button) still works
	// so an operator can fetch on demand during an Axiom incident.
	salesPoller := sales.NewPoller(customerMgr, log)
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("SALES_POLL_ENABLED"))); v == "false" || v == "0" || v == "no" {
		log.Warn("sales poller auto-start DISABLED via SALES_POLL_ENABLED — dashboard will only update when refreshed manually")
	} else {
		go salesPoller.Start(ctx)
		defer salesPoller.Stop()
	}

	// Phase D1 follow-up — auto-summariser fired from the
	// archive route. Wired with the dispatcher's agent client
	// when available so it uses the API path; otherwise it
	// falls back to a deterministic last-message stitcher.
	autoSummary := chat.NewAutoSummariser(db.DB, log, dispatcher.AgentClient())

	// Phase C1 follow-up — stream replay buffer. Subscribes to
	// chat.stream / chat.complete / chat.error topics and rings
	// the last 100 chunks per conversation so reload / reconnect
	// can replay missed chunks via /conversations/{id}/replay.
	streamBuf := chat.NewStreamBuffer()
	streamBuf.AttachToBus(bus)

	// Dark NOC — read-only ClickHouse fault telemetry + 41-agent
	// reference registry. The registry is loaded once at startup
	// from the operator's local DarkNoc.md (default
	// ~/Downloads/DarkNoc.md). Routes are env-gated by
	// DARK_NOC_ENABLED so a SIT install boots cleanly without one.
	registry := darknoc.LoadRegistry(darknoc.DefaultRegistryPath())
	if len(registry) > 0 {
		log.Info("dark noc registry loaded", "count", len(registry))
	}
	darkNocAdapter := darknoc.NewClickHouseAdapter(db, log, registry)
	// Optional rate-limit override via env (defaults to 10 rps / 20 burst).
	if rps := envFloat("CLICKHOUSE_RATE_PER_SEC", 0); rps > 0 {
		burst := envInt("CLICKHOUSE_RATE_BURST", int(rps*2))
		darkNocAdapter.SetRateLimit(burst, rps)
		log.Info("clickhouse rate limit", "rps", rps, "burst", burst)
	}
	darkNocGrafana := darknoc.NewGrafanaProxy(db, log)

	// Axiom HTTP API client (rate-limited). AXIOM_API_BASE_URL is the
	// only required env var — defaults to the SIT host so a stock
	// install just works on the rain VPN. Token is optional; some
	// endpoints are open from the VPN.
	axiomAPIBase := os.Getenv("AXIOM_API_BASE_URL")
	if axiomAPIBase == "" {
		axiomAPIBase = "https://api.sit.rain.co.za"
	}
	axiomClient := axiomapi.NewClient(axiomAPIBase, log)
	if tok := strings.TrimSpace(os.Getenv("AXIOM_API_TOKEN")); tok != "" {
		axiomClient.SetToken(tok)
	}
	if rps := envFloat("AXIOM_API_RATE_PER_SEC", 0); rps > 0 {
		burst := envInt("AXIOM_API_RATE_BURST", int(rps*2))
		axiomClient.SetRateLimit(burst, rps)
		log.Info("axiom-api rate limit", "rps", rps, "burst", burst)
	}
	log.Info("axiom-api client wired", "base", axiomAPIBase, "token_set", os.Getenv("AXIOM_API_TOKEN") != "")

	// GaussDB DWS · PROD — alternate source for daily-CDR usage. The
	// pgxpool is lazy (opens on first call), and the route handlers
	// gate themselves on RAIN_SUPPORT_L2 + GAUSSDB_USAGE_ENABLED, so
	// always wiring the client costs nothing at idle. PlaceholderSQL
	// constant in internal/gaussdb/queries.go keeps the routes 503'd
	// until the operator pastes the real SQL.
	gaussClient := gaussdb.NewClient(db, log)
	if id := strings.TrimSpace(os.Getenv("GAUSSDB_CONNECTION_ID")); id != "" {
		gaussClient.SetConnection(id)
	}
	log.Info("gaussdb client wired",
		"placeholder_sql", gaussdb.PlaceholderSQL,
		"usage_source_default", strings.TrimSpace(os.Getenv("USAGE_SOURCE")))

	// State of the Network poller — refreshes its in-memory snapshot
	// every 30s by default. The frontend reads from the snapshot so
	// page loads never block on ClickHouse. Override the cadence with
	// NETWORKSTATE_INTERVAL (e.g. "10s" for tight refresh, "60s" to
	// reduce ClickHouse load). Floor of 5s enforced internally.
	networkPoller := networkstate.NewPoller(darkNocAdapter, platformMon, dbMon, db.DB, log)
	if v := strings.TrimSpace(os.Getenv("NETWORKSTATE_INTERVAL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			networkPoller.SetInterval(d)
			log.Info("networkstate interval override", "interval", d.String())
		} else {
			log.Warn("NETWORKSTATE_INTERVAL parse failed; using default", "raw", v, "error", err)
		}
	}
	go networkPoller.Start(ctx)
	defer networkPoller.Stop()

	api := &server.API{
		DB:             db.DB,
		Store:          db,
		Log:            log,
		Bus:            bus,
		Hub:            hub,
		QueueMgr:       queueMgr,
		Dispatcher:     dispatcher,
		ActiveConvs:    chat.NewActiveConversations(),
		AutoSummary:    autoSummary,
		StreamBuf:      streamBuf,
		ClickUp:        cfg.ClickUp,
		SyncEngine:     syncEngine,
		CustomerMgr:    customerMgr,
		MCPHealth:      mcpHealth,
		PlatformMon:    platformMon,
		DBHealth:       dbMon,
		AlertSink:      alertSink,
		Runner:         runMgr,
		SalesPoller:    salesPoller,
		Feed:           feedPub,
		DarkNoc:        darkNocAdapter,
		DarkNocGrafana: darkNocGrafana,
		AxiomAPI:       axiomClient,
		Gaussdb:        gaussClient,
		NetworkState:   networkPoller,
		StartTime:      time.Now(),
	}

	// Liveness heartbeat — the /health/live handler returns 503 if
	// this goroutine stops flipping the timestamp (a proxy for a
	// deadlocked main loop). Previously the handler was an
	// unconditional 200, useless for detecting deadlocks.
	server.Heartbeat(ctx)

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

// athenaUsageAdapter bridges athena.UsageService → the
// customer.UsageLookerUpper interface. Keeps the customer package
// free of an Athena import, breaking the cycle.
type athenaUsageAdapter struct {
	svc *athena.UsageService
}

func (a *athenaUsageAdapter) Available() bool { return a != nil && a.svc != nil && a.svc.Available() }

func (a *athenaUsageAdapter) UsageSince(ctx context.Context, imsis []int64) ([]customer.CDRUsage, error) {
	if !a.Available() {
		return nil, nil
	}
	rows, err := a.svc.UsageSince(ctx, imsis)
	if err != nil {
		return nil, err
	}
	out := make([]customer.CDRUsage, 0, len(rows))
	for _, r := range rows {
		out = append(out, customer.CDRUsage{
			Date:           r.Date,
			AccountCode:    r.AccountCode,
			BillingAccount: r.BillingAccount,
			IMEI:           r.IMEI,
			IMSI:           r.IMSI,
			MSISDN:         r.MSISDN,
			UsageGB:        r.UsageGB,
		})
	}
	return out, nil
}

// envFloat reads a positive float from the named env var. Returns
// the supplied default when the var is empty or unparseable.
func envFloat(name string, fallback float64) float64 {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

// envInt reads a positive int from the named env var. Returns the
// supplied default on empty / unparseable / non-positive.
func envInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}
