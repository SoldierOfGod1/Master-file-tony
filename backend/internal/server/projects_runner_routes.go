package server

import (
	"net/http"
	"strconv"
)

// RegisterProjectsRunnerRoutes wires start/stop/status/logs for the
// in-app dev-server launcher. The Runner is optional — if the API
// wasn't configured with one, each handler returns 503 so the frontend
// can downgrade gracefully.
func RegisterProjectsRunnerRoutes(mux *http.ServeMux, api *API) {
	mux.HandleFunc("POST /api/v1/projects/{id}/run", api.handleProjectRun)
	mux.HandleFunc("POST /api/v1/projects/{id}/stop", api.handleProjectStop)
	mux.HandleFunc("GET  /api/v1/projects/{id}/runner", api.handleProjectRunnerStatus)
	mux.HandleFunc("GET  /api/v1/projects/{id}/runner/logs", api.handleProjectRunnerLogs)
	mux.HandleFunc("GET  /api/v1/runner", api.handleRunnerAll)
}

func (a *API) handleProjectRun(w http.ResponseWriter, r *http.Request) {
	if a.Runner == nil {
		jsonError(w, 503, "runner not configured")
		return
	}
	id := r.PathValue("id")
	var localPath string
	err := a.DB.QueryRow("SELECT COALESCE(local_path,'') FROM projects WHERE id=?", id).Scan(&localPath)
	if err != nil {
		jsonError(w, 404, "project not found")
		return
	}
	if localPath == "" {
		jsonError(w, 400, "project has no local_path — edit the project to add one")
		return
	}
	g, err := a.Runner.Start(id, localPath)
	if err != nil {
		// Still 200 if the group was partially started — the payload
		// has the per-process state the UI uses to show what failed.
		if g != nil {
			jsonError(w, 409, err.Error())
			return
		}
		jsonError(w, 400, err.Error())
		return
	}
	jsonOK(w, g)
}

func (a *API) handleProjectStop(w http.ResponseWriter, r *http.Request) {
	if a.Runner == nil {
		jsonError(w, 503, "runner not configured")
		return
	}
	id := r.PathValue("id")
	if err := a.Runner.Stop(id); err != nil {
		jsonError(w, 404, err.Error())
		return
	}
	jsonOK(w, map[string]string{"id": id, "status": "stopping"})
}

func (a *API) handleProjectRunnerStatus(w http.ResponseWriter, r *http.Request) {
	if a.Runner == nil {
		jsonError(w, 503, "runner not configured")
		return
	}
	id := r.PathValue("id")
	g, ok := a.Runner.Status(id)
	if !ok {
		jsonOK(w, map[string]any{"project_id": id, "processes": []any{}})
		return
	}
	jsonOK(w, g)
}

func (a *API) handleProjectRunnerLogs(w http.ResponseWriter, r *http.Request) {
	if a.Runner == nil {
		jsonError(w, 503, "runner not configured")
		return
	}
	id := r.PathValue("id")
	idx := 0
	if v := r.URL.Query().Get("component"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			idx = n
		}
	}
	tail := 200
	if v := r.URL.Query().Get("tail"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 2000 {
			tail = n
		}
	}
	jsonOK(w, a.Runner.Logs(id, idx, tail))
}

func (a *API) handleRunnerAll(w http.ResponseWriter, r *http.Request) {
	if a.Runner == nil {
		jsonError(w, 503, "runner not configured")
		return
	}
	jsonOK(w, a.Runner.All())
}
