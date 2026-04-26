package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/SoldierOfGod1/command-centre/internal/clickup"
	"github.com/SoldierOfGod1/command-centre/internal/store"
)

// RegisterClickUpRoutes wires ClickUp endpoints. Credentials live in the
// app_settings table (populated from TOML on first boot, mutable via the
// Settings page) so the user can swap workspace/list IDs at runtime without
// a backend restart.
func RegisterClickUpRoutes(mux *http.ServeMux, api *API) {
	mux.HandleFunc("GET /api/v1/clickup/config", api.handleClickUpConfig)
	mux.HandleFunc("GET /api/v1/clickup/tasks", api.handleClickUpListTasks)
	mux.HandleFunc("POST /api/v1/clickup/tasks", api.handleClickUpCreateTask)
	mux.HandleFunc("PATCH /api/v1/clickup/tasks/{id}", api.handleClickUpUpdateTask)
	// Force-reapply the 10 rain statuses to the currently-configured
	// list. Idempotent. Called by the "Sync Statuses" button or invoked
	// automatically after a list_id change in settings.
	mux.HandleFunc("POST /api/v1/clickup/ensure-statuses", api.handleClickUpEnsureStatuses)
}

// handleClickUpEnsureStatuses calls clickup.EnsureListStatuses on the
// configured list. Idempotent — if the list already has the 10 rain
// statuses in the right order it's a no-op and returns updated=false.
func (a *API) handleClickUpEnsureStatuses(w http.ResponseWriter, r *http.Request) {
	token, _, listID, ok := a.clickupCreds()
	if !ok {
		jsonError(w, http.StatusServiceUnavailable, "clickup not configured")
		return
	}
	client := clickup.New(token)
	ensureErr := client.EnsureListStatuses(listID, clickup.ProjectStatuses)
	// Always fetch the live state so the caller can see what ClickUp
	// actually has after the attempt — even if the PUT errored.
	live, overrides, _ := client.ListStatuses(listID)
	resp := map[string]any{
		"list_id":           listID,
		"wanted":            clickup.ProjectStatuses,
		"live":              live,
		"override_statuses": overrides,
		"ok":                ensureErr == nil,
	}
	if ensureErr != nil {
		resp["error"] = ensureErr.Error()
		w.WriteHeader(http.StatusBadGateway)
	}
	jsonOK(w, resp)
}

// clickupCreds loads the most up-to-date token/workspace/list from the
// settings table. Returning `ok=false` lets callers short-circuit to a 503
// when ClickUp isn't configured yet.
func (a *API) clickupCreds() (token, workspaceID, listID string, ok bool) {
	all, err := a.Store.GetAllSettings()
	if err != nil {
		a.Log.Warn("clickup: read settings", "error", err)
		return "", "", "", false
	}
	token = all[store.SettingClickUpToken]
	workspaceID = all[store.SettingClickUpWorkspaceID]
	listID = all[store.SettingClickUpListID]
	ok = token != "" && listID != ""
	return
}

func (a *API) handleClickUpConfig(w http.ResponseWriter, r *http.Request) {
	token, workspaceID, listID, _ := a.clickupCreds()
	jsonOK(w, map[string]any{
		"configured":   token != "" && listID != "",
		"workspace_id": workspaceID,
		"list_id":      listID,
	})
}

func (a *API) handleClickUpListTasks(w http.ResponseWriter, r *http.Request) {
	token, _, listID, ok := a.clickupCreds()
	if !ok {
		jsonError(w, http.StatusServiceUnavailable, "ClickUp not configured — set token + list id in Settings")
		return
	}
	client := clickup.New(token)
	tasks, err := client.ListTasks(listID)
	if err != nil {
		if errors.Is(err, clickup.ErrNotConfigured) {
			jsonError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonOK(w, tasks)
}

func (a *API) handleClickUpCreateTask(w http.ResponseWriter, r *http.Request) {
	token, _, listID, ok := a.clickupCreds()
	if !ok {
		jsonError(w, http.StatusServiceUnavailable, "ClickUp not configured")
		return
	}
	var in clickup.CreateTaskInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if in.Name == "" {
		jsonError(w, http.StatusBadRequest, "name is required")
		return
	}
	client := clickup.New(token)
	task, err := client.CreateTask(listID, in)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonOK(w, task)
}

func (a *API) handleClickUpUpdateTask(w http.ResponseWriter, r *http.Request) {
	token, _, _, ok := a.clickupCreds()
	if !ok {
		jsonError(w, http.StatusServiceUnavailable, "ClickUp not configured")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		jsonError(w, http.StatusBadRequest, "task id required")
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Status == "" {
		jsonError(w, http.StatusBadRequest, "status is required")
		return
	}
	client := clickup.New(token)
	if err := client.UpdateTaskStatus(id, body.Status); err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonOK(w, map[string]string{"id": id, "status": body.Status})
}
