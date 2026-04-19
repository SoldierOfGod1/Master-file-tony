package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SoldierOfGod1/command-centre/internal/customer"
	"github.com/SoldierOfGod1/command-centre/internal/store"
)

// RegisterCustomerRoutes wires the Customer 360 endpoints.
func RegisterCustomerRoutes(mux *http.ServeMux, api *API) {
	mux.HandleFunc("GET /api/v1/customer", api.handleCustomerLookup)
	mux.HandleFunc("GET /api/v1/customer/{id}", api.handleCustomerByID)
	mux.HandleFunc("GET /api/v1/customer/config", api.handleCustomerConfig)
}

// handleCustomerConfig summarises whether the user has at least one
// connection with a filled password so the frontend knows whether to
// render the lookup form or the "configure Axiom" empty state.
func (a *API) handleCustomerConfig(w http.ResponseWriter, r *http.Request) {
	conns, _ := a.Store.ListConnections()
	configured := false
	var primaryID string
	for _, c := range conns {
		if c.Driver == "postgres" && c.Filled() {
			configured = true
		}
		if c.IsPrimary {
			primaryID = c.ID
		}
	}
	jsonOK(w, map[string]any{
		"configured":        configured,
		"primary":           primaryID,
		"connections_count": len(conns),
	})
}

// selectPool picks which pgx pool to use for a lookup. Optional ?connection=
// query param overrides the primary; falls back to the primary connection.
func (a *API) selectPool(r *http.Request) (*pgxpool.Pool, store.Connection, error) {
	if a.CustomerMgr == nil {
		return nil, store.Connection{}, errors.New("customer manager not initialised")
	}
	if id := strings.TrimSpace(r.URL.Query().Get("connection")); id != "" {
		return a.CustomerMgr.PoolByID(r.Context(), id)
	}
	return a.CustomerMgr.PrimaryPool(r.Context())
}

// handleCustomerLookup accepts ?phone=... OR ?email=... and returns the
// full Customer360 bundle.
func (a *API) handleCustomerLookup(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	phone := strings.TrimSpace(q.Get("phone"))
	email := strings.TrimSpace(q.Get("email"))
	if phone == "" && email == "" {
		jsonError(w, http.StatusBadRequest, "supply either ?phone= or ?email=")
		return
	}
	mode, value := "email", email
	if phone != "" {
		mode, value = "phone", phone
	}

	pool, _, err := a.selectPool(r)
	if err != nil {
		mapDBError(w, err)
		return
	}

	view, err := customer.Lookup(r.Context(), pool, a.Log, mode, value)
	if err != nil {
		var nf *customer.NotFoundError
		if errors.As(err, &nf) {
			jsonError(w, http.StatusNotFound, nf.Error())
			return
		}
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonOK(w, view)
}

// handleCustomerByID is the deep-link entry point used by neighbours.
func (a *API) handleCustomerByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonError(w, http.StatusBadRequest, "id required")
		return
	}
	pool, _, err := a.selectPool(r)
	if err != nil {
		mapDBError(w, err)
		return
	}
	view, err := customer.Lookup(r.Context(), pool, a.Log, "id", id)
	if err != nil {
		var nf *customer.NotFoundError
		if errors.As(err, &nf) {
			jsonError(w, http.StatusNotFound, nf.Error())
			return
		}
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonOK(w, view)
}

// mapDBError converts a pool-selection error into an HTTP response. Keeps
// the lookup handlers tidy.
func mapDBError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, customer.ErrNotConfigured):
		jsonError(w, http.StatusServiceUnavailable, "No usable database connection — configure one in Settings.")
	case errors.Is(err, customer.ErrClickHouseUnsupported):
		jsonError(w, http.StatusBadRequest, err.Error())
	default:
		jsonError(w, http.StatusBadGateway, err.Error())
	}
}
