package server

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/SoldierOfGod1/command-centre/internal/platforms"
)

// RegisterPlatformsRoutes exposes the full rain Service surface —
// service health, DB health, alerts, incidents, and per-service
// history. The Dashboard's PlatformMonitorPanel and the /service
// page both call into these; the frontend decides which subset to
// render based on `criticality`.
func RegisterPlatformsRoutes(mux *http.ServeMux, api *API) {
	mux.HandleFunc("GET /api/v1/platforms", api.handleListPlatforms)
	mux.HandleFunc("GET /api/v1/platforms/health", api.handlePlatformsHealth)
	mux.HandleFunc("GET /api/v1/platforms/databases", api.handlePlatformsDatabases)
	mux.HandleFunc("GET /api/v1/platforms/alerts", api.handlePlatformsAlerts)
	// Manual resolve for stuck alerts — services that haven't fired
	// a 'recovered' Emit (watcher restarted, env disabled) leave rows
	// open forever otherwise.
	mux.HandleFunc("POST /api/v1/platforms/alerts/{id}/resolve", api.handlePlatformAlertResolve)
	mux.HandleFunc("GET /api/v1/platforms/incidents", api.handlePlatformsIncidents)
	mux.HandleFunc("POST /api/v1/platforms/incidents/{id}/ack", api.handlePlatformIncidentAck)
	mux.HandleFunc("POST /api/v1/platforms/incidents/{id}/resolve", api.handlePlatformIncidentResolve)
	// Phase D2 — incident correlation rollup. Returns every
	// conversation, audit row, approval, and spend record tagged
	// with the given incident_id, so an operator (or the agent)
	// can reconstruct everything we did during one incident.
	mux.HandleFunc("GET /api/v1/platforms/incidents/{id}/timeline", api.handleIncidentTimeline)
}

// handleIncidentTimeline rolls up everything tagged with this
// incident_id across the system. Phase D2 of the agent-orchestrator
// plan. Cheap-but-honest: each table queried independently with a
// LIMIT cap; no joins. Returns the four lists separately so the
// frontend (or the agent) can render each section as needed.
func (a *API) handleIncidentTimeline(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		jsonError(w, 400, "incident id required")
		return
	}
	if a.DB == nil {
		jsonError(w, 503, "db unavailable")
		return
	}
	out := map[string]any{
		"incident_id":   id,
		"conversations": queryRows(a.DB, `SELECT id, title, project_dir, source, status, user_id, created_at FROM conversations WHERE incident_id = ? ORDER BY created_at DESC LIMIT 50`, id),
		"approvals":     queryRows(a.DB, `SELECT id, type, title, status, requester, priority, created_at FROM approvals WHERE incident_id = ? ORDER BY created_at DESC LIMIT 50`, id),
		"imsi_audits":   queryRows(a.DB, `SELECT id, individual_id, source, winning_phase, imsi_count, response_code, at FROM imsi_lookup_audit WHERE incident_id = ? ORDER BY at DESC LIMIT 100`, id),
		"cost_records":  queryRows(a.DB, `SELECT id, date, model_name, amount_zar, tokens_used, conversation_id, user_id FROM cost_records WHERE incident_id = ? ORDER BY date DESC LIMIT 100`, id),
	}
	// Total spend for the incident — useful summary for the
	// frontend tile. Cheap aggregate query.
	var totalZAR float64
	_ = a.DB.QueryRow(`SELECT COALESCE(SUM(amount_zar), 0) FROM cost_records WHERE incident_id = ?`, id).Scan(&totalZAR)
	out["total_zar"] = totalZAR
	jsonOK(w, out)
}

