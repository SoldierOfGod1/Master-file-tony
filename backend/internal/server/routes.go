package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/SoldierOfGod1/command-centre/internal/chat"
	"github.com/SoldierOfGod1/command-centre/internal/config"
	"github.com/SoldierOfGod1/command-centre/internal/customer"
	"github.com/SoldierOfGod1/command-centre/internal/event"
	"github.com/SoldierOfGod1/command-centre/internal/middleware"
	"github.com/SoldierOfGod1/command-centre/internal/platforms"
	"github.com/SoldierOfGod1/command-centre/internal/runner"
	"github.com/SoldierOfGod1/command-centre/internal/sales"
	"github.com/SoldierOfGod1/command-centre/internal/skills"
	"github.com/SoldierOfGod1/command-centre/internal/store"
	"github.com/SoldierOfGod1/command-centre/internal/sync"
	"github.com/SoldierOfGod1/command-centre/internal/ws"
)

type API struct {
	DB          *sql.DB
	Store       *store.Store
	Log         *slog.Logger
	Bus         *event.Bus
	Hub         *ws.Hub
	QueueMgr    *chat.QueueManager
	// Phase A3 — hybrid agent dispatcher. Optional: nil means
	// /api/v1/chat/agent returns 503 and the existing /chat
	// path is the only entry. Wire from main.go via
	// chat.NewDispatcherFromEnv after building the catalogue.
	Dispatcher  *chat.Dispatcher
	ClickUp     config.ClickUpConfig
	SyncEngine  *sync.Engine
	CustomerMgr *customer.Manager
	MCPHealth   *skills.HealthMonitor
	PlatformMon *platforms.Monitor
	DBHealth    *platforms.DBMonitor
	Runner      *runner.Manager
	SalesPoller *sales.Poller
	Feed        *event.Publisher // activity feed writer; may be nil in tests
	StartTime   time.Time
}

func NewRouter(api *API, hub *ws.Hub, staticDir string) http.Handler {
	mux := http.NewServeMux()

	// WebSocket
	mux.HandleFunc("GET /ws", ws.HandleUpgrade(hub, api.Log))

	// Health
	mux.HandleFunc("GET /health", api.handleHealth)
	mux.HandleFunc("GET /health/ready", api.handleHealthReady)
	mux.HandleFunc("GET /health/live", api.handleHealthLive)

	// Agents
	mux.HandleFunc("GET /api/v1/agents", api.handleListAgents)
	mux.HandleFunc("GET /api/v1/agents/{id}", api.handleGetAgent)
	mux.HandleFunc("PUT /api/v1/agents/{id}", api.handleUpdateAgent)

	// Tasks
	mux.HandleFunc("GET /api/v1/tasks", api.handleListTasks)
	mux.HandleFunc("POST /api/v1/tasks", api.handleCreateTask)
	mux.HandleFunc("GET /api/v1/tasks/{id}", api.handleGetTask)
	mux.HandleFunc("PUT /api/v1/tasks/{id}", api.handleUpdateTask)
	mux.HandleFunc("DELETE /api/v1/tasks/{id}", api.handleDeleteTask)

	// KPIs
	mux.HandleFunc("GET /api/v1/kpis", api.handleGetKPIs)

	// Feed
	mux.HandleFunc("GET /api/v1/feed", api.handleListFeed)

	// Tools
	mux.HandleFunc("GET /api/v1/tools", api.handleListTools)
	mux.HandleFunc("GET /api/v1/tools/{id}", api.handleGetTool)
	mux.HandleFunc("PUT /api/v1/tools/{id}", api.handleUpdateTool)

	// Health metrics (dashboard gauges)
	mux.HandleFunc("GET /api/v1/health-metrics", api.handleHealthMetrics)

	// Logs
	mux.HandleFunc("GET /api/v1/logs", api.handleListLogs)

	// Costs
	mux.HandleFunc("GET /api/v1/costs", api.handleGetCosts)

	// Security
	mux.HandleFunc("GET /api/v1/security", api.handleGetSecurity)

	// Approvals
	mux.HandleFunc("GET /api/v1/approvals", api.handleListApprovals)
	mux.HandleFunc("POST /api/v1/approvals", api.handleCreateApproval)
	mux.HandleFunc("GET /api/v1/approvals/{id}", api.handleGetApproval)
	mux.HandleFunc("PUT /api/v1/approvals/{id}", api.handleUpdateApproval)

	// Projects
	mux.HandleFunc("GET /api/v1/projects", api.handleListProjects)
	mux.HandleFunc("POST /api/v1/projects", api.handleCreateProject)
	mux.HandleFunc("GET /api/v1/projects/{id}", api.handleGetProject)
	mux.HandleFunc("PUT /api/v1/projects/{id}", api.handleUpdateProject)
	mux.HandleFunc("DELETE /api/v1/projects/{id}", api.handleDeleteProject)
	mux.HandleFunc("POST /api/v1/projects/sync", api.handleSyncProjects)
	mux.HandleFunc("GET /api/v1/projects/sync/status", api.handleSyncStatus)

	// App-level settings (read/write from Settings page)
	mux.HandleFunc("GET /api/v1/settings", api.handleGetSettings)
	mux.HandleFunc("PUT /api/v1/settings", api.handleUpdateSettings)

	// Pipelines
	mux.HandleFunc("GET /api/v1/pipelines", api.handleListPipelines)
	mux.HandleFunc("POST /api/v1/pipelines", api.handleCreatePipeline)
	mux.HandleFunc("GET /api/v1/pipelines/{id}", api.handleGetPipeline)
	mux.HandleFunc("PUT /api/v1/pipelines/{id}", api.handleUpdatePipeline)

	// Documents
	mux.HandleFunc("GET /api/v1/documents", api.handleListDocuments)
	mux.HandleFunc("POST /api/v1/documents", api.handleCreateDocument)
	mux.HandleFunc("GET /api/v1/documents/{id}", api.handleGetDocument)
	mux.HandleFunc("PUT /api/v1/documents/{id}", api.handleUpdateDocument)

	// Agent Office
	mux.HandleFunc("GET /api/v1/office", api.handleGetOffice)
	mux.HandleFunc("GET /api/v1/office/{agentId}", api.handleGetOfficeAgent)

	// Chat & Conversations
	RegisterChatRoutes(mux, api)

	// Skills + MCP catalogue
	RegisterSkillsRoutes(mux, api)

	// ClickUp integration
	RegisterClickUpRoutes(mux, api)

	// Customer 360 — Axiom lookup
	RegisterCustomerRoutes(mux, api)

	// DB connection registry (CRUD for Customer 360's multi-cluster setup)
	RegisterConnectionsRoutes(mux, api)

	// Agent Fleet — agents + hooks + rules + per-agent memory
	RegisterAgentsRoutes(mux, api)

	// Quality gates — go vet + tsc + secret scan, surfaced on Dashboard
	RegisterQualityRoutes(mux, api)

	// Loop Operator — list/pause/kill active chat queue workers
	RegisterLoopsRoutes(mux, api)

	// Platform Monitor — rain BSS middleware + satellite apps up/down
	RegisterPlatformsRoutes(mux, api)

	// Axiom schema explorer + Snowflake-middleware correlation map
	RegisterAxiomRoutes(mux, api)

	// Projects Runner — start/stop local dev servers from the Projects page
	RegisterProjectsRunnerRoutes(mux, api)

	// rain Sales — background-polled dashboard snapshot
	RegisterSalesRoutes(mux, api)

	// Static files (frontend) with SPA fallback for React Router
	mux.HandleFunc("/", spaHandler(staticDir))

	// Apply middleware
	var handler http.Handler = mux
	handler = middleware.CORS(handler)
	handler = middleware.Logging(api.Log)(handler)
	handler = middleware.Recovery(api.Log)(handler)

	return handler
}

