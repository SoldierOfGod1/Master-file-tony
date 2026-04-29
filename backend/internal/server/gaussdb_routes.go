package server

import (
	"errors"
	"net/http"

	"github.com/SoldierOfGod1/command-centre/internal/gaussdb"
)

// RegisterGaussdbRoutes wires the GaussDB-specific endpoints. Today
// that's the schema catalogue used by Cybertron and the dev-tools
// inspector to compose valid SQL against the cluster. The usage-
// summary path itself is dispatched from the existing axiomapi
// route based on USAGE_SOURCE — there's no /gaussdb-summary route
// because that would let a curious operator hit gaussdb-direct even
// when ops has set USAGE_SOURCE=axiom-api as canonical. Keeping the
// summary path single-entry forces consistent operator-facing
// numbers across the tile.
//
// Gated by RAIN_SUPPORT_L2 (matches the rest of the L2 surface) AND
// GAUSSDB_USAGE_ENABLED (defaults true; flip to false as a fast
// cluster-load kill-switch without redeploying a new binary).
func RegisterGaussdbRoutes(mux *http.ServeMux, api *API) {
	mux.HandleFunc("GET /api/v1/customer/usage/gaussdb-catalogue", api.handleGaussdbCatalogue)
}

func (a *API) handleGaussdbCatalogue(w http.ResponseWriter, r *http.Request) {
	if !overrideWritesEnabled() {
		jsonError(w, http.StatusForbidden,
			"gaussdb catalogue disabled — set RAIN_SUPPORT_L2=true to enable")
		return
	}
	if !gaussdbUsageEnabled() {
		jsonError(w, http.StatusServiceUnavailable,
			"gaussdb usage source disabled via GAUSSDB_USAGE_ENABLED=false")
		return
	}
	if a.Gaussdb == nil {
		jsonError(w, http.StatusServiceUnavailable,
			"gaussdb client not initialised — restart the backend after registering a gaussdb-prod connection")
		return
	}
	cat, err := a.Gaussdb.CrawlCatalogue(r.Context())
	if err != nil {
		// Available()-style errors (PlaceholderSQL, NotConfigured) are
		// non-fatal here — the catalogue endpoint can return a partial
		// result with Source="unavailable" + a Note. CrawlCatalogue
		// already does that. Treat anything else as a 502.
		if errors.Is(err, gaussdb.ErrNotConfigured) {
			jsonError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonOK(w, cat)
}
