package server

import (
	"encoding/json"
	"net/http"
)

// RegisterLoopsRoutes exposes the Loop Operator surface: list active queue
// workers, pause/resume a queue, and kill the currently-running CLI.
func RegisterLoopsRoutes(mux *http.ServeMux, api *API) {
	mux.HandleFunc("GET /api/v1/loops", api.handleListLoops)
	mux.HandleFunc("POST /api/v1/loops/pause", api.handlePauseLoop)
	mux.HandleFunc("POST /api/v1/loops/kill", api.handleKillLoop)
}

func (a *API) handleListLoops(w http.ResponseWriter, r *http.Request) {
	if a.QueueMgr == nil {
		jsonOK(w, []any{})
		return
	}
	jsonOK(w, a.QueueMgr.ListActive())
}

type pauseBody struct {
	ProjectDir string `json:"project_dir"`
	Paused     bool   `json:"paused"`
}

func (a *API) handlePauseLoop(w http.ResponseWriter, r *http.Request) {
	if a.QueueMgr == nil {
		jsonError(w, http.StatusServiceUnavailable, "queue not initialised")
		return
	}
	var body pauseBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.ProjectDir == "" {
		jsonError(w, http.StatusBadRequest, "project_dir required")
		return
	}
	a.QueueMgr.SetPaused(body.ProjectDir, body.Paused)
	jsonOK(w, map[string]any{"project_dir": body.ProjectDir, "paused": body.Paused})
}

type killBody struct {
	ProjectDir string `json:"project_dir"`
}

func (a *API) handleKillLoop(w http.ResponseWriter, r *http.Request) {
	if a.QueueMgr == nil {
		jsonError(w, http.StatusServiceUnavailable, "queue not initialised")
		return
	}
	var body killBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.ProjectDir == "" {
		jsonError(w, http.StatusBadRequest, "project_dir required")
		return
	}
	killed := a.QueueMgr.Kill(body.ProjectDir)
	jsonOK(w, map[string]any{"project_dir": body.ProjectDir, "killed": killed})
}