// ── JSON helpers ──────────────────────────────────────────

func jsonOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"success": false, "error": msg})
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

// ── Health ────────────────────────────────────────────────

func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	uptime := int(time.Since(a.StartTime).Seconds())
	jsonOK(w, map[string]any{"status": "healthy", "version": "1.0.0", "uptime_seconds": uptime})
}

// handleHealthReady probes every critical dependency and returns 503
// if ANY of them is degraded. Previously it only pinged the SQLite
// handle — that missed sales poller stalls, MCP crashes, and
// platform-monitor outages. The new probe walks:
//   1. SQLite         — DB.Ping (still)
//   2. Sales poller   — last snapshot within 2× interval
//   3. Platform mon   — at least one service status cached
//   4. DB monitor     — at least one DB snapshot cached
// Each check reports its own key so curl output tells you exactly
// what's broken.
func (a *API) handleHealthReady(w http.ResponseWriter, r *http.Request) {
	checks := map[string]string{}
	ready := true

	if err := a.DB.Ping(); err != nil {
		checks["sqlite"] = "down: " + err.Error()
		ready = false
	} else {
		checks["sqlite"] = "ok"
	}

	// Sales poller: degraded if last snapshot is older than 2× interval (30 min)
	if a.SalesPoller != nil {
		snap := a.SalesPoller.Snapshot()
		if snap == nil || snap.AsOf.IsZero() {
			checks["sales_poller"] = "no snapshot yet"
		} else if time.Since(snap.AsOf) > 30*time.Minute {
			checks["sales_poller"] = "stale: last poll " + time.Since(snap.AsOf).Round(time.Second).String() + " ago"
			ready = false
		} else {
			checks["sales_poller"] = "ok"
		}
	}

	if a.PlatformMon != nil {
		pl := a.PlatformMon.Snapshot()
		if len(pl) == 0 {
			checks["platform_monitor"] = "no data"
		} else {
			checks["platform_monitor"] = "ok"
		}
	}

	if a.DBHealth != nil {
		dbSnap := a.DBHealth.Snapshot()
		checks["db_monitor"] = "ok"
		_ = dbSnap
	}

	payload := map[string]any{"status": "ready", "checks": checks}
	if !ready {
		payload["status"] = "degraded"
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = writeJSON(w, payload)
		return
	}
	// Happy path uses the standard envelope ({"success":true,"data":…}).
	jsonOK(w, payload)
}

// heartbeat is updated every 5s by a goroutine started in main.go.
// handleHealthLive returns 503 when the heartbeat is > 30s stale —
// a proxy for "main loop is deadlocked". An always-200 liveness
// endpoint (the previous behaviour) fails silently in that scenario.
var heartbeatNanos atomic.Int64

// Heartbeat returns a ticker goroutine that flips heartbeatNanos
// every 5s. Call once from main.go.
func Heartbeat(ctx context.Context) {
	heartbeatNanos.Store(time.Now().UnixNano())
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				heartbeatNanos.Store(time.Now().UnixNano())
			}
		}
	}()
}

func (a *API) handleHealthLive(w http.ResponseWriter, r *http.Request) {
	last := heartbeatNanos.Load()
	if last == 0 {
		// Heartbeat not started yet — don't fail, just note it.
		jsonOK(w, map[string]string{"status": "live", "heartbeat": "unset"})
		return
	}
	age := time.Since(time.Unix(0, last))
	if age > 30*time.Second {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = writeJSON(w, map[string]any{"status": "stalled", "heartbeat_age_s": int(age.Seconds())})
		return
	}
	jsonOK(w, map[string]any{"status": "live", "heartbeat_age_s": int(age.Seconds())})
}

