package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SoldierOfGod1/command-centre/internal/customer"
	"github.com/SoldierOfGod1/command-centre/internal/event"
	"github.com/SoldierOfGod1/command-centre/internal/store"
)

// RegisterCustomerRoutes wires the Customer 360 endpoints.
func RegisterCustomerRoutes(mux *http.ServeMux, api *API) {
	mux.HandleFunc("GET /api/v1/customer", api.handleCustomerLookup)
	mux.HandleFunc("GET /api/v1/customer/{id}", api.handleCustomerByID)
	mux.HandleFunc("GET /api/v1/customer/config", api.handleCustomerConfig)
	// v2 decisioning — record an NBA outcome (accept / dismiss /
	// snooze). Inserts an audit row + updates the recommendation's
	// status so the same action can't fire again within its
	// cooldown window.
	mux.HandleFunc("POST /api/v1/customer/{id}/recommendation/{rec_id}/action", api.handleRecommendationAction)
	// IMSI override — operators paste known IMSIs for a customer
	// when our 3-pivot resolver can't find them. Usage + CDR
	// panels use these directly on subsequent lookups.
	mux.HandleFunc("GET /api/v1/customer/{id}/imsi-override", api.handleGetIMSIOverride)
	mux.HandleFunc("PUT /api/v1/customer/{id}/imsi-override", api.handleSetIMSIOverride)
	// Debug: dump every column of service_accounts + the SIM
	// inventory view for a customer so we can see which column
	// actually carries their IMSI list. Read-only; returns JSON.
	mux.HandleFunc("GET /api/v1/customer/{id}/imsi-debug", api.handleIMSIDebug)
}

