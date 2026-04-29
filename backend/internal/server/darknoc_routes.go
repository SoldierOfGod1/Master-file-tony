package server

import (
	"net/http"
	"os"
	"strings"

	"github.com/SoldierOfGod1/command-centre/internal/darknoc"
)

// RegisterDarkNocRoutes wires the read-only Dark NOC HUD endpoints.
// All endpoints are gated behind DARK_NOC_ENABLED so a SIT install
// never accidentally serves a half-configured tab.
func RegisterDarkNocRoutes(mux *http.ServeMux, api *API) {
	mux.HandleFunc("GET /api/v1/darknoc/config", api.handleDarkNocConfig)
	mux.HandleFunc("GET /api/v1/darknoc/overview", api.handleDarkNocOverview)
	mux.HandleFunc("GET /api/v1/darknoc/faults", api.handleDarkNocFaults)
	mux.HandleFunc("GET /api/v1/darknoc/registry", api.handleDarkNocRegistry)
	mux.HandleFunc("GET /api/v1/darknoc/catalogue", api.handleDarkNocCatalogue)
}

// darkNocEnabled reads the env var on every request so the operator
// can toggle without a restart via a wrapper. Same shape as
// overrideWritesEnabled() (RAIN_SUPPORT_L2) elsewhere.
func darkNocEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("DARK_NOC_ENABLED")))
	switch v {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// handleDarkNocConfig is intentionally unauthed and not env-gated:
// the frontend uses it on every page load to decide whether to render
// the tab content or the "set DARK_NOC_ENABLED" banner. Returning a
// JSON `{enabled: false}` is friendlier than a 503.
func (a *API) handleDarkNocConfig(w http.ResponseWriter, r *http.Request) {
	dash := ""
	if a.DarkNocGrafana != nil {
		dash = a.DarkNocGrafana.DashboardUID()
	}
	jsonOK(w, map[string]any{
		"enabled":         darkNocEnabled(),
		"grafana_dashboard_uid": dash,
	})
}

func (a *API) handleDarkNocOverview(w http.ResponseWriter, r *http.Request) {
	if !darkNocEnabled() {
		jsonError(w, http.StatusServiceUnavailable,
			"Dark NOC disabled — set DARK_NOC_ENABLED=true to enable")
		return
	}
	if a.DarkNoc == nil {
		jsonError(w, http.StatusServiceUnavailable, "darknoc connector not initialised")
		return
	}
	v, err := a.DarkNoc.Overview(r.Context())
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonOK(w, v)
}

func (a *API) handleDarkNocFaults(w http.ResponseWriter, r *http.Request) {
	if !darkNocEnabled() {
		jsonError(w, http.StatusServiceUnavailable,
			"Dark NOC disabled — set DARK_NOC_ENABLED=true to enable")
		return
	}
	if a.DarkNoc == nil {
		jsonError(w, http.StatusServiceUnavailable, "darknoc connector not initialised")
		return
	}
	rows, err := a.DarkNoc.Faults(r.Context())
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	if rows == nil {
		rows = []darknoc.Fault{}
	}
	jsonOK(w, rows)
}

func (a *API) handleDarkNocRegistry(w http.ResponseWriter, r *http.Request) {
	// Registry is fine to return even when DARK_NOC_ENABLED is off —
	// it's a static reference list, no live data. Saves the operator
	// a confused "blank tab" moment.
	if a.DarkNoc == nil {
		jsonOK(w, []darknoc.RegistryAgent{})
		return
	}
	rows := a.DarkNoc.Registry()
	if rows == nil {
		rows = []darknoc.RegistryAgent{}
	}
	jsonOK(w, rows)
}

// handleDarkNocCatalogue dumps the live ClickHouse schema. Cybertron
// reads it before composing SQL so it stops hallucinating table
// names. Server-side crawl uses the saved connection — the operator
// already authenticated when they saved the password, so we don't
// need a separate Python script asking for creds.
func (a *API) handleDarkNocCatalogue(w http.ResponseWriter, r *http.Request) {
	if !darkNocEnabled() {
		jsonError(w, http.StatusServiceUnavailable,
			"Dark NOC disabled — set DARK_NOC_ENABLED=true to enable")
		return
	}
	if a.DarkNoc == nil {
		jsonError(w, http.StatusServiceUnavailable, "darknoc connector not initialised")
		return
	}
	cat, err := a.DarkNoc.CrawlCatalogue(r.Context())
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonOK(w, cat)
}