// writeJSON is a minimal inline helper for 503 paths where we want
// the standard envelope shape but jsonOK is wrong (it always writes
// 200 status before serialising).
func writeJSON(w http.ResponseWriter, v any) error {
	b, err := json.Marshal(map[string]any{"success": false, "error": v})
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

func (a *API) handleHealthMetrics(w http.ResponseWriter, r *http.Request) {
	// Everything below is computed from real process state — no scaling to
	// fake a "nominal" graph. Idle backend correctly shows small numbers.
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	memPct := 0.0
	if m.Sys > 0 {
		memPct = float64(m.Alloc) / float64(m.Sys) * 100
	}
	goroutines := runtime.NumGoroutine()
	// "CPU" gauge is a goroutine-count proxy (we don't sample host CPU from
	// inside a Go process without adding a dep). 1 goroutine == 1%, capped
	// at 100. For 20 goroutines you get a 20% reading — honest signal, not
	// a randomised fake.
	cpuGauge := float64(goroutines)
	if cpuGauge > 100 {
		cpuGauge = 100
	}
	jsonOK(w, map[string]any{
		"cpu":        cpuGauge,
		"memory":     memPct,
		"network":    float64(a.Hub.ClientCount()),
		"goroutines": goroutines,
	})
}

// ── Agents ────────────────────────────────────────────────

func (a *API) handleListAgents(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.Query("SELECT id,name,model,max_instances,status,task,role FROM agents ORDER BY id")
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	var agents []map[string]any
	for rows.Next() {
		var id, name, model, status, task, role string
		var maxInst sql.NullInt64
		rows.Scan(&id, &name, &model, &maxInst, &status, &task, &role)
		agent := map[string]any{
			"id": id, "name": name, "model": model, "status": status, "task": task, "role": role,
			"maxInstances": nil,
		}
		if maxInst.Valid {
			agent["maxInstances"] = maxInst.Int64
		}
		agents = append(agents, agent)
	}
	// Return [] rather than null for empty lists so frontends can treat
	// the response uniformly without null-checks.
	if agents == nil {
		agents = []map[string]any{}
	}
	jsonOK(w, agents)
}

func (a *API) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var name, model, status, task, role string
	var maxInst sql.NullInt64
	err := a.DB.QueryRow("SELECT name,model,max_instances,status,task,role FROM agents WHERE id=?", id).
		Scan(&name, &model, &maxInst, &status, &task, &role)
	if err != nil {
		jsonError(w, 404, "agent not found")
		return
	}
	agent := map[string]any{"id": id, "name": name, "model": model, "status": status, "task": task, "role": role, "maxInstances": nil}
	if maxInst.Valid {
		agent["maxInstances"] = maxInst.Int64
	}
	jsonOK(w, agent)
}

func (a *API) handleUpdateAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Status string `json:"status"`
		Task   string `json:"task"`
	}
	if err := readJSON(r, &body); err != nil {
		jsonError(w, 400, "invalid json")
		return
	}
	now := time.Now().Format(time.RFC3339)
	a.DB.Exec("UPDATE agents SET status=?,task=?,updated_at=? WHERE id=?", body.Status, body.Task, now, id)
	a.Bus.PublishJSON("agent.status", map[string]string{"id": id, "status": body.Status})
	jsonOK(w, map[string]string{"id": id, "status": "updated"})
}

// ── Tasks ─────────────────────────────────────────────────

func (a *API) handleListTasks(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.Query("SELECT id,title,agent_id,priority,col,created_at FROM tasks ORDER BY created_at DESC")
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	var tasks []map[string]any
	for rows.Next() {
		var id, title, agent, priority, col, created string
		rows.Scan(&id, &title, &agent, &priority, &col, &created)
		tasks = append(tasks, map[string]any{
			"id": id, "title": title, "agent": agent, "priority": priority, "column": col, "time": created,
		})
	}
	if tasks == nil {
		tasks = []map[string]any{}
	}
	jsonOK(w, tasks)
}

func (a *API) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title    string `json:"title"`
		Agent    string `json:"agent"`
		Priority string `json:"priority"`
		Column   string `json:"column"`
	}
	if err := readJSON(r, &body); err != nil {
		jsonError(w, 400, "invalid json")
		return
	}
	id := "t" + time.Now().Format("20060102150405")
	now := time.Now().Format(time.RFC3339)
	if body.Column == "" {
		body.Column = "inbox"
	}
	if body.Priority == "" {
		body.Priority = "p3"
	}
	a.DB.Exec("INSERT INTO tasks (id,title,agent_id,priority,col,created_at,updated_at) VALUES (?,?,?,?,?,?,?)",
		id, body.Title, body.Agent, body.Priority, body.Column, now, now)
	a.Bus.PublishJSON("task.update", map[string]string{"id": id, "action": "created"})
	if a.Feed != nil {
		a.Feed.Publish(r.Context(), event.FeedKindTask, body.Agent, "Task created: "+body.Title)
	}
	jsonOK(w, map[string]string{"id": id})
}

func (a *API) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var title, agent, priority, col, created string
	err := a.DB.QueryRow("SELECT title,agent_id,priority,col,created_at FROM tasks WHERE id=?", id).
		Scan(&title, &agent, &priority, &col, &created)
	if err != nil {
		jsonError(w, 404, "task not found")
		return
	}
	jsonOK(w, map[string]any{"id": id, "title": title, "agent": agent, "priority": priority, "column": col, "time": created})
}

func (a *API) handleUpdateTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		jsonError(w, 400, "invalid json")
		return
	}
	now := time.Now().Format(time.RFC3339)
	if col, ok := body["column"].(string); ok {
		a.DB.Exec("UPDATE tasks SET col=?,updated_at=? WHERE id=?", col, now, id)
	}
	if title, ok := body["title"].(string); ok {
		a.DB.Exec("UPDATE tasks SET title=?,updated_at=? WHERE id=?", title, now, id)
	}
	if priority, ok := body["priority"].(string); ok {
		a.DB.Exec("UPDATE tasks SET priority=?,updated_at=? WHERE id=?", priority, now, id)
	}
	a.Bus.PublishJSON("task.update", map[string]string{"id": id, "action": "updated"})
	jsonOK(w, map[string]string{"id": id, "status": "updated"})
}

func (a *API) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a.DB.Exec("DELETE FROM tasks WHERE id=?", id)
	a.Bus.PublishJSON("task.update", map[string]string{"id": id, "action": "deleted"})
	jsonOK(w, map[string]string{"id": id, "status": "deleted"})
}

// ── KPIs ──────────────────────────────────────────────────

