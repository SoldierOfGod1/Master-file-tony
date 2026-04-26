package server

import (
	"net/http"

	"github.com/SoldierOfGod1/command-centre/internal/chat"
)

// RegisterPatternsRoutes wires the cross-user aggregate ops
// telemetry endpoint. Originally deferred for InfoSec review;
// the chat.AggregatePatterns implementation has two privacy
// guards baked in (counts only, k-anonymity ≥3) so the route
// is now safe to expose, gated to RAIN_SUPPORT_L2 the same way
// memory CRUD is.
//
// No mutating verbs — this is purely a read endpoint. The gate
// is on read because the aggregate, while privacy-safe, is
// fleet-wide ops data and not appropriate for arbitrary clients.
func RegisterPatternsRoutes(mux *http.ServeMux, api *API) {
	mux.HandleFunc("GET /api/v1/patterns/aggregate", api.handlePatternsAggregate)
}

func (a *API) handlePatternsAggregate(w http.ResponseWriter, r *http.Request) {
	if !overrideWritesEnabled() {
		jsonError(w, 403,
			"patterns aggregate disabled — set RAIN_SUPPORT_L2=true to enable")
		return
	}
	if a.DB == nil {
		jsonError(w, 503, "db unavailable")
		return
	}
	jsonOK(w, chat.AggregatePatterns(a.DB))
}
