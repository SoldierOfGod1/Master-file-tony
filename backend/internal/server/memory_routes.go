package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/SoldierOfGod1/command-centre/internal/chat"
)

// RegisterMemoryRoutes wires the agent_memory CRUD endpoints.
// Operator-facing — lets you inspect / add / scrub entries that
// the agent reads at the start of every run. Writes gated by
// RAIN_SUPPORT_L2 (same envelope as IMSI override and budget caps)
// because a poisoned memory entry can quietly mis-steer every
// subsequent agent decision for that user.
func RegisterMemoryRoutes(mux *http.ServeMux, api *API) {
	mux.HandleFunc("GET /api/v1/memory", api.handleListMemory)
	mux.HandleFunc("POST /api/v1/memory", api.handleCreateMemory)
	mux.HandleFunc("DELETE /api/v1/memory/{id}", api.handleDeleteMemory)
}

// handleListMemory returns all memory rows, optionally filtered
// by ?user_id=. No pagination — the store hard-caps at 500 rows
// in chat.ListMemory.
func (a *API) handleListMemory(w http.ResponseWriter, r *http.Request) {
	if a.DB == nil {
		jsonError(w, 503, "db unavailable")
		return
	}
	user := strings.TrimSpace(r.URL.Query().Get("user_id"))
	jsonOK(w, chat.ListMemory(a.DB, user))
}

// handleCreateMemory lets the operator manually pin a memory
// entry — useful for seeding ("user prefers concise replies") or
// teaching the agent without round-tripping through the chat.
// Same write-gate envelope as the rest of the operator surface.
func (a *API) handleCreateMemory(w http.ResponseWriter, r *http.Request) {
	if !overrideWritesEnabled() {
		jsonError(w, 403, "memory edits disabled — set RAIN_SUPPORT_L2=true to enable")
		return
	}
	if a.DB == nil {
		jsonError(w, 503, "db unavailable")
		return
	}
	var body struct {
		UserID string `json:"user_id"`
		Kind   string `json:"kind"`
		Body   string `json:"body"`
	}
	if err := readJSON(r, &body); err != nil {
		jsonError(w, 400, "invalid json")
		return
	}
	id, err := chat.WriteMemory(a.DB, strings.TrimSpace(body.UserID), body.Kind, body.Body)
	if err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	jsonOK(w, map[string]any{"id": id})
}

// handleDeleteMemory scrubs a single entry. Returns 404 if the
// row didn't exist — useful for the UI to detect stale list
// state and refresh.
func (a *API) handleDeleteMemory(w http.ResponseWriter, r *http.Request) {
	if !overrideWritesEnabled() {
		jsonError(w, 403, "memory edits disabled — set RAIN_SUPPORT_L2=true to enable")
		return
	}
	if a.DB == nil {
		jsonError(w, 503, "db unavailable")
		return
	}
	idStr := strings.TrimSpace(r.PathValue("id"))
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		jsonError(w, 400, "id must be a positive integer")
		return
	}
	ok, err := chat.DeleteMemory(a.DB, id)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	if !ok {
		jsonError(w, 404, "no memory entry with that id")
		return
	}
	jsonOK(w, map[string]any{"id": id, "deleted": true})
}
