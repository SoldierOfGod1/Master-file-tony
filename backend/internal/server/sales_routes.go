package server

import (
	"net/http"
)

// RegisterSalesRoutes exposes the rain Sales dashboard snapshot. All
// reads are served from the in-memory snapshot the Poller maintains —
// no database round-trips from the HTTP layer. If the Poller wasn't
// wired, handlers return 503.
func RegisterSalesRoutes(mux *http.ServeMux, api *API) {
	mux.HandleFunc("GET /api/v1/sales/snapshot", api.handleSalesSnapshot)
	mux.HandleFunc("POST /api/v1/sales/refresh", api.handleSalesRefresh)
}

func (a *API) handleSalesSnapshot(w http.ResponseWriter, r *http.Request) {
	if a.SalesPoller == nil {
		jsonError(w, http.StatusServiceUnavailable, "sales poller not configured")
		return
	}
	jsonOK(w, a.SalesPoller.Snapshot())
}

// handleSalesRefresh triggers an on-demand poll. The Poller enforces
// its own "one-in-flight" guard, so repeat clicks from the UI collapse
// to a single execution on the BSS primary — a user tide can't fan
// out into N concurrent heavy queries. Returns the resulting snapshot.
func (a *API) handleSalesRefresh(w http.ResponseWriter, r *http.Request) {
	if a.SalesPoller == nil {
		jsonError(w, http.StatusServiceUnavailable, "sales poller not configured")
		return
	}
	jsonOK(w, a.SalesPoller.Refresh(r.Context()))
}
