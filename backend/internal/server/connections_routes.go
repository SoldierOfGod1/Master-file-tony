package server

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/SoldierOfGod1/command-centre/internal/store"
)

// RegisterConnectionsRoutes wires the multi-DB connection registry CRUD.
func RegisterConnectionsRoutes(mux *http.ServeMux, api *API) {
	mux.HandleFunc("GET /api/v1/connections", api.handleListConnections)
	mux.HandleFunc("POST /api/v1/connections", api.handleUpsertConnection)
	mux.HandleFunc("PUT /api/v1/connections/{id}", api.handleUpdateConnection)
	mux.HandleFunc("DELETE /api/v1/connections/{id}", api.handleDeleteConnection)
	mux.HandleFunc("POST /api/v1/connections/{id}/primary", api.handleSetPrimary)
	mux.HandleFunc("POST /api/v1/connections/{id}/test", api.handleTestConnection)
}

// publicConnection masks the password on the wire — full value stays in
// the local config DB. Every GET response uses this view.
type publicConnection struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Driver    string `json:"driver"`
	Host      string `json:"host"`
	Port      string `json:"port"`
	Database  string `json:"database"`
	User      string `json:"user"`
	Password  string `json:"password"` // always masked in GET responses
	SSLMode   string `json:"ssl_mode"`
	IsPrimary bool   `json:"is_primary"`
	Filled    bool   `json:"filled"`
}

func toPublic(c store.Connection) publicConnection {
	return publicConnection{
		ID: c.ID, Label: c.Label, Driver: c.Driver,
		Host: c.Host, Port: c.Port, Database: c.Database, User: c.User,
		Password: c.MaskedPassword(), SSLMode: c.SSLMode,
		IsPrimary: c.IsPrimary, Filled: c.Filled(),
	}
}

func (a *API) handleListConnections(w http.ResponseWriter, r *http.Request) {
	conns, err := a.Store.ListConnections()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]publicConnection, 0, len(conns))
	for _, c := range conns {
		out = append(out, toPublic(c))
	}
	jsonOK(w, out)
}

// handleUpsertConnection accepts a body with or without an id. Missing id
// triggers a slug derived from the label — avoids the frontend having to
// generate ids for a "create new" flow.
func (a *API) handleUpsertConnection(w http.ResponseWriter, r *http.Request) {
	var body store.Connection
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.ID == "" {
		body.ID = slugify(body.Label)
	}
	if body.ID == "" {
		jsonError(w, http.StatusBadRequest, "id or label required")
		return
	}
	if body.Driver == "" {
		body.Driver = "postgres"
	}
	if err := a.Store.UpsertConnection(body); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Rebuild the pool next time — creds may have changed.
	if a.CustomerMgr != nil {
		a.CustomerMgr.Invalidate(body.ID)
	}
	after, _, _ := a.Store.GetConnection(body.ID)
	jsonOK(w, toPublic(after))
}

// handleUpdateConnection is PUT-style; body must include the new field
// values. id comes from the URL.
func (a *API) handleUpdateConnection(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body store.Connection
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	body.ID = id
	if err := a.Store.UpsertConnection(body); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if a.CustomerMgr != nil {
		a.CustomerMgr.Invalidate(id)
	}
	after, _, _ := a.Store.GetConnection(id)
	jsonOK(w, toPublic(after))
}

func (a *API) handleDeleteConnection(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := a.Store.DeleteConnection(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if a.CustomerMgr != nil {
		a.CustomerMgr.Invalidate(id)
	}
	jsonOK(w, map[string]string{"id": id, "status": "deleted"})
}

func (a *API) handleSetPrimary(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	conns, err := a.Store.ListConnections()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	found := false
	for i := range conns {
		conns[i].IsPrimary = conns[i].ID == id
		if conns[i].IsPrimary {
			found = true
		}
	}
	if !found {
		jsonError(w, http.StatusNotFound, "connection not found")
		return
	}
	if err := a.Store.SaveConnections(conns); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]string{"primary": id})
}

// handleTestConnection pings the DB behind the given id. Returns 200 with
// a one-line OK message on success, 502 with the real pg error on failure.
func (a *API) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if a.CustomerMgr == nil {
		jsonError(w, http.StatusServiceUnavailable, "customer manager not initialised")
		return
	}
	if err := a.CustomerMgr.TestConnection(r.Context(), id); err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonOK(w, map[string]string{"id": id, "status": "ok"})
}

// slugify turns a label like "Axiom BSS · SIT" into "axiom-bss-sit" so it
// can be used as an id. Idempotent.
var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(label string) string {
	s := strings.ToLower(strings.TrimSpace(label))
	s = nonAlnum.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}