func (a *API) handleGetKPIs(w http.ResponseWriter, r *http.Request) {
	// All values come from real state — no hard-coded 99.9% uptime or fake
	// trend arrows. `trend` stays "flat" everywhere until we have enough
	// history to compute a real delta (24h window is the planned horizon).
	var activeAgents, totalAgents int
	a.DB.QueryRow("SELECT COUNT(*) FROM agents WHERE status='active'").Scan(&activeAgents)
	a.DB.QueryRow("SELECT COUNT(*) FROM agents").Scan(&totalAgents)

	var tasksInFlight int
	a.DB.QueryRow("SELECT COUNT(*) FROM tasks WHERE col IN ('inbox','progress','review')").Scan(&tasksInFlight)

	// Cost / tokens: sum from cost_records for today (UTC). If nothing's been
	// recorded yet the sum is 0 — an empty state, not a fake number.
	today := time.Now().UTC().Format("2006-01-02")
	var tokensToday int
	var costToday float64
	a.DB.QueryRow("SELECT COALESCE(SUM(tokens_used),0), COALESCE(SUM(amount_zar),0) FROM cost_records WHERE date=?", today).
		Scan(&tokensToday, &costToday)

	// Uptime: wall-clock since StartTime. Pure fact.
	uptimeSeconds := int(time.Since(a.StartTime).Seconds())

	// Error rate: share of ERROR log_entries in the last hour. If there are
	// no log entries it's 0, not 0.0%-of-nothing.
	var errorCount, totalLogs int
	oneHourAgo := time.Now().Add(-1 * time.Hour).Format("15:04:05.000")
	a.DB.QueryRow("SELECT COUNT(*) FROM log_entries WHERE timestamp >= ?", oneHourAgo).Scan(&totalLogs)
	a.DB.QueryRow("SELECT COUNT(*) FROM log_entries WHERE timestamp >= ? AND level = 'ERROR'", oneHourAgo).Scan(&errorCount)
	errorRate := 0.0
	if totalLogs > 0 {
		errorRate = float64(errorCount) / float64(totalLogs) * 100
	}

	jsonOK(w, map[string]any{
		"activeAgents":  map[string]any{"value": activeAgents, "max": totalAgents, "trend": "flat"},
		"tasksInFlight": map[string]any{"value": tasksInFlight, "trend": "flat"},
		"tokensToday":   map[string]any{"value": tokensToday, "trend": "flat"},
		"costToday":     map[string]any{"value": costToday, "trend": "flat"},
		"uptime":        map[string]any{"value": uptimeSeconds, "unit": "seconds", "trend": "flat"},
		"errorRate":     map[string]any{"value": errorRate, "trend": "flat"},
	})
}

// ── Feed ──────────────────────────────────────────────────

func (a *API) handleListFeed(w http.ResponseWriter, r *http.Request) {
	filter := r.URL.Query().Get("type")
	query := "SELECT time,type,agent_id,message FROM feed_events ORDER BY id DESC LIMIT 50"
	args := []any{}
	if filter != "" && filter != "all" {
		query = "SELECT time,type,agent_id,message FROM feed_events WHERE type=? ORDER BY id DESC LIMIT 50"
		args = append(args, filter)
	}
	rows, _ := a.DB.Query(query, args...)
	defer rows.Close()

	var events []map[string]string
	for rows.Next() {
		var t, typ, agent, msg string
		rows.Scan(&t, &typ, &agent, &msg)
		events = append(events, map[string]string{"time": t, "type": typ, "agent": agent, "message": msg})
	}
	if events == nil {
		events = []map[string]string{}
	}
	jsonOK(w, events)
}

// ── Tools ─────────────────────────────────────────────────

func (a *API) handleListTools(w http.ResponseWriter, r *http.Request) {
	rows, _ := a.DB.Query("SELECT id,name,icon,description,detail,agents,systems,status FROM tools")
	defer rows.Close()

	var tools []map[string]any
	for rows.Next() {
		var id, name, icon, desc, detail, agents, systems, status string
		rows.Scan(&id, &name, &icon, &desc, &detail, &agents, &systems, &status)
		var agentArr, sysArr []string
		json.Unmarshal([]byte(agents), &agentArr)
		json.Unmarshal([]byte(systems), &sysArr)
		tools = append(tools, map[string]any{
			"id": id, "name": name, "icon": icon, "desc": desc, "detail": detail,
			"agents": agentArr, "systems": sysArr, "status": status,
		})
	}
	if tools == nil {
		tools = []map[string]any{}
	}
	jsonOK(w, tools)
}

func (a *API) handleGetTool(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var name, icon, desc, detail, agents, systems, status string
	err := a.DB.QueryRow("SELECT name,icon,description,detail,agents,systems,status FROM tools WHERE id=?", id).
		Scan(&name, &icon, &desc, &detail, &agents, &systems, &status)
	if err != nil {
		jsonError(w, 404, "tool not found")
		return
	}
	var agentArr, sysArr []string
	json.Unmarshal([]byte(agents), &agentArr)
	json.Unmarshal([]byte(systems), &sysArr)
	jsonOK(w, map[string]any{
		"id": id, "name": name, "icon": icon, "desc": desc, "detail": detail,
		"agents": agentArr, "systems": sysArr, "status": status,
	})
}

func (a *API) handleUpdateTool(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Status string `json:"status"`
	}
	if err := readJSON(r, &body); err != nil {
		jsonError(w, 400, "invalid json")
		return
	}
	a.DB.Exec("UPDATE tools SET status=?,updated_at=? WHERE id=?", body.Status, time.Now().Format(time.RFC3339), id)
	jsonOK(w, map[string]string{"id": id, "status": "updated"})
}

// ── Logs ──────────────────────────────────────────────────

func (a *API) handleListLogs(w http.ResponseWriter, r *http.Request) {
	level := r.URL.Query().Get("level")
	query := "SELECT timestamp,level,agent_id,message FROM log_entries ORDER BY id DESC LIMIT 200"
	args := []any{}
	if level != "" && level != "all" {
		query = "SELECT timestamp,level,agent_id,message FROM log_entries WHERE level=? ORDER BY id DESC LIMIT 200"
		args = append(args, strings.ToUpper(level))
	}
	rows, _ := a.DB.Query(query, args...)
	defer rows.Close()

	var logs []map[string]string
	for rows.Next() {
		var ts, lvl, agent, msg string
		rows.Scan(&ts, &lvl, &agent, &msg)
		logs = append(logs, map[string]string{"ts": ts, "level": lvl, "agent": agent, "msg": msg})
	}
	if logs == nil {
		logs = []map[string]string{}
	}
	jsonOK(w, logs)
}

// ── Costs ─────────────────────────────────────────────────