// queryRows is a tiny untyped helper — the timeline endpoint
// returns four heterogeneous tables and we don't want four
// strongly-typed structs cluttering this file. Each row is
// rendered as map[string]any keyed by the SQL column name.
func queryRows(db *sql.DB, q string, args ...any) []map[string]any {
	rows, err := db.Query(q, args...)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return []map[string]any{}
	}
	out := []map[string]any{}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			continue
		}
		row := map[string]any{}
		for i, c := range cols {
			v := vals[i]
			// SQLite returns []byte for TEXT — convert so JSON
			// renders as a string rather than base64.
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			row[c] = v
		}
		out = append(out, row)
	}
	return out
}

// handleListPlatforms returns the static catalogue (no live status).
func (a *API) handleListPlatforms(w http.ResponseWriter, r *http.Request) {
	if a.PlatformMon == nil {
		jsonOK(w, []platforms.Target{})
		return
	}
	jsonOK(w, a.PlatformMon.Targets())
}

// handlePlatformsHealth returns every target's latest snapshot.
// The Dashboard tile filters client-side to criticality=top; the
// /service page renders all rows.
func (a *API) handlePlatformsHealth(w http.ResponseWriter, r *http.Request) {
	if a.PlatformMon == nil {
		jsonOK(w, []platforms.Status{})
		return
	}
	jsonOK(w, a.PlatformMon.Snapshot())
}

// handlePlatformsDatabases returns the latest DB health snapshot —
// Axiom pinned first by the monitor's own sort.
func (a *API) handlePlatformsDatabases(w http.ResponseWriter, r *http.Request) {
	if a.DBHealth == nil {
		jsonOK(w, []platforms.DatabaseHealth{})
		return
	}
	jsonOK(w, a.DBHealth.Snapshot())
}

// handlePlatformsAlerts returns alerts — `state` filter optional
// ("open" | "resolved" | ""), `limit` defaults to 100.
func (a *API) handlePlatformsAlerts(w http.ResponseWriter, r *http.Request) {
	if a.DB == nil {
		jsonOK(w, []platforms.StoredAlert{})
		return
	}
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	alerts, err := platforms.ListAlerts(r.Context(), a.DB, state, limit)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if alerts == nil {
		alerts = []platforms.StoredAlert{}
	}
	jsonOK(w, alerts)
}

// handlePlatformsIncidents returns incidents with timeline inlined.
func (a *API) handlePlatformsIncidents(w http.ResponseWriter, r *http.Request) {
	if a.DB == nil {
		jsonOK(w, []platforms.Incident{})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	incs, err := platforms.ListIncidents(r.Context(), a.DB, limit)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if incs == nil {
		incs = []platforms.Incident{}
	}
	jsonOK(w, incs)
}

func (a *API) handlePlatformIncidentAck(w http.ResponseWriter, r *http.Request) {
	if a.DB == nil {
		jsonError(w, http.StatusServiceUnavailable, "db unavailable")
		return
	}
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		jsonError(w, http.StatusBadRequest, "bad incident id")
		return
	}
	note := strings.TrimSpace(r.URL.Query().Get("note"))
	if err := platforms.AckIncident(r.Context(), a.DB, id, note); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

func (a *API) handlePlatformIncidentResolve(w http.ResponseWriter, r *http.Request) {
	if a.DB == nil {
		jsonError(w, http.StatusServiceUnavailable, "db unavailable")
		return
	}
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		jsonError(w, http.StatusBadRequest, "bad incident id")
		return
	}
	note := strings.TrimSpace(r.URL.Query().Get("note"))
	if err := platforms.ResolveIncident(r.Context(), a.DB, id, note); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

// handlePlatformAlertResolve manually closes a service_alerts row.
// Only "open" alerts move; resolved ones keep their original
// resolved_at. The frontend's × Resolve button hits this.
func (a *API) handlePlatformAlertResolve(w http.ResponseWriter, r *http.Request) {
	if a.DB == nil {
		jsonError(w, http.StatusServiceUnavailable, "db unavailable")
		return
	}
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		jsonError(w, http.StatusBadRequest, "bad alert id")
		return
	}
	if err := platforms.ResolveAlert(r.Context(), a.DB, id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}
