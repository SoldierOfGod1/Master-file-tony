package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/SoldierOfGod1/command-centre/internal/chat"
)

// RegisterBudgetsRoutes wires the Phase B3 budget CRUD endpoints.
// Operator-facing: lets you list every user's spend vs cap and
// adjust caps without restarting. Read-only for normal users;
// writes are gated by the same RAIN_SUPPORT_L2 env var that gates
// IMSI override writes (see chat_routes.go for the pattern).
func RegisterBudgetsRoutes(mux *http.ServeMux, api *API) {
	mux.HandleFunc("GET /api/v1/budgets", api.handleListBudgets)
	mux.HandleFunc("GET /api/v1/budgets/{user_id}", api.handleGetBudget)
	mux.HandleFunc("PUT /api/v1/budgets/{user_id}", api.handleSetBudget)
}

// handleListBudgets returns one row per user with a budget plus
// their current week's spend. A row appears for every distinct
// user_id seen in cost_records this week, even if no cap was
// configured (those use DefaultWeeklyCapZAR). Sorted by spend
// descending — the busy users surface first.
func (a *API) handleListBudgets(w http.ResponseWriter, r *http.Request) {
	if a.DB == nil {
		jsonError(w, 503, "db unavailable")
		return
	}

	// Distinct user_ids seen in cost_records this week PLUS any
	// user_ids that have a cap row but no spend yet (so the operator
	// can see every configured user). Anonymous bucket included.
	rows, err := a.DB.Query(`
		WITH wk AS (
		  SELECT user_id FROM cost_records WHERE date >= date('now','weekday 0','-6 days')
		  UNION
		  SELECT user_id FROM user_budgets
		)
		SELECT DISTINCT user_id FROM wk WHERE user_id IS NOT NULL
		ORDER BY user_id`)
	if err != nil {
		jsonError(w, 500, "list users: "+err.Error())
		return
	}
	defer rows.Close()

	var userIDs []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err == nil {
			userIDs = append(userIDs, u)
		}
	}
	// Always include the anonymous bucket so operators can see + cap it.
	if !contains(userIDs, chat.AnonymousUserID) {
		userIDs = append(userIDs, chat.AnonymousUserID)
	}

	gate := chat.NewBudgetGate(a.DB, a.Log)
	out := make([]map[string]any, 0, len(userIDs))
	for _, u := range userIDs {
		st := gate.Check(u)
		out = append(out, map[string]any{
			"user_id":    st.UserID,
			"spent_zar":  st.SpentZAR,
			"cap_zar":    st.CapZAR,
			"pct_spent":  st.PctSpent,
			"verdict":    st.Verdict,
			"week_start": st.WeekStart,
		})
	}
	jsonOK(w, out)
}

// handleGetBudget returns one user's budget state.
func (a *API) handleGetBudget(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("user_id"))
	if id == "" {
		jsonError(w, 400, "user_id required")
		return
	}
	if a.DB == nil {
		jsonError(w, 503, "db unavailable")
		return
	}
	st := chat.NewBudgetGate(a.DB, a.Log).Check(id)
	jsonOK(w, st)
}

// handleSetBudget upserts a per-user weekly cap. Gated on
// RAIN_SUPPORT_L2 — same envelope as IMSI override writes; this
// is operator config, not normal user activity.
func (a *API) handleSetBudget(w http.ResponseWriter, r *http.Request) {
	if !overrideWritesEnabled() {
		jsonError(w, 403, "budget edits disabled — set RAIN_SUPPORT_L2=true to enable")
		return
	}
	if a.DB == nil {
		jsonError(w, 503, "db unavailable")
		return
	}
	id := strings.TrimSpace(r.PathValue("user_id"))
	if id == "" {
		jsonError(w, 400, "user_id required")
		return
	}
	var body struct {
		WeeklyZARCap any `json:"weekly_zar_cap"`
	}
	if err := readJSON(r, &body); err != nil {
		jsonError(w, 400, "invalid json")
		return
	}
	cap, ok := parseFloat(body.WeeklyZARCap)
	if !ok || cap < 0 {
		jsonError(w, 400, "weekly_zar_cap must be a non-negative number")
		return
	}
	if err := chat.SetCap(a.DB, id, cap); err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	// Invalidate the gate cache so the next /chat/agent call sees
	// the new cap immediately rather than waiting out the 30s TTL.
	// Cheap — single map delete.
	chat.NewBudgetGate(a.DB, a.Log).Invalidate(id)
	jsonOK(w, map[string]any{"user_id": id, "weekly_zar_cap": cap})
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// parseFloat handles both numeric and string-encoded inputs from
// JSON since the operator UI may send "50" or 50.0 depending on
// form serialiser.
func parseFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(x), 64); err == nil {
			return f, true
		}
	}
	return 0, false
}