// handleIMSIDebug runs three probes and returns everything it
// finds so we can design a 4th pivot that works across customers.
//
//   1. customer.service_accounts rows WHERE subscriber = $1  — full row
//      dump (all columns, `resources` jsonb expanded)
//   2. customer.service_accounts rows WHERE created_by/owned_by = $1
//      — in case the mapping uses a different key
//   3. customer.vw_service_account_state_latest WHERE user_id IN
//      (any user_ids found in step 1/2) — confirms which IMSIs
//      that customer's user_ids actually own
//
// Fires against the primary connection's `customer` database.
// Useful for reverse-engineering the schema on accounts where
// the 3-pivot resolver comes up empty.
func (a *API) handleIMSIDebug(w http.ResponseWriter, r *http.Request) {
	if a.CustomerMgr == nil {
		jsonError(w, http.StatusServiceUnavailable, "customer manager not wired")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		jsonError(w, http.StatusBadRequest, "customer id required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	_, primary, err := a.CustomerMgr.PrimaryPool(ctx)
	if err != nil {
		jsonError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	custPool, _, err := a.CustomerMgr.PoolByIDWithDB(ctx, primary.ID, "customer")
	if err != nil {
		jsonError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	out := map[string]any{
		"customer_id": id,
	}

	// ---- 1. service_accounts rows by subscriber ----
	subscriberRows, err := dumpServiceAccountRows(ctx, custPool,
		`SELECT id::text, name, primary_service, subscriber,
		        created_by::text, owned_by::text, user_id::text,
		        financial_account_id::text, account_type_id::text,
		        service_type, service_status, account_status, status,
		        policy::text, service_policy::text, account_policy::text,
		        resources::text, meta::text, sub_services::text,
		        status_flags::text, service_list::text,
		        inserted_at::text, updated_at::text
		   FROM public.service_accounts
		  WHERE subscriber = $1
		  LIMIT 20`, id)
	if err != nil {
		out["subscriber_error"] = err.Error()
	} else {
		out["subscriber_matches"] = subscriberRows
	}

	// ---- 2. service_accounts rows by owned_by / created_by (uuid cast) ----
	if looksLikeUUIDBasic(id) {
		altRows, err := dumpServiceAccountRows(ctx, custPool,
			`SELECT id::text, name, primary_service, subscriber,
			        created_by::text, owned_by::text, user_id::text,
			        financial_account_id::text, service_type, service_status, status,
			        resources::text, inserted_at::text
			   FROM public.service_accounts
			  WHERE owned_by = $1::uuid OR created_by = $1::uuid
			  LIMIT 20`, id)
		if err != nil {
			out["ownedby_error"] = err.Error()
		} else {
			out["ownedby_matches"] = altRows
		}
	}

	// ---- 3. collect distinct user_ids from whatever matched, then
	//        hit the SIM inventory view with those ----
	userIDs := map[string]struct{}{}
	if rows, ok := out["subscriber_matches"].([]map[string]any); ok {
		for _, r := range rows {
			if u, _ := r["user_id"].(string); u != "" {
				userIDs[u] = struct{}{}
			}
		}
	}
	if rows, ok := out["ownedby_matches"].([]map[string]any); ok {
		for _, r := range rows {
			if u, _ := r["user_id"].(string); u != "" {
				userIDs[u] = struct{}{}
			}
		}
	}
	userList := make([]string, 0, len(userIDs))
	for u := range userIDs {
		userList = append(userList, u)
	}
	out["user_ids_collected"] = userList
	if len(userList) > 0 {
		simRows, err := dumpSimInventory(ctx, custPool, userList)
		if err != nil {
			out["sim_view_error"] = err.Error()
		} else {
			out["sim_view_matches"] = simRows
		}
	}

	// ---- 4. raw column listing so the UI can show what exists ----
	cols, err := dumpColumns(ctx, custPool, "public", "service_accounts")
	if err == nil {
		out["service_accounts_columns"] = cols
	}

	// ---- 5. reverse lookup: given IMSIs (from ?imsi= or from the
	//        override store), find which user_id / account_number
	//        /subscriber they resolve to in the view + in
	//        service_accounts. This is the key: if we can see WHICH
	//        column ties the SIM back to the customer, that's the
	//        pivot we should be using. ----
	imsiParam := strings.TrimSpace(r.URL.Query().Get("imsi"))
	var probeImsis []string
	if imsiParam != "" {
		for _, p := range strings.Split(imsiParam, ",") {
			if s := strings.TrimSpace(p); s != "" {
				probeImsis = append(probeImsis, s)
			}
		}
	}
	// Fall back to the saved override if no ?imsi= supplied.
	if len(probeImsis) == 0 && a.DB != nil {
		var raw string
		_ = a.DB.QueryRow(`SELECT imsis FROM customer_imsi_overrides WHERE customer_id=?`, id).Scan(&raw)
		for _, p := range strings.Split(raw, "|") {
			if s := strings.TrimSpace(p); s != "" {
				probeImsis = append(probeImsis, s)
			}
		}
	}
	if len(probeImsis) > 0 {
		out["probe_imsis"] = probeImsis
		// Reverse-lookup on the SIM view itself.
		viewRows, verr := dumpServiceAccountRows(ctx, custPool, `
			SELECT imsi::text, msisdn::text, user_id::text,
			       COALESCE(account_number,''), sim_name,
			       primary_service, service_status,
			       start_date::text, end_date::text
			  FROM public.vw_service_account_state_latest
			 WHERE imsi::text = ANY($1::text[])`, probeImsis)
		if verr != nil {
			out["imsi_reverse_view_error"] = verr.Error()
		} else {
			out["imsi_reverse_view"] = viewRows
		}
		// Pull the user_ids that came back and dump the matching
		// service_accounts rows by user_id — that's the column that
		// should be our join.
		userIDs2 := map[string]struct{}{}
		if rows, ok := out["imsi_reverse_view"].([]map[string]any); ok {
			for _, r := range rows {
				if u, _ := r["user_id"].(string); u != "" {
					userIDs2[u] = struct{}{}
				}
			}
		}
		ul := make([]string, 0, len(userIDs2))
		for u := range userIDs2 {
			ul = append(ul, u)
		}
		out["imsi_reverse_user_ids"] = ul
		if len(ul) > 0 {
			saRows, serr := dumpServiceAccountRows(ctx, custPool, `
				SELECT id::text, subscriber, user_id::text,
				       created_by::text, owned_by::text,
				       financial_account_id::text,
				       name, primary_service, service_type,
				       service_status, status,
				       resources::text, inserted_at::text
				  FROM public.service_accounts
				 WHERE user_id::text = ANY($1::text[])
				 LIMIT 50`, ul)
			if serr != nil {
				out["imsi_reverse_sa_error"] = serr.Error()
			} else {
				out["imsi_reverse_sa"] = saRows
			}
		}

		// ---- 6. The view may not have our SIMs, but
		// `service_accounts.resources` (jsonb) often carries the
		// IMSI embedded. Search for any row whose `resources`
		// text contains one of the probe IMSIs. Slow (full scan)
		// but diagnostic only — not run in prod paths.
		imsiFilters := make([]string, 0, len(probeImsis))
		for _, v := range probeImsis {
			imsiFilters = append(imsiFilters, "%"+v+"%")
		}
		saByResources, serr := dumpServiceAccountRows(ctx, custPool, `
			SELECT id::text, subscriber, user_id::text,
			       created_by::text, owned_by::text,
			       financial_account_id::text, name,
			       primary_service, service_type, service_status,
			       resources::text
			  FROM public.service_accounts
			 WHERE resources::text ILIKE ANY($1::text[])
			 LIMIT 20`, imsiFilters)
		if serr != nil {
			out["sa_by_resources_error"] = serr.Error()
		} else {
			out["sa_by_resources"] = saByResources
		}
	}

	// ---- 6b. Product → jt_prod_rs_ref → resource_ref debug dump.
	// Full trace of the join so we can see where the path loses
	// data for customers whose SIMs aren't in the SIM view. ----
	// Need to fetch the customer's billing_accounts first via a
	// quick lookup so we can pass billing_account.id values in.
	{
		// Reuse LookupProd's view by calling a lightweight SQL to
		// get the billing_account ids for this individual. We keep
		// it minimal to avoid slowing the debug endpoint.
		acctPool, _, err := a.CustomerMgr.PoolByIDWithDB(ctx, primary.ID, "account")
		if err == nil {
			rows, e2 := acctPool.Query(ctx, `
				SELECT id FROM account.billing_account
				 WHERE related_party_id = $1
				 LIMIT 20`, id)
			if e2 == nil {
				baIDs := []string{}
				for rows.Next() {
					var bid string
					if rows.Scan(&bid) == nil && bid != "" {
						baIDs = append(baIDs, bid)
					}
				}
				rows.Close()
				out["billing_account_ids"] = baIDs
				if len(baIDs) > 0 {
					out["product_path_debug"] = customer.FetchIMSIsViaProductPathDebug(ctx, a.CustomerMgr, primary.ID, baIDs)
				}
			} else {
				out["billing_accounts_error"] = e2.Error()
			}
		}
	}

	// ---- 7. customer.customer table — does it have a row for
	// this individual? Might give us the "customer id" used by
	// service_accounts. ----
	customerRows, cerr := dumpServiceAccountRows(ctx, custPool, `
		SELECT id::text, name, login_name, individual_id,
		       created_by::text, owned_by::text, inserted_at::text
		  FROM public.tmp_prep_bill
		 LIMIT 1`)
	// That was a wildcard probe; what we actually want is the real
	// customer table.
	_ = customerRows; _ = cerr
	custByIndiv, cerr2 := dumpServiceAccountRows(ctx, custPool, `
		SELECT id::text, login_name,
		       inserted_at::text, updated_at::text
		  FROM customer.customer
		 WHERE id = $1 OR login_name = $1
		 LIMIT 5`, id)
	if cerr2 != nil {
		out["customer_table_error"] = cerr2.Error()
	} else {
		out["customer_table_matches"] = custByIndiv
	}

	jsonOK(w, out)
}

// dumpServiceAccountRows scans any columns the SQL defines and
// returns them as a slice of maps. Works because we cast every
// column to text in the SQL — no pgx type plumbing needed for a
// read-only diagnostic endpoint.
func dumpServiceAccountRows(ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) ([]map[string]any, error) {
	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	fieldNames := make([]string, 0, len(rows.FieldDescriptions()))
	for _, f := range rows.FieldDescriptions() {
		fieldNames = append(fieldNames, string(f.Name))
	}
	out := []map[string]any{}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			continue
		}
		m := map[string]any{}
		for i, name := range fieldNames {
			if i < len(vals) {
				m[name] = vals[i]
			}
		}
		out = append(out, m)
	}
	return out, nil
}

func dumpSimInventory(ctx context.Context, pool *pgxpool.Pool, userIDs []string) ([]map[string]any, error) {
	rows, err := pool.Query(ctx, `
		SELECT user_id::text,
		       COALESCE(account_number, ''),
		       imsi::text, msisdn::text, imei::text, iccid::text,
		       COALESCE(sim_name, ''),
		       COALESCE(primary_service, ''),
		       service_status, start_date::text, end_date::text
		  FROM public.vw_service_account_state_latest
		 WHERE user_id::text = ANY($1::text[])
		 LIMIT 50`, userIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	fieldNames := make([]string, 0, len(rows.FieldDescriptions()))
	for _, f := range rows.FieldDescriptions() {
		fieldNames = append(fieldNames, string(f.Name))
	}
	out := []map[string]any{}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			continue
		}
		m := map[string]any{}
		for i, name := range fieldNames {
			if i < len(vals) {
				m[name] = vals[i]
			}
		}
		out = append(out, m)
	}
	return out, nil
}

func dumpColumns(ctx context.Context, pool *pgxpool.Pool, schema, table string) ([]map[string]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT column_name, data_type
		  FROM information_schema.columns
		 WHERE table_schema = $1 AND table_name = $2
		 ORDER BY ordinal_position`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]string
	for rows.Next() {
		var c, t string
		if err := rows.Scan(&c, &t); err == nil {
			out = append(out, map[string]string{"name": c, "type": t})
		}
	}
	return out, nil
}

// looksLikeUUIDBasic is a lighter version of looksLikeUUID in the
// customer package. Duplicated here to avoid exporting that helper
// for a single caller.
func looksLikeUUIDBasic(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' {
				return false
			}
			continue
		}
		ok := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
		if !ok {
			return false
		}
	}
	return true
}

// handleGetIMSIOverride returns the saved IMSI list for a customer.
// Response: { "imsis": ["655380004807362", ...] }  — strings (IMSIs
// are 15-digit numbers; returning JSON numbers risks JS precision
// loss).
func (a *API) handleGetIMSIOverride(w http.ResponseWriter, r *http.Request) {
	if a.DB == nil {
		jsonError(w, http.StatusServiceUnavailable, "db unavailable")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		jsonError(w, http.StatusBadRequest, "customer id required")
		return
	}
	var raw string
	_ = a.DB.QueryRowContext(r.Context(),
		`SELECT imsis FROM customer_imsi_overrides WHERE customer_id = ?`, id,
	).Scan(&raw)
	parts := []string{}
	for _, p := range strings.Split(raw, "|") {
		p = strings.TrimSpace(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	jsonOK(w, map[string]any{"imsis": parts})
}

// handleSetIMSIOverride writes the pipe-separated list. Empty list
// clears the row.
//
// Phase 6 of docs/axiom/sim-diagnostics-plan.md: env-gated.
// `RAIN_SUPPORT_L2=true` must be set in the backend environment to
// permit override writes. The plan called for a `support-l2` role
// gate, but the backend has no auth layer (single-user localhost
// tool — see CEO review's role-middleware audit). Env flag is the
// right shape for that constraint: the operator who launches the
// server makes the explicit choice to allow override writes for
// the duration of that session.
func (a *API) handleSetIMSIOverride(w http.ResponseWriter, r *http.Request) {
	if !overrideWritesEnabled() {
		jsonError(w, http.StatusForbidden,
			"imsi override writes disabled — set RAIN_SUPPORT_L2=true to enable")
		return
	}
	if a.DB == nil {
		jsonError(w, http.StatusServiceUnavailable, "db unavailable")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		jsonError(w, http.StatusBadRequest, "customer id required")
		return
	}
	var body struct {
		IMSIs []string `json:"imsis"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	// Light validation — IMSIs are 14-15 digit numbers. Drop
	// anything that isn't.
	clean := make([]string, 0, len(body.IMSIs))
	for _, raw := range body.IMSIs {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		// Trim any spaces/punctuation just in case an operator
		// pasted "imsi 655380004807362, 655380..."
		digits := make([]rune, 0, len(s))
		for _, c := range s {
			if c >= '0' && c <= '9' {
				digits = append(digits, c)
			}
		}
		cleaned := string(digits)
		if len(cleaned) < 6 || len(cleaned) > 20 {
			continue
		}
		clean = append(clean, cleaned)
	}
	joined := strings.Join(clean, "|")
	if _, err := a.DB.ExecContext(r.Context(),
		`INSERT INTO customer_imsi_overrides (customer_id, imsis, updated_at)
		 VALUES (?, ?, datetime('now'))
		 ON CONFLICT(customer_id) DO UPDATE SET
		   imsis = excluded.imsis, updated_at = datetime('now')`,
		id, joined,
	); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]any{"imsis": clean, "count": len(clean)})
}

