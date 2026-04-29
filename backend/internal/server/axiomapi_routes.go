package server

import (
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/SoldierOfGod1/command-centre/internal/gaussdb"
)

// RegisterAxiomAPIRoutes wires the rain Axiom HTTP API proxy.
//
// Two endpoints today:
//
//   - /usage/daily   — raw daily-usage rows (legacy shape)
//   - /usage/summary — 4-KPI rollup the Customer 360 tile binds to.
//
// The summary route can dispatch to one of two backing clients per
// USAGE_SOURCE env (default "axiom-api"):
//
//	axiom-api → rain Axiom HTTP API at api.sit.rain.co.za
//	gaussdb   → Huawei GaussDB DWS · PROD via the gaussdb client
//
// There's no UI-side toggle — the operator switches by setting env on
// the backend, which keeps the source decision in ops's hands instead
// of letting two operators read different numbers from the same tile.
//
// Gated by RAIN_SUPPORT_L2 to match the rest of the support-tier
// surface (memory CRUD, patterns aggregate, Cybertron chat tool).
// Without the gate any chat session could pull cdr usage for an
// arbitrary MSISDN.
func RegisterAxiomAPIRoutes(mux *http.ServeMux, api *API) {
	mux.HandleFunc("GET /api/v1/customer/usage/daily", api.handleAxiomDailyUsage)
	mux.HandleFunc("GET /api/v1/customer/usage/summary", api.handleAxiomUsageSummary)
}

func (a *API) handleAxiomDailyUsage(w http.ResponseWriter, r *http.Request) {
	if !overrideWritesEnabled() {
		jsonError(w, http.StatusForbidden,
			"daily usage disabled — set RAIN_SUPPORT_L2=true to enable")
		return
	}
	if a.AxiomAPI == nil {
		jsonError(w, http.StatusServiceUnavailable,
			"axiom-api client not configured (set AXIOM_API_BASE_URL on the backend)")
		return
	}
	msisdn := strings.TrimSpace(r.URL.Query().Get("msisdn"))
	if msisdn == "" {
		jsonError(w, http.StatusBadRequest, "msisdn query param required")
		return
	}
	rows, raw, err := a.AxiomAPI.DailyUsage(r.Context(), msisdn)
	if err != nil {
		// Upstream errors are surfaced verbatim so the operator can
		// see whether it's a 5xx, a rate-limit, or a parse miss.
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	// Always include the raw body so the operator (and Cybertron)
	// can inspect upstream changes the typed shape didn't capture.
	jsonOK(w, map[string]any{
		"msisdn": msisdn,
		"rows":   rows,
		"raw":    string(raw),
	})
}

// handleAxiomUsageSummary computes the 4-KPI rollup for a single
// MSISDN. Backing source is chosen by USAGE_SOURCE env, NOT by query
// param — operators don't get to pick per-call. The selected source
// is echoed back in the response (UsageSummary.Source) so the UI chip
// reflects provenance.
//
// Failure mode for source=gaussdb when the GaussDB queries are still
// the placeholder is 503 with an explicit message, NOT a silent fall-
// back to axiom-api. Silent fallback hides misconfiguration; explicit
// failure forces the operator to fix the queries before trusting
// gaussdb numbers.
func (a *API) handleAxiomUsageSummary(w http.ResponseWriter, r *http.Request) {
	if !overrideWritesEnabled() {
		jsonError(w, http.StatusForbidden,
			"usage summary disabled — set RAIN_SUPPORT_L2=true to enable")
		return
	}
	msisdn := strings.TrimSpace(r.URL.Query().Get("msisdn"))
	if msisdn == "" {
		jsonError(w, http.StatusBadRequest, "msisdn query param required")
		return
	}

	source := preferredUsageSource()
	switch source {
	case "gaussdb":
		if a.Gaussdb == nil {
			jsonError(w, http.StatusServiceUnavailable,
				"gaussdb client not initialised — restart the backend after registering a gaussdb-prod connection")
			return
		}
		ok, availErr := a.Gaussdb.Available()
		if !ok {
			// Surface the precise reason (placeholder SQL vs unconfigured)
			// so the operator knows whether to edit queries.go or
			// register a connection.
			msg := "gaussdb usage source unavailable"
			if availErr != nil {
				msg = availErr.Error()
			}
			jsonError(w, http.StatusServiceUnavailable, msg)
			return
		}
		summary, err := a.Gaussdb.UsageSummary(r.Context(), msisdn)
		if err != nil {
			if errors.Is(err, gaussdb.ErrPlaceholderSQL) || errors.Is(err, gaussdb.ErrNotConfigured) {
				jsonError(w, http.StatusServiceUnavailable, err.Error())
				return
			}
			jsonError(w, http.StatusBadGateway, err.Error())
			return
		}
		jsonOK(w, summary)
		return
	default:
		// axiom-api path — the historical default.
		if a.AxiomAPI == nil {
			jsonError(w, http.StatusServiceUnavailable,
				"axiom-api client not configured (set AXIOM_API_BASE_URL on the backend)")
			return
		}
		summary, _, err := a.AxiomAPI.Summary(r.Context(), msisdn)
		if err != nil {
			jsonError(w, http.StatusBadGateway, err.Error())
			return
		}
		// Defensive: Summary() sets Source = "axiom-api" but if a
		// future code path forgets, this guarantees the chip never
		// goes blank on the UI.
		if summary.Source == "" {
			summary.Source = "axiom-api"
		}
		jsonOK(w, summary)
		return
	}
}

// preferredUsageSource reads USAGE_SOURCE env and normalises. Anything
// unrecognised falls through to axiom-api (the safe default — the
// path that's been in production the longest).
func preferredUsageSource() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("USAGE_SOURCE"))) {
	case "gaussdb", "gauss":
		return "gaussdb"
	case "axiom-api", "axiom", "":
		return "axiom-api"
	default:
		return "axiom-api"
	}
}

// gaussdbUsageEnabled is the additional kill-switch the gaussdb
// catalogue route checks (the summary path checks USAGE_SOURCE
// instead, since selecting that source IS the enable signal). When
// set false, /api/v1/customer/usage/gaussdb-catalogue returns 503.
//
// Default true — once the operator has registered the gaussdb-prod
// connection, the catalogue endpoint is harmless (read-only schema
// dump, RAIN_SUPPORT_L2 already gates it).
func gaussdbUsageEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("GAUSSDB_USAGE_ENABLED")))
	switch v {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

