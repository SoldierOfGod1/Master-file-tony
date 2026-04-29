package server

import (
	"context"
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
	// Copy the stored password from one connection to another. Handy when
	// the same LDAP secret backs several clusters — user enters it once
	// against (say) SIT, then clones it across without re-typing.
	// The plaintext never leaves the server process.
	mux.HandleFunc("POST /api/v1/connections/{id}/copy-password", api.handleCopyPassword)
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

// handleTestConnection pings the DB behind the given id. Returns 200
// with a one-line OK on success, 502 with the underlying error on
// failure. Dispatches by driver:
//   - postgres   → customer.Manager pgxpool ping
//   - clickhouse → darknoc.ClickHouseAdapter.TestConnection (HTTP)
//   - grafana    → darknoc.GrafanaProxy.TestConnection (Bearer + /api/user)
//
// The customer.Manager path was the original implementation and only
// understood postgres — testing a clickhouse or grafana row through
// it returned ErrNotConfigured / ErrClickHouseUnsupported and made
// the Settings UI report "test failed — see backend log" with no
// useful detail. This dispatcher fixes that.
func (a *API) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, ok, err := a.Store.GetConnection(id)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		jsonError(w, http.StatusNotFound, "connection "+id+" not found")
		return
	}
	switch strings.ToLower(strings.TrimSpace(c.Driver)) {
	case "postgres", "":
		if a.CustomerMgr == nil {
			jsonError(w, http.StatusServiceUnavailable, "customer manager not initialised")
			return
		}
		if err := a.CustomerMgr.TestConnection(r.Context(), id); err != nil {
			jsonError(w, http.StatusBadGateway, err.Error())
			return
		}
	case "clickhouse":
		// Cast through the Connector interface to the concrete
		// adapter so we can call TestConnection on a fresh row
		// (not the cached `clickhouse-prod`).
		ch, isCH := a.DarkNoc.(interface {
			TestConnection(ctx context.Context, c store.Connection) error
		})
		if !isCH {
			jsonError(w, http.StatusServiceUnavailable, "clickhouse adapter not initialised")
			return
		}
		if err := ch.TestConnection(r.Context(), c); err != nil {
			jsonError(w, http.StatusBadGateway, err.Error())
			return
		}
	case "grafana":
		if a.DarkNocGrafana == nil {
			jsonError(w, http.StatusServiceUnavailable, "grafana proxy not initialised")
			return
		}
		if err := a.DarkNocGrafana.TestConnection(r.Context(), c); err != nil {
			jsonError(w, http.StatusBadGateway, err.Error())
			return
		}
	default:
		jsonError(w, http.StatusBadRequest, "unsupported driver: "+c.Driver)
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

// handleCopyPassword copies the stored password from the source
// connection (query param `source`) into the target connection (URL
// path `id`). Server-side only — the plaintext never enters a request
// body or response body. Intended for "I set up one cluster with LDAP,
// I want the same creds on the other four" flows.
func (a *API) handleCopyPassword(w http.ResponseWriter, r *http.Request) {
	targetID := r.PathValue("id")
	sourceID := r.URL.Query().Get("source")
	if targetID == "" || sourceID == "" {
		jsonError(w, http.StatusBadRequest, "id + ?source= required")
		return
	}
	if targetID == sourceID {
		jsonError(w, http.StatusBadRequest, "source and target must differ")
		return
	}

	source, okSrc, err := a.Store.GetConnection(sourceID)
	if err != nil || !okSrc {
		jsonError(w, http.StatusNotFound, "source connection not found")
		return
	}
	if source.Password == "" {
		jsonError(w, http.StatusBadRequest, "source has no stored password")
		return
	}

	target, okTgt, err := a.Store.GetConnection(targetID)
	if err != nil || !okTgt {
		jsonError(w, http.StatusNotFound, "target connection not found")
		return
	}
	target.Password = source.Password
	if err := a.Store.UpsertConnection(target); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if a.CustomerMgr != nil {
		a.CustomerMgr.Invalidate(targetID)
	}
	after, _, _ := a.Store.GetConnection(targetID)
	jsonOK(w, toPublic(after))
}