// overrideWritesEnabled reports whether RAIN_SUPPORT_L2 is set to a
// truthy value. Read on every request (no caching) so the operator
// can flip the flag without restarting if needed via a wrapper
// process. Phase 6 gate per docs/axiom/sim-diagnostics-plan.md.
func overrideWritesEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("RAIN_SUPPORT_L2")))
	switch v {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// handleRecommendationAction captures the agent's decision on one
// recommendation. Body is JSON:
//   { "action": "accept" | "dismiss" | "snooze",
//     "channel": "sms" | "email" | "call" | "agent",
//     "agent_id": "optional",
//     "note": "optional free text" }
// The rec's status in customer_recommendations flips to match the
// action so the cooldown query (7-day per kind) excludes it.
func (a *API) handleRecommendationAction(w http.ResponseWriter, r *http.Request) {
	if a.DB == nil {
		jsonError(w, http.StatusServiceUnavailable, "db unavailable")
		return
	}
	custID := strings.TrimSpace(r.PathValue("id"))
	recID := strings.TrimSpace(r.PathValue("rec_id"))
	if custID == "" || recID == "" {
		jsonError(w, http.StatusBadRequest, "customer id and rec_id required")
		return
	}
	var body struct {
		Action  string `json:"action"`
		Channel string `json:"channel"`
		AgentID string `json:"agent_id"`
		Note    string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	action := strings.ToLower(strings.TrimSpace(body.Action))
	switch action {
	case "accept", "accepted":
		action = "accepted"
	case "dismiss", "dismissed":
		action = "dismissed"
	case "snooze", "snoozed":
		action = "snoozed"
	default:
		jsonError(w, http.StatusBadRequest, "action must be accept | dismiss | snooze")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	// Flip the rec's status.
	if _, err := a.DB.ExecContext(r.Context(),
		`UPDATE customer_recommendations SET status = ? WHERE id = ? AND customer_id = ?`,
		action, recID, custID,
	); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Log the raw audit row — this is the training-data seed for a
	// future uplift model. channel/agent_id/note are all optional.
	if _, err := a.DB.ExecContext(r.Context(),
		`INSERT INTO customer_recommendation_actions
		   (recommendation_id, customer_id, action, channel, agent_id, note, at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		recID, custID, action,
		strings.TrimSpace(body.Channel),
		strings.TrimSpace(body.AgentID),
		strings.TrimSpace(body.Note),
		now,
	); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]any{"ok": true, "action": action, "at": now})
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

	// Multi-DB resolver when a manager is available; falls back to the
	// legacy single-pool path for any caller that isn't using the
	// prod cluster (e.g. the merged-DB SIT install).
	view, err := customer.LookupProd(r.Context(), a.CustomerMgr, a.Log, mode, value)
	// Activity feed: emit one row per successful lookup so the
	// Dashboard card ticks up. Only log successful lookups with a
	// resolved identity — candidate pickers and 404s aren't
	// "activity" in the sense the agent cares about.
	if err == nil && a.Feed != nil && view != nil && view.Identity.ID != "" {
		name := view.Identity.FullName
		if name == "" {
			name = view.Identity.ID
		}
		a.Feed.Publish(r.Context(), event.FeedKindCustomer, "",
			"Customer lookup: "+name+" ("+mode+"="+value+")")
	}
	if err != nil {
		var nf *customer.NotFoundError
		if errors.As(err, &nf) {
			jsonError(w, http.StatusNotFound, nf.Error())
			return
		}
		// Fall back to legacy single-pool lookup so this endpoint still
		// works against the SIT (merged-DB) cluster.
		pool, _, perr := a.selectPool(r)
		if perr == nil {
			if legacy, lerr := customer.Lookup(r.Context(), pool, a.Log, mode, value); lerr == nil {
				jsonOK(w, legacy)
				return
			}
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
	view, err := customer.LookupProd(r.Context(), a.CustomerMgr, a.Log, "id", id)
	if err != nil {
		// Fall back to single-pool legacy lookup.
		view, err = customer.Lookup(r.Context(), pool, a.Log, "id", id)
	}
	_ = pool
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