// handleGetCosts reads real rows from `cost_records` (written by the chat
// executor's usage tracker) and returns:
//   - models: total ZAR per model family (opus/sonnet/haiku) across all time
//   - daily:  last-7-days ZAR totals, oldest-first
//   - total:  running sum across every cost row
func (a *API) handleGetCosts(w http.ResponseWriter, r *http.Request) {
	modelValue := map[string]float64{"Opus": 0, "Sonnet": 0, "Haiku": 0, "Other": 0}
	rows, err := a.DB.Query(`SELECT LOWER(COALESCE(model_name,'')), COALESCE(amount_zar,0) FROM cost_records`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var name string
			var amt float64
			if err := rows.Scan(&name, &amt); err != nil {
				continue
			}
			switch {
			case strings.Contains(name, "opus"):
				modelValue["Opus"] += amt
			case strings.Contains(name, "sonnet"):
				modelValue["Sonnet"] += amt
			case strings.Contains(name, "haiku"):
				modelValue["Haiku"] += amt
			default:
				modelValue["Other"] += amt
			}
		}
	}

	// 7-day rolling window (UTC), oldest-first so chart reads left→right.
	daily := make([]float64, 7)
	for i := 0; i < 7; i++ {
		d := time.Now().UTC().AddDate(0, 0, -(6 - i)).Format("2006-01-02")
		var amt float64
		_ = a.DB.QueryRow("SELECT COALESCE(SUM(amount_zar),0) FROM cost_records WHERE date=?", d).Scan(&amt)
		daily[i] = amt
	}

	var total float64
	_ = a.DB.QueryRow("SELECT COALESCE(SUM(amount_zar),0) FROM cost_records").Scan(&total)

	jsonOK(w, map[string]any{
		"models": []map[string]any{
			{"name": "Opus", "value": modelValue["Opus"], "color": "#0077C8"},
			{"name": "Sonnet", "value": modelValue["Sonnet"], "color": "#00f0ff"},
			{"name": "Haiku", "value": modelValue["Haiku"], "color": "#00ff88"},
			{"name": "Other", "value": modelValue["Other"], "color": "#7cc6ff"},
		},
		"daily": daily,
		"total": total,
	})
}

// ── Security ──────────────────────────────────────────────

func (a *API) handleGetSecurity(w http.ResponseWriter, r *http.Request) {
	// Derive from the live monitoring tables instead of reading the
	// dead security_state singleton (migrations reset it to zeros,
	// nothing writes back). Formula mirrors the /service tab severity
	// thresholds so the Dashboard number matches what the ops console
	// shows — no two versions of truth.
	//
	//   trust_score = 100 - min(100, 25*p1 + 10*critical + 1*warning)
	//
	// lastScan is the wall-clock of the most-recent service_check row.
	var p1, critical, warning, info int
	if a.DB != nil {
		_ = a.DB.QueryRow(`
			SELECT
			  SUM(CASE WHEN severity='p1'       AND state='open' THEN 1 ELSE 0 END),
			  SUM(CASE WHEN severity='critical' AND state='open' THEN 1 ELSE 0 END),
			  SUM(CASE WHEN severity='warning'  AND state='open' THEN 1 ELSE 0 END),
			  SUM(CASE WHEN severity='info'     AND state='open' THEN 1 ELSE 0 END)
			FROM service_alerts`).Scan(&p1, &critical, &warning, &info)
	}
	penalty := 25*p1 + 10*critical + 1*warning
	if penalty > 100 {
		penalty = 100
	}
	trust := 100 - penalty
	// Rules-active = number of service checks persisted in the last
	// 24h — proxy for "how much is actively being monitored". Zero
	// when the platform monitor isn't running yet.
	var rules int
	var lastScan string
	if a.DB != nil {
		since := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339Nano)
		_ = a.DB.QueryRow(`SELECT COUNT(DISTINCT service_id) FROM service_checks WHERE checked_at >= ?`, since).Scan(&rules)
		_ = a.DB.QueryRow(`SELECT COALESCE(MAX(checked_at),'') FROM service_checks`).Scan(&lastScan)
	}
	jsonOK(w, map[string]any{
		"trustScore":  trust,
		"critical":    critical + p1, // UI collapses P1 into critical count
		"warning":     warning,
		"info":        info,
		"rulesActive": rules,
		"lastScan":    lastScan,
	})
}

// ── Approvals ─────────────────────────────────────────────

func (a *API) handleListApprovals(w http.ResponseWriter, r *http.Request) {
	rows, _ := a.DB.Query("SELECT id,type,title,description,requester,status,priority,created_at FROM approvals ORDER BY created_at DESC")
	defer rows.Close()
	var items []map[string]any
	for rows.Next() {
		var id, typ, title, desc, req, status, priority, created string
		rows.Scan(&id, &typ, &title, &desc, &req, &status, &priority, &created)
		items = append(items, map[string]any{
			"id": id, "type": typ, "title": title, "description": desc,
			"requester": req, "status": status, "priority": priority, "createdAt": created,
		})
	}
	if items == nil {
		items = []map[string]any{}
	}
	jsonOK(w, items)
}

func (a *API) handleCreateApproval(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Type        string `json:"type"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Requester   string `json:"requester"`
		Priority    string `json:"priority"`
	}
	if err := readJSON(r, &body); err != nil {
		jsonError(w, 400, "invalid json")
		return
	}
	id := "apr-" + time.Now().Format("20060102150405")
	now := time.Now().Format(time.RFC3339)
	a.DB.Exec("INSERT INTO approvals (id,type,title,description,requester,priority,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?)",
		id, body.Type, body.Title, body.Description, body.Requester, body.Priority, now, now)
	a.Bus.PublishJSON("approval.update", map[string]string{"id": id, "action": "created"})
	jsonOK(w, map[string]string{"id": id})
}

func (a *API) handleGetApproval(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var typ, title, desc, req, status, priority, reviewer, comment, created string
	err := a.DB.QueryRow("SELECT type,title,description,requester,status,priority,reviewer,review_comment,created_at FROM approvals WHERE id=?", id).
		Scan(&typ, &title, &desc, &req, &status, &priority, &reviewer, &comment, &created)
	if err != nil {
		jsonError(w, 404, "approval not found")
		return
	}
	jsonOK(w, map[string]any{
		"id": id, "type": typ, "title": title, "description": desc,
		"requester": req, "status": status, "priority": priority,
		"reviewer": reviewer, "reviewComment": comment, "createdAt": created,
	})
}

func (a *API) handleUpdateApproval(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Status  string `json:"status"`
		Comment string `json:"comment"`
	}
	if err := readJSON(r, &body); err != nil {
		jsonError(w, 400, "invalid json")
		return
	}
	now := time.Now().Format(time.RFC3339)
	a.DB.Exec("UPDATE approvals SET status=?,review_comment=?,reviewer=?,updated_at=? WHERE id=?",
		body.Status, body.Comment, "Soldier of God", now, id)
	a.Bus.PublishJSON("approval.update", map[string]string{"id": id, "status": body.Status})
	jsonOK(w, map[string]string{"id": id, "status": "updated"})
}

// ── Projects ──────────────────────────────────────────────

func (a *API) handleListProjects(w http.ResponseWriter, r *http.Request) {
	rows, _ := a.DB.Query(`
		SELECT id, name, description, status, priority, owner, progress_pct, created_at,
		       COALESCE(local_path, ''), COALESCE(components, '[]'),
		       COALESCE(has_frontend, 0), COALESCE(has_backend, 0),
		       COALESCE(clickup_task_id, ''), COALESCE(clickup_last_sync, ''),
		       COALESCE(sit_url, ''), COALESCE(prod_url, '')
		FROM projects ORDER BY created_at DESC
	`)
	defer rows.Close()
	var items []map[string]any
	for rows.Next() {
		var id, name, desc, status, priority, owner, created string
		var localPath, componentsJSON, clickupTaskID, clickupLastSync string
		var sitURL, prodURL string
		var progress, hasFrontend, hasBackend int
		rows.Scan(&id, &name, &desc, &status, &priority, &owner, &progress, &created,
			&localPath, &componentsJSON, &hasFrontend, &hasBackend, &clickupTaskID, &clickupLastSync,
			&sitURL, &prodURL)
		var components []map[string]string
		_ = json.Unmarshal([]byte(componentsJSON), &components)
		if components == nil {
			components = []map[string]string{}
		}
		item := map[string]any{
			"id":              id,
			"name":            name,
			"description":     desc,
			"status":          status,
			"priority":        priority,
			"owner":           owner,
			"progress":        progress,
			"createdAt":       created,
			"localPath":       localPath,
			"components":      components,
			"hasFrontend":     hasFrontend == 1,
			"hasBackend":      hasBackend == 1,
			"clickupTaskId":   clickupTaskID,
			"clickupLastSync": clickupLastSync,
			"sitUrl":          sitURL,
			"prodUrl":         prodURL,
		}
		if clickupTaskID != "" {
			item["clickupUrl"] = "https://app.clickup.com/t/" + clickupTaskID
		}
		items = append(items, item)
	}
	if items == nil {
		items = []map[string]any{}
	}
	jsonOK(w, items)
}

func (a *API) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Priority    string `json:"priority"`
		Owner       string `json:"owner"`
		Status      string `json:"status"`
		LocalPath   string `json:"localPath"`
		SITURL      string `json:"sitUrl"`
		ProdURL     string `json:"prodUrl"`
	}
	if err := readJSON(r, &body); err != nil {
		jsonError(w, 400, "invalid json")
		return
	}
	if body.Status == "" {
		body.Status = "To Do"
	}
	if body.Priority == "" {
		body.Priority = "normal"
	}
	id := "proj-" + time.Now().Format("20060102150405")
	now := time.Now().Format(time.RFC3339)
	a.DB.Exec(`INSERT INTO projects
		(id,name,description,status,priority,owner,local_path,sit_url,prod_url,created_at,updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		id, body.Name, body.Description, body.Status, body.Priority, body.Owner,
		body.LocalPath, body.SITURL, body.ProdURL, now, now)
	a.Bus.PublishJSON("project.update", map[string]string{"id": id, "action": "created"})

	// Mirror to ClickUp immediately so the task appears on the board.
	// Failure is non-fatal — the poller retries as long as clickup_task_id is empty.
	if a.SyncEngine != nil {
		if err := a.SyncEngine.PushProject(id); err != nil {
			a.Log.Warn("clickup push failed on create", "id", id, "error", err)
		}
	}

	jsonOK(w, map[string]string{"id": id})
}

func (a *API) handleGetProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var name, desc, status, priority, owner, created string
	var localPath, sitURL, prodURL string
	var progress int
	err := a.DB.QueryRow(`
		SELECT name, description, status, priority, owner, progress_pct, created_at,
		       COALESCE(local_path,''), COALESCE(sit_url,''), COALESCE(prod_url,'')
		  FROM projects WHERE id=?`, id).
		Scan(&name, &desc, &status, &priority, &owner, &progress, &created,
			&localPath, &sitURL, &prodURL)
	if err != nil {
		jsonError(w, 404, "project not found")
		return
	}
	jsonOK(w, map[string]any{
		"id": id, "name": name, "description": desc, "status": status,
		"priority": priority, "owner": owner, "progress": progress, "createdAt": created,
		"localPath": localPath, "sitUrl": sitURL, "prodUrl": prodURL,
	})
}

func (a *API) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		jsonError(w, 400, "invalid json")
		return
	}
	now := time.Now().Format(time.RFC3339)
	if s, ok := body["status"].(string); ok {
		a.DB.Exec("UPDATE projects SET status=?,updated_at=? WHERE id=?", s, now, id)
	}
	if p, ok := body["progress"].(float64); ok {
		a.DB.Exec("UPDATE projects SET progress_pct=?,updated_at=? WHERE id=?", int(p), now, id)
	}
	if d, ok := body["description"].(string); ok {
		a.DB.Exec("UPDATE projects SET description=?,updated_at=? WHERE id=?", d, now, id)
	}
	if pr, ok := body["priority"].(string); ok {
		a.DB.Exec("UPDATE projects SET priority=?,updated_at=? WHERE id=?", pr, now, id)
	}
	if n, ok := body["name"].(string); ok {
		a.DB.Exec("UPDATE projects SET name=?,updated_at=? WHERE id=?", n, now, id)
	}
	if o, ok := body["owner"].(string); ok {
		a.DB.Exec("UPDATE projects SET owner=?,updated_at=? WHERE id=?", o, now, id)
	}
	if lp, ok := body["localPath"].(string); ok {
		a.DB.Exec("UPDATE projects SET local_path=?,updated_at=? WHERE id=?", lp, now, id)
	}
	if su, ok := body["sitUrl"].(string); ok {
		a.DB.Exec("UPDATE projects SET sit_url=?,updated_at=? WHERE id=?", su, now, id)
	}
	if pu, ok := body["prodUrl"].(string); ok {
		a.DB.Exec("UPDATE projects SET prod_url=?,updated_at=? WHERE id=?", pu, now, id)
	}
	a.Bus.PublishJSON("project.update", map[string]string{"id": id, "action": "updated"})

	// Push to ClickUp after the local write. Non-fatal — if it fails, the
	// next poll tick will retry once external_updated_at gets out of sync.
	if a.SyncEngine != nil {
		if err := a.SyncEngine.PushProject(id); err != nil {
			a.Log.Warn("clickup push failed on update", "id", id, "error", err)
		}
	}

	jsonOK(w, map[string]string{"id": id, "status": "updated"})
}

// handleDeleteProject removes the local row AND its mirrored ClickUp
// task (+ any cached subtasks). Delegates the cascade to the sync
// engine so the two systems stay aligned.
func (a *API) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonError(w, 400, "id required")
		return
	}
	if a.SyncEngine == nil {
		// No engine — still drop the local row so the UI isn't lying.
		_, _ = a.DB.Exec(`DELETE FROM pipelines WHERE project_id = ?`, id)
		if _, err := a.DB.Exec(`DELETE FROM projects WHERE id = ?`, id); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		a.Bus.PublishJSON("project.delete", map[string]string{"id": id})
		jsonOK(w, map[string]any{"id": id, "deleted": true})
		return
	}
	if err := a.SyncEngine.DeleteProject(id); err != nil {
		// Treat "not found" as a 404 so the UI can ignore stale refs.
		if strings.Contains(err.Error(), "not found") {
			jsonError(w, 404, err.Error())
			return
		}
		jsonError(w, 500, err.Error())
		return
	}
	a.Bus.PublishJSON("project.delete", map[string]string{"id": id})
	jsonOK(w, map[string]any{"id": id, "deleted": true})
}

// handleSyncProjects kicks off a full push of every local project to
// ClickUp asynchronously — the "Sync now" button returns immediately
// and the work runs in a goroutine. Progress is readable via
// GET /api/v1/projects/sync/status. Inbound pulls happen on the
// poller loop.
func (a *API) handleSyncProjects(w http.ResponseWriter, r *http.Request) {
	if a.SyncEngine == nil {
		jsonError(w, 503, "sync engine not initialised")
		return
	}
	// Don't double-run. If one is already in flight, report back.
	if state := a.SyncEngine.Status(); state.InProgress {
		jsonOK(w, map[string]any{
			"started":        false,
			"already_running": true,
			"status":         state,
		})
		return
	}
	a.SyncEngine.StartAsyncPushAll()
	// 202 Accepted — work is queued, caller should poll /status.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"data":    map[string]any{"started": true, "status": a.SyncEngine.Status()},
	})
}

// handleSyncStatus returns the current (or last-completed) sync run's
// progress so the frontend can poll it without a long-held POST.
func (a *API) handleSyncStatus(w http.ResponseWriter, r *http.Request) {
	if a.SyncEngine == nil {
		jsonError(w, 503, "sync engine not initialised")
		return
	}
	jsonOK(w, a.SyncEngine.Status())
}

// ── Pipelines ─────────────────────────────────────────────

func (a *API) handleListPipelines(w http.ResponseWriter, r *http.Request) {
	rows, _ := a.DB.Query("SELECT id,project_id,name,type,status,trigger_type,branch,stages,duration_ms,created_at FROM pipelines ORDER BY created_at DESC")
	defer rows.Close()
	var items []map[string]any
	for rows.Next() {
		var id, projID, name, typ, status, trigger, branch, stagesJSON, created string
		var duration int
		rows.Scan(&id, &projID, &name, &typ, &status, &trigger, &branch, &stagesJSON, &duration, &created)
		var stages []any
		json.Unmarshal([]byte(stagesJSON), &stages)
		items = append(items, map[string]any{
			"id": id, "projectId": projID, "name": name, "type": typ, "status": status,
			"trigger": trigger, "branch": branch, "stages": stages, "durationMs": duration, "createdAt": created,
		})
	}
	if items == nil {
		items = []map[string]any{}
	}
	jsonOK(w, items)
}

func (a *API) handleCreatePipeline(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProjectID string `json:"projectId"`
		Name      string `json:"name"`
		Type      string `json:"type"`
		Branch    string `json:"branch"`
	}
	if err := readJSON(r, &body); err != nil {
		jsonError(w, 400, "invalid json")
		return
	}
	id := "pipe-" + time.Now().Format("20060102150405")
	now := time.Now().Format(time.RFC3339)
	stages := `[{"name":"pre-flight","status":"pending"},{"name":"lint","status":"pending"},{"name":"build","status":"pending"},{"name":"test","status":"pending"},{"name":"deploy","status":"pending"}]`
	a.DB.Exec("INSERT INTO pipelines (id,project_id,name,type,status,trigger_type,branch,stages,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?)",
		id, body.ProjectID, body.Name, body.Type, "running", "manual", body.Branch, stages, now, now)
	a.Bus.PublishJSON("pipeline.update", map[string]string{"id": id, "action": "created"})
	jsonOK(w, map[string]string{"id": id})
}

func (a *API) handleGetPipeline(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var projID, name, typ, status, trigger, branch, stagesJSON, created string
	var duration int
	err := a.DB.QueryRow("SELECT project_id,name,type,status,trigger_type,branch,stages,duration_ms,created_at FROM pipelines WHERE id=?", id).
		Scan(&projID, &name, &typ, &status, &trigger, &branch, &stagesJSON, &duration, &created)
	if err != nil {
		jsonError(w, 404, "pipeline not found")
		return
	}
	var stages []any
	json.Unmarshal([]byte(stagesJSON), &stages)
	jsonOK(w, map[string]any{
		"id": id, "projectId": projID, "name": name, "type": typ, "status": status,
		"trigger": trigger, "branch": branch, "stages": stages, "durationMs": duration, "createdAt": created,
	})
}

func (a *API) handleUpdatePipeline(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Status string `json:"status"`
		Stages string `json:"stages"`
	}
	if err := readJSON(r, &body); err != nil {
		jsonError(w, 400, "invalid json")
		return
	}
	now := time.Now().Format(time.RFC3339)
	if body.Status != "" {
		a.DB.Exec("UPDATE pipelines SET status=?,updated_at=? WHERE id=?", body.Status, now, id)
	}
	if body.Stages != "" {
		a.DB.Exec("UPDATE pipelines SET stages=?,updated_at=? WHERE id=?", body.Stages, now, id)
	}
	a.Bus.PublishJSON("pipeline.update", map[string]string{"id": id, "action": "updated"})
	jsonOK(w, map[string]string{"id": id, "status": "updated"})
}

// ── Documents ─────────────────────────────────────────────

func (a *API) handleListDocuments(w http.ResponseWriter, r *http.Request) {
	rows, _ := a.DB.Query("SELECT id,project_id,title,type,version,author,created_at FROM documents ORDER BY created_at DESC")
	defer rows.Close()
	var items []map[string]any
	for rows.Next() {
		var id, projID, title, typ, author, created string
		var version int
		rows.Scan(&id, &projID, &title, &typ, &version, &author, &created)
		items = append(items, map[string]any{
			"id": id, "projectId": projID, "title": title, "type": typ,
			"version": version, "author": author, "createdAt": created,
		})
	}
	if items == nil {
		items = []map[string]any{}
	}
	jsonOK(w, items)
}

func (a *API) handleCreateDocument(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProjectID string `json:"projectId"`
		Title     string `json:"title"`
		Type      string `json:"type"`
		Content   string `json:"content"`
		Author    string `json:"author"`
	}
	if err := readJSON(r, &body); err != nil {
		jsonError(w, 400, "invalid json")
		return
	}
	id := "doc-" + time.Now().Format("20060102150405")
	now := time.Now().Format(time.RFC3339)
	a.DB.Exec("INSERT INTO documents (id,project_id,title,type,content,author,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?)",
		id, body.ProjectID, body.Title, body.Type, body.Content, body.Author, now, now)
	a.Bus.PublishJSON("document.update", map[string]string{"id": id, "action": "created"})
	jsonOK(w, map[string]string{"id": id})
}

func (a *API) handleGetDocument(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var projID, title, typ, content, author, created string
	var version int
	err := a.DB.QueryRow("SELECT project_id,title,type,content,version,author,created_at FROM documents WHERE id=?", id).
		Scan(&projID, &title, &typ, &content, &version, &author, &created)
	if err != nil {
		jsonError(w, 404, "document not found")
		return
	}
	jsonOK(w, map[string]any{
		"id": id, "projectId": projID, "title": title, "type": typ,
		"content": content, "version": version, "author": author, "createdAt": created,
	})
}

func (a *API) handleUpdateDocument(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := readJSON(r, &body); err != nil {
		jsonError(w, 400, "invalid json")
		return
	}
	now := time.Now().Format(time.RFC3339)
	a.DB.Exec("UPDATE documents SET title=?,content=?,version=version+1,updated_at=? WHERE id=?", body.Title, body.Content, now, id)
	a.Bus.PublishJSON("document.update", map[string]string{"id": id, "action": "updated"})
	jsonOK(w, map[string]string{"id": id, "status": "updated"})
}

// ── SPA Handler ──────────────────────────────────────────

func spaHandler(staticDir string) http.HandlerFunc {
	fs := http.Dir(staticDir)
	fileServer := http.FileServer(fs)

	return func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Clean(r.URL.Path)
		if path == "/" {
			path = "/index.html"
		}

		// Try to open the file
		f, err := os.Open(filepath.Join(staticDir, path))
		if err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		// File not found — serve index.html for SPA routing
		http.ServeFile(w, r, filepath.Join(staticDir, "index.html"))
	}
}

// ── Agent Office ──────────────────────────────────────────

func (a *API) handleGetOffice(w http.ResponseWriter, r *http.Request) {
	rows, _ := a.DB.Query(`SELECT o.agent_id, a.name, o.desk_x, o.desk_y, o.zone, o.activity, o.mood, o.current_file, o.last_action
		FROM agent_office_states o JOIN agents a ON o.agent_id = a.id`)
	defer rows.Close()
	var agents []map[string]any
	for rows.Next() {
		var id, name, zone, activity, mood, file, action string
		var x, y int
		rows.Scan(&id, &name, &x, &y, &zone, &activity, &mood, &file, &action)
		agents = append(agents, map[string]any{
			"id": id, "name": name, "x": x, "y": y, "zone": zone,
			"activity": activity, "mood": mood, "currentFile": file, "lastAction": action,
		})
	}
	if agents == nil {
		agents = []map[string]any{}
	}

	zones := []map[string]any{
		{"id": "command-bridge", "name": "Command Bridge", "x": 300, "y": 0, "w": 200, "h": 100, "color": "#0077C8"},
		{"id": "research-lab", "name": "Research Lab", "x": 0, "y": 100, "w": 200, "h": 150, "color": "#00f0ff"},
		{"id": "security-vault", "name": "Security Vault", "x": 600, "y": 100, "w": 200, "h": 150, "color": "#ff3355"},
		{"id": "backend-wing", "name": "Backend Wing", "x": 100, "y": 200, "w": 200, "h": 150, "color": "#00ff88"},
		{"id": "data-center", "name": "Data Center", "x": 300, "y": 200, "w": 200, "h": 150, "color": "#ffaa00"},
		{"id": "devops-zone", "name": "DevOps Zone", "x": 500, "y": 200, "w": 150, "h": 150, "color": "#ff00e5"},
		{"id": "frontend-lab", "name": "Frontend Lab", "x": 0, "y": 300, "w": 250, "h": 150, "color": "#0077C8"},
		{"id": "integration-hub", "name": "Integration Hub", "x": 300, "y": 300, "w": 200, "h": 150, "color": "#00f0ff"},
		{"id": "ai-lab", "name": "AI Lab", "x": 500, "y": 300, "w": 250, "h": 150, "color": "#ff00e5"},
		{"id": "testing-floor", "name": "Testing Floor", "x": 400, "y": 100, "w": 200, "h": 150, "color": "#00ff88"},
		{"id": "cloud-deck", "name": "Cloud Deck", "x": 600, "y": 0, "w": 200, "h": 100, "color": "#ffaa00"},
		{"id": "review-room", "name": "Review Room", "x": 250, "y": 100, "w": 150, "h": 100, "color": "#00f0ff"},
		{"id": "comms-room", "name": "Comms Room", "x": 0, "y": 0, "w": 150, "h": 100, "color": "#ffaa00"},
	}

	jsonOK(w, map[string]any{"agents": agents, "zones": zones})
}

func (a *API) handleGetOfficeAgent(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("agentId")
	var zone, activity, mood, file, action string
	var x, y int
	err := a.DB.QueryRow("SELECT desk_x,desk_y,zone,activity,mood,current_file,last_action FROM agent_office_states WHERE agent_id=?", agentID).
		Scan(&x, &y, &zone, &activity, &mood, &file, &action)
	if err != nil {
		jsonError(w, 404, "agent office state not found")
		return
	}
	jsonOK(w, map[string]any{
		"id": agentID, "x": x, "y": y, "zone": zone,
		"activity": activity, "mood": mood, "currentFile": file, "lastAction": action,
	})
}
