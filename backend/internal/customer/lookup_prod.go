// Package customer — multi-DB Customer 360 resolver for the Axiom PROD
// cluster. Replaces the single-pool lookup with a fan-out across the
// per-domain databases confirmed in docs/axiom/axiom-prod-catalogue.json:
//
//   party        → individual + contact_medium
//   account      → billing_account + account_balance + promise_to_pay
//   payment      → payment ledger
//   customer     → invoices
//   service      → service_order
//   snowflake    → ticket (middleware-owned)
//   communication→ sms_history (recent outreach)
//
// Every sub-fetch has its own context timeout so one slow DB cannot
// drag the whole page. Partial failures become `DataSourceStatus` rows
// in the response — the UI then shows an honest "X not available" chip
// instead of pretending the data was empty.

package customer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	primaryConnID = "axiom-prod" // fallback — overridden via manager primary
	subQueryBudget = 8 * time.Second
)

// LookupProd is the multi-DB resolver. Supply a *Manager; it routes per
// DB from that. Looks up by phone | email | id.
func LookupProd(ctx context.Context, mgr *Manager, log *slog.Logger, mode, value string) (*Customer360, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("empty lookup value")
	}
	if mgr == nil {
		return nil, errors.New("customer manager not configured")
	}

	// Resolve the primary connection so we know which connection id to
	// use when switching databases for downstream calls.
	_, primary, err := mgr.PrimaryPool(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve primary: %w", err)
	}
	connID := primary.ID

	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	// ── Identity resolution (party DB) ────────────────────────
	partyPool, _, err := mgr.PoolByIDWithDB(ctx, connID, "party")
	if err != nil {
		return nil, fmt.Errorf("party pool: %w", err)
	}

	// When a phone/email matches more than one party.individual
	// (rain family plans share phones across multiple individuals),
	// return a candidates list so the UI can render a picker rather
	// than silently auto-selecting whichever row was most recently
	// updated. Callers can then re-submit with mode=id to resolve
	// to the chosen individual. We skip the ambiguity check when the
	// caller already specified mode=id.
	if strings.EqualFold(mode, "phone") || strings.EqualFold(mode, "email") {
		cands, err := resolveCandidates(ctx, mgr, connID, partyPool, log, mode, value)
		if err != nil {
			log.Warn("resolve candidates", "error", err, "mode", mode, "value", value)
		}
		if len(cands) > 1 {
			return &Customer360{
				LookedUpBy: fmt.Sprintf("%s=%s", mode, value),
				LookedUpAt: time.Now().UTC(),
				Candidates: cands,
			}, nil
		}
		// Exactly one candidate: promote it so downstream fetchers
		// resolve by id. This is what lets the MSISDN-view backfill
		// rescue lookups where the individual has no phone row in
		// party.contact_medium (SIM-only family members).
		if len(cands) == 1 {
			log.Info("customer360 single candidate promoted",
				"from_mode", mode, "value", value, "id", cands[0].ID)
			mode, value = "id", cands[0].ID
		}
	}

	individualID, identity, contacts, err := resolveIdentityProd(ctx, partyPool, mode, value)
	if err != nil {
		return nil, err
	}
	if individualID == "" {
		return nil, &NotFoundError{Query: value}
	}

	resp := &Customer360{
		Identity:   identity,
		Contacts:   contacts,
		LookedUpBy: fmt.Sprintf("%s=%s", mode, value),
		LookedUpAt: time.Now().UTC(),
		DeepLinks: DeepLinks{
			Station: "https://www.the101.info/customer/" + individualID,
			Athena:  "https://assisted-sales.athena.rain.co.za/customer/" + individualID,
		},
	}

	// ── Fan out per-DB queries in parallel ─────────────────────
	// We use a plain sync.WaitGroup + mutex (not errgroup) because we
	// want partial success — every error is captured as a DataSourceStatus
	// row rather than short-circuiting the whole response.
	type fetch struct {
		name, database string
		run            func(c context.Context) (rows int, err error)
	}

	// Stage 1: load billing accounts FIRST (synchronously) because
	// invoices + balance-by-account depend on the financial_account_id
	// and billing_account_id pivots. This is a 5-second budget; if it
	// fails we still return the rest.
	stage1Ctx, s1Cancel := context.WithTimeout(ctx, 5*time.Second)
	{
		pool, _, err := mgr.PoolByIDWithDB(stage1Ctx, connID, "account")
		if err == nil {
			start := time.Now()
			bas, bals, e2 := fetchAccountBits(stage1Ctx, pool, individualID)
			lat := time.Since(start).Milliseconds()
			status := DataSourceStatus{Name: "billing_accounts", Database: "account", LatencyMS: lat, Rows: len(bas) + len(bals)}
			if e2 != nil {
				status.State = "error"
				status.Error = e2.Error()
			} else if len(bas) == 0 && len(bals) == 0 {
				status.State = "empty"
			} else {
				status.State = "ok"
				resp.BillingAccounts = bas
				resp.Balances = bals
			}
			resp.DataSources = append(resp.DataSources, status)
		}
	}
	s1Cancel()

	// Load IMSI overrides (if any) once per lookup so every
	// resolveIMSIs call below short-circuits straight to the
	// operator-configured list.
	if db := mgr.LocalDB(); db != nil && individualID != "" {
		resp.IMSIOverrides = loadIMSIOverrides(ctx, db, individualID)
	}

	fetches := []fetch{
		{"promises_to_pay", "account", func(c context.Context) (int, error) {
			pool, _, err := mgr.PoolByIDWithDB(c, connID, "account")
			if err != nil { return 0, err }
			pros, err := fetchPromises(c, pool, individualID)
			if err != nil { return 0, err }
			resp.Promises = pros
			return len(pros), nil
		}},
		{"payments", "payment", func(c context.Context) (int, error) {
			pool, _, err := mgr.PoolByIDWithDB(c, connID, "payment")
			if err != nil { return 0, err }
			pays, err := fetchPaymentsProd(c, pool, individualID)
			if err != nil { return 0, err }
			resp.Payments = pays
			return len(pays), nil
		}},
		{"invoices", "customer", func(c context.Context) (int, error) {
			// Billing accounts already loaded (stage 1). Pivot on their
			// financial_account_id UUIDs.
			pool, _, err := mgr.PoolByIDWithDB(c, connID, "customer")
			if err != nil { return 0, err }
			var finIDs []string
			for _, b := range resp.BillingAccounts {
				if b.FinancialAccountID != "" { finIDs = append(finIDs, b.FinancialAccountID) }
			}
			if len(finIDs) == 0 {
				return 0, nil
			}
			inv, err := fetchInvoicesByFinAccount(c, pool, finIDs)
			if err != nil { return 0, err }
			resp.Invoices = inv
			return len(inv), nil
		}},
		{"services", "service", func(c context.Context) (int, error) {
			pool, _, err := mgr.PoolByIDWithDB(c, connID, "service")
			if err != nil { return 0, err }
			subs, err := fetchServices(c, pool, individualID)
			if err != nil { return 0, err }
			resp.Subscriptions = subs
			return len(subs), nil
		}},
		{"tickets", "snowflake", func(c context.Context) (int, error) {
			pool, _, err := mgr.PoolByIDWithDB(c, connID, "snowflake")
			if err != nil { return 0, err }
			tk, err := fetchTicketsProd(c, pool, individualID)
			if err != nil { return 0, err }
			resp.Tickets = tk
			return len(tk), nil
		}},
		{"products", "product", func(c context.Context) (int, error) {
			// product.product.product is keyed by billing_account_id
			// (the 8-digit account number — same value as the billing
			// account's `name` field). Stage-1 loaded billing accounts,
			// so we can pivot through them now.
			accountNums := make([]string, 0, len(resp.BillingAccounts))
			finAccts := make([]string, 0, len(resp.BillingAccounts))
			for _, b := range resp.BillingAccounts {
				if b.Name != "" { accountNums = append(accountNums, b.Name) }
				if b.FinancialAccountID != "" { finAccts = append(finAccts, b.FinancialAccountID) }
			}
			if len(accountNums) == 0 {
				return 0, nil
			}
			pool, _, err := mgr.PoolByIDWithDB(c, connID, "product")
			if err != nil { return 0, err }
			// Best-effort: the service_category enrichment adds
			// "Rain Home / Work / Mobile / Loop" labels + a product
			// image URL. If the service cluster isn't in the registry
			// we still return the product rows, just without the
			// friendly label.
			servicePool, _, _ := mgr.PoolByIDWithDB(c, connID, "service")
			prods, err := fetchProductsProd(c, pool, servicePool, finAccts, accountNums)
			if err != nil { return 0, err }
			resp.Products = prods
			return len(prods), nil
		}},
		{"cdr_usage", "athena", func(c context.Context) (int, error) {
			// Actual GPRS data usage lives in Athena's
			// iv_usage_cdr_detail, NOT in ralf.resource_policy
			// (which is policy/quota metadata). Silent skip when
			// Athena isn't configured.
			ath := mgr.AthenaUsage()
			if ath == nil || !ath.Available() {
				return 0, nil
			}
			custPool, _, err := mgr.PoolByIDWithDB(c, connID, "customer")
			if err != nil { return 0, err }
			srcs := resolveIMSIs(c, mgr, connID, custPool, resp, log, individualID, "cdr_usage")
			imsis := imsiInts(srcs)
			if len(imsis) == 0 { return 0, nil }
			rows, err := ath.UsageSince(c, imsis)
			if err != nil {
				log.Warn("cdr_usage athena query", "error", err, "imsis", len(imsis))
				return 0, err
			}
			log.Info("cdr_usage athena result", "imsis", len(imsis), "rows", len(rows))
			resp.CDRUsage = rows
			return len(rows), nil
		}},
		{"usage", "resource", func(c context.Context) (int, error) {
			// ralf.resource_policy is keyed by IMSI, not MSISDN.
			// Resolve IMSIs via the shared helper (billing-account
			// pivot first, MSISDN fallback second) then look up the
			// latest policy/quota row per IMSI.
			custPool, _, err := mgr.PoolByIDWithDB(c, connID, "customer")
			if err != nil { return 0, err }
			srcs := resolveIMSIs(c, mgr, connID, custPool, resp, log, individualID, "usage")
			imsis := imsiInts(srcs)
			if len(imsis) == 0 { return 0, nil }
			pool, _, err := mgr.PoolByIDWithDB(c, connID, "resource")
			if err != nil { return 0, err }
			usages, err := fetchUsageProd(c, pool, imsis)
			if err != nil { return 0, err }
			resp.Usage = usages
			return len(usages), nil
		}},
		{"notifications", "communication", func(c context.Context) (int, error) {
			pool, _, err := mgr.PoolByIDWithDB(c, connID, "communication")
			if err != nil { return 0, err }
			phones := phonesOf(contacts)
			if len(phones) == 0 {
				return 0, nil
			}
			evs, err := fetchRecentSMS(c, pool, phones, individualID)
			if err != nil { return 0, err }
			resp.RecentNotifications = evs
			return len(evs), nil
		}},
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	wg.Add(len(fetches))
	for _, f := range fetches {
		f := f
		go func() {
			defer wg.Done()
			subCtx, sc := context.WithTimeout(ctx, subQueryBudget)
			defer sc()
			start := time.Now()
			rows, err := f.run(subCtx)
			lat := time.Since(start).Milliseconds()
			status := DataSourceStatus{
				Name: f.name, Database: f.database, LatencyMS: lat, Rows: rows,
			}
			switch {
			case err != nil:
				status.State = "error"
				status.Error = err.Error()
				log.Warn("customer360 fetch",
					"source", f.name, "db", f.database, "error", err)
			case rows == 0:
				status.State = "empty"
			default:
				status.State = "ok"
			}
			mu.Lock()
			resp.DataSources = append(resp.DataSources, status)
			mu.Unlock()
		}()
	}
	wg.Wait()

	// Fail-closed POPIA audit gate (eng-review 2A). If any
	// resolveIMSIs call set the audit-failed flag during fan-out,
	// refuse to return Customer360 — the caller will surface 500
	// to the HTTP layer. Better to deny the lookup entirely than
	// to serve customer data without an audit trail.
	if resp.AuditFailed() {
		return nil, fmt.Errorf("imsi audit write failed; refusing to serialise customer 360 (POPIA fail-closed)")
	}

	// Ensure every slice is non-nil so the JSON response encodes as `[]`
	// rather than `null` — keeps the frontend's `.length` / `.map` calls
	// safe without each panel having to nil-guard.
	if resp.Contacts == nil            { resp.Contacts = []ContactMedium{} }
	if resp.Payments == nil            { resp.Payments = []Payment{} }
	if resp.Subscriptions == nil       { resp.Subscriptions = []Subscription{} }
	if resp.Tickets == nil             { resp.Tickets = []Ticket{} }
	if resp.Chargebacks == nil         { resp.Chargebacks = []Chargeback{} }
	if resp.Neighbours == nil          { resp.Neighbours = []Neighbour{} }
	if resp.BillingAccounts == nil     { resp.BillingAccounts = []BillingAccount{} }
	if resp.Balances == nil            { resp.Balances = []AccountBalance{} }
	if resp.Invoices == nil            { resp.Invoices = []Invoice{} }
	if resp.Promises == nil            { resp.Promises = []PromiseToPay{} }
	if resp.RecentNotifications == nil { resp.RecentNotifications = []NotificationEvent{} }
	if resp.Products == nil            { resp.Products = []Product{} }
	if resp.Usage == nil               { resp.Usage = []UsageSnapshot{} }

	// Derived fields — LTV, risk, timeline, heatmap, age.
	resp.LifetimeValue = sumSuccessfulPayments(resp.Payments)
	resp.AccountAge = ageOf(resp.Identity.CreatedAt)
	resp.DaysSinceLastPayment = daysSinceLastPaymentProd(resp.Payments)
	resp.PaymentHeatmap = buildHeatmap(resp.Payments)
	resp.RiskScore = computeRiskScoreProd(resp)
	resp.Timeline = buildTimelineProd(resp)

	// ---- v2 decisioning layer ---------------------------------
	// All three engines operate on the already-populated Customer360,
	// so no new Axiom queries. The NBA engine persists its outputs
	// into SQLite (for cooldown enforcement + outcome learning).
	resp.Predictions = computePredictions(resp)
	resp.JourneyStage = computeJourneyStage(resp, resp.Predictions)
	resp.Recommendations = rankRecommendations(ctx, mgr.LocalDB(), resp, resp.Predictions, individualID)
	return resp, nil
}

// ── Identity ─────────────────────────────────────────────────

// resolveCandidates returns every party.individual row matching the
// given phone or email, up to 10. Used by the lookup layer to show a
// picker when a single lookup is ambiguous. Returns nil + no error
// when the mode is unsupported (caller should skip the disambiguation
// step).
//
// For phone lookups we union FOUR sources so family plans resolve
// correctly:
//   1. party.contact_medium.phone_number — matches the billing
//      contact's own phone field.
//   2. customer.public.vw_service_account_state_latest.msisdn — the
//      SIM/MSISDN-level view. Family members who own a SIM on a shared
//      account appear here even when their party.contact_medium row
//      carries a different (or no) phone, which is the common case
//      rain BSS produces.
//   3. account.public.rainone_customers.msisdn — the rainOne customer
//      roster (4.8M rows) which carries user_id as text; catches
//      rainOne-specific account holders Station finds but the two
//      sources above miss.
//   4. product.public.product_billing_resources_tmp.msisdn — the
//      MSISDN↔related_party_id↔imsi bridge table (7M rows). Last-
//      chance pivot for edge cases where the SIM is active in the
//      product layer but not yet denormalised into the views above.
//
// Without these sources we silently auto-pick the billing owner and
// hide the other individuals — reproducing the "only one account"
// bug reported against +27744432221.
func resolveCandidates(ctx context.Context, mgr *Manager, connID string, partyPool *pgxpool.Pool, log *slog.Logger, mode, value string) ([]IdentityCandidate, error) {
	// Per-source queries run in parallel — each needs its own slice
	// of the budget, not a shared sequential 5s. 12s total is plenty
	// for four independent ops and still comfortably inside the
	// parent request timeout.
	subCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	// seed carries whatever we learned about a candidate before the
	// party.individual fetch. Keyed by individual_id (candidate id).
	type seed struct {
		id            string
		accountNumber string
		msisdn        string
		source        string
	}
	seeds := map[string]*seed{}
	counts := map[string]int{}
	var mu sync.Mutex
	upsert := func(id, acct, msisdn, src string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		s, ok := seeds[id]
		if !ok {
			s = &seed{id: id, source: src}
			seeds[id] = s
		}
		if s.accountNumber == "" && acct != "" {
			s.accountNumber = acct
		}
		if s.msisdn == "" && msisdn != "" {
			s.msisdn = msisdn
		}
		if s.source == "" && src != "" {
			s.source = src
		}
		counts[src]++
	}

	switch strings.ToLower(mode) {
	case "phone":
		digits := keepDigits(value)
		if len(digits) < 7 {
			return nil, nil
		}
		trail := digits[len(digits)-9:]
		variants := msisdnVariantsBig(digits)

		var wg sync.WaitGroup

		// Source 1 — party.contact_medium (billing contact phone).
		// Plain ILIKE on the trailing 9 digits — a regex_replace
		// scan on 2.5M rows reliably exhausts the query budget
		// before returning. ILIKE is still a full scan but the
		// per-row cost is ~50× lower, so it completes in time. The
		// tradeoff: phones stored with punctuation in the last 9
		// digits (rare) won't match — the other three sources
		// cover those edge cases.
		wg.Add(1)
		go func() {
			defer wg.Done()
			qctx, qcancel := context.WithTimeout(subCtx, 8*time.Second)
			defer qcancel()
			// Mirror the fast-path ORDER BY + LIMIT pattern that
			// resolveIdentityProd uses — gives Postgres the option
			// to use a preferred/updated_at index and short-circuit
			// once 20 rows are found instead of full-scanning 2.5M.
			// LIMIT 5 (was 20) + ORDER BY preferred encourages
			// Postgres to stop scanning as soon as five rows match
			// via the preferred/updated_at index, rather than full-
			// scanning to guarantee the top 20. Cuts snapshot hold
			// time roughly in half on this 2.5M-row table.
			cmRows, err := partyPool.Query(qctx, `
				SELECT individual_id
				  FROM party.contact_medium
				 WHERE phone_number ILIKE '%' || $1
				   AND individual_id IS NOT NULL
				 ORDER BY preferred DESC NULLS LAST, updated_at DESC
				 LIMIT 5`, trail)
			if err != nil {
				if log != nil {
					log.Warn("candidates: contact_medium query", "error", err)
				}
				return
			}
			defer cmRows.Close()
			for cmRows.Next() {
				var id string
				if cmRows.Scan(&id) == nil {
					upsert(id, "", "", "contact_medium")
				}
			}
			if err := cmRows.Err(); err != nil && log != nil {
				log.Warn("candidates: contact_medium iter", "error", err)
			}
		}()

		// Source 2 — customer.vw_service_account_state_latest (MSISDN
		// → user_id + account_number). The view's user_id is uuid and
		// DOES NOT always match party.individual.id 1:1 in our data
		// — so we keep the account_number + msisdn on the seed and
		// return the candidate even when party.individual has no row.
		if mgr != nil && connID != "" && len(variants) > 0 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				qctx, qcancel := context.WithTimeout(subCtx, 3*time.Second)
				defer qcancel()
				custPool, _, perr := mgr.PoolByIDWithDB(qctx, connID, "customer")
				if perr != nil {
					if log != nil {
						log.Warn("candidates: sim_view pool", "error", perr)
					}
					return
				}
				msRows, err := custPool.Query(qctx, `
					SELECT DISTINCT
					       user_id::text,
					       COALESCE(account_number, ''),
					       COALESCE(msisdn::text, '')
					  FROM public.vw_service_account_state_latest
					 WHERE msisdn = ANY($1)
					   AND user_id IS NOT NULL
					 LIMIT 10`, variants)
				if err != nil {
					if log != nil {
						log.Warn("candidates: sim_view query", "error", err)
					}
					return
				}
				defer msRows.Close()
				for msRows.Next() {
					var id, acct, ms string
					if msRows.Scan(&id, &acct, &ms) == nil {
						upsert(id, acct, ms, "sim_view")
					}
				}
				if err := msRows.Err(); err != nil && log != nil {
					log.Warn("candidates: sim_view iter", "error", err)
				}
			}()
		}

		// Source 3 — account.public.rainone_customers (MSISDN →
		// user_id). user_id here is already text in the catalogue,
		// which is friendlier than the sim_view's uuid. MSISDN is
		// stored as bigint; match against the variant list.
		if mgr != nil && connID != "" && len(variants) > 0 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				qctx, qcancel := context.WithTimeout(subCtx, 3*time.Second)
				defer qcancel()
				acctPool, _, perr := mgr.PoolByIDWithDB(qctx, connID, "account")
				if perr != nil {
					if log != nil {
						log.Warn("candidates: rainone_customers pool", "error", perr)
					}
					return
				}
				rcRows, err := acctPool.Query(qctx, `
					SELECT DISTINCT
					       user_id::text,
					       COALESCE(msisdn::text, '')
					  FROM public.rainone_customers
					 WHERE msisdn = ANY($1::bigint[])
					   AND user_id IS NOT NULL
					 LIMIT 10`, variants)
				if err != nil {
					if log != nil {
						log.Warn("candidates: rainone_customers query", "error", err)
					}
					return
				}
				defer rcRows.Close()
				for rcRows.Next() {
					var id, ms string
					if rcRows.Scan(&id, &ms) == nil {
						upsert(id, "", ms, "rainone_customers")
					}
				}
				if err := rcRows.Err(); err != nil && log != nil {
					log.Warn("candidates: rainone_customers iter", "error", err)
				}
			}()
		}

		// Source 4 — product.public.product_billing_resources_tmp —
		// INTENTIONALLY DISABLED. The `_tmp` table suffix means
		// there's almost certainly no index on msisdn, so ANY
		// lookup is a full scan of 7M rows. That held open
		// snapshots on Axiom prod long enough to cause replication
		// lag. Re-enable ONLY once BSS confirms an index exists on
		// product.public.product_billing_resources_tmp(msisdn), or
		// after moving reads off the primary onto a replica /
		// Snowflake middleware target.
		_ = msisdnVariantsText // keep helper compiled for future use

		wg.Wait()
	case "email":
		cmRows, err := partyPool.Query(subCtx, `
			SELECT DISTINCT cm.individual_id
			  FROM party.contact_medium cm
			 WHERE cm.email_address ILIKE $1
			   AND cm.individual_id IS NOT NULL
			 LIMIT 10`, value)
		if err != nil {
			return nil, err
		}
		for cmRows.Next() {
			var id string
			if cmRows.Scan(&id) == nil {
				upsert(id, "", "", "contact_medium")
			}
		}
		cmRows.Close()
	default:
		return nil, nil
	}

	if log != nil {
		log.Info("resolve candidates",
			"mode", mode,
			"value", value,
			"contact_medium", counts["contact_medium"],
			"sim_view", counts["sim_view"],
			"rainone_customers", counts["rainone_customers"],
			"product_billing", counts["product_billing"],
			"unique_ids", len(seeds),
		)
	}

	if len(seeds) <= 1 {
		return nil, nil
	}
	list := make([]string, 0, len(seeds))
	for id := range seeds {
		list = append(list, id)
	}

	// Enrich with party.individual — any id that returns a row gets
	// full_name / email / created_at. Ids that don't match still
	// become candidates using just the seed (source="sim_view"
	// gives us account_number + msisdn so the card is still useful).
	enriched := map[string]IdentityCandidate{}
	indRows, err := partyPool.Query(subCtx, `
		SELECT id,
		       COALESCE(full_name, COALESCE(given_name,'') || ' ' || COALESCE(family_name,'')),
		       COALESCE(given_name,''), COALESCE(family_name,''),
		       COALESCE(login_name,''),
		       inserted_at
		  FROM party.individual
		 WHERE id = ANY($1::text[])`, list)
	if err == nil {
		for indRows.Next() {
			var c IdentityCandidate
			if err := indRows.Scan(&c.ID, &c.FullName, &c.GivenName, &c.FamilyName, &c.Email, &c.CreatedAt); err == nil {
				enriched[c.ID] = c
			}
		}
		indRows.Close()
	}

	out := make([]IdentityCandidate, 0, len(seeds))
	for _, s := range seeds {
		c, ok := enriched[s.id]
		if !ok {
			c = IdentityCandidate{ID: s.id}
		}
		// Always overlay the seed's view-only fields — even when
		// party.individual matched, the account_number / msisdn help
		// the user tell two accounts apart.
		c.AccountNumber = s.accountNumber
		c.MSISDN = s.msisdn
		c.Source = s.source
		out = append(out, c)
	}
	// Newest-first by party.individual.inserted_at so the most
	// recently touched account rises to the top; view-only entries
	// (zero CreatedAt) sort last.
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j].CreatedAt.After(out[i].CreatedAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

// msisdnVariantsText is the string-typed counterpart of
// msisdnVariantsBig — some Axiom tables (e.g. product.
// product_billing_resources_tmp) store msisdn as TEXT, so we need
// matching text variants to avoid a type-cast error on ANY($1::bigint[]).
func msisdnVariantsText(digits string) []string {
	// 9-digit minimum — shorter inputs can't produce a valid
	// MSISDN and would panic on the digits[len-9:] slice below.
	if len(digits) < 9 {
		return nil
	}
	seen := map[string]struct{}{}
	out := []string{}
	add := func(s string) {
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	last9 := digits[len(digits)-9:]
	add("27" + last9)
	add("0" + last9)
	add(last9)
	add(digits)
	return out
}

// msisdnVariantsBig expands a digit-only phone string into every form
// the Axiom MSISDN column might carry as bigint:
//   - E164 no plus   ("27744432221")
//   - local leading-zero stripped ("744432221") — often just "msisdn" on views
//   - plus-stripped with leading zero replaced ("0744432221" → 744432221)
//
// The returned []int64 is deduped and drops anything that doesn't fit
// in bigint or is too short to be a ZA mobile number.
func msisdnVariantsBig(digits string) []int64 {
	// Need at least 9 digits to slice the trailing 9 — shorter
	// inputs can't produce a valid ZA MSISDN anyway.
	if len(digits) < 9 {
		return nil
	}
	seen := map[int64]struct{}{}
	out := []int64{}
	add := func(s string) {
		if s == "" {
			return
		}
		var v int64
		if _, err := fmt.Sscanf(s, "%d", &v); err != nil || v <= 0 {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	last9 := digits[len(digits)-9:]
	add("27" + last9) // E164 no plus
	add("0" + last9)  // local leading-zero
	add(last9)        // bare national
	add(digits)       // whatever the user typed
	return out
}

func resolveIdentityProd(ctx context.Context, pool *pgxpool.Pool, mode, value string) (string, Identity, []ContactMedium, error) {
	subCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var indID string
	switch strings.ToLower(mode) {
	case "phone":
		// Strip leading + / zeros; match trailing digits so "0824001122"
		// matches "+27824001122" without needing to normalise in SQL.
		digits := keepDigits(value)
		if len(digits) < 7 {
			return "", Identity{}, nil, fmt.Errorf("phone too short")
		}
		trail := "%" + digits[len(digits)-9:]
		err := pool.QueryRow(subCtx,
			`SELECT individual_id FROM party.contact_medium
			  WHERE phone_number ILIKE $1
			    AND individual_id IS NOT NULL
			  ORDER BY preferred DESC NULLS LAST, updated_at DESC
			  LIMIT 1`, trail,
		).Scan(&indID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", Identity{}, nil, fmt.Errorf("phone lookup: %w", err)
		}
	case "email":
		err := pool.QueryRow(subCtx,
			`SELECT COALESCE(individual_id,'') FROM party.contact_medium
			  WHERE email_address ILIKE $1
			  ORDER BY preferred DESC NULLS LAST, updated_at DESC
			  LIMIT 1`, value,
		).Scan(&indID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", Identity{}, nil, fmt.Errorf("email lookup: %w", err)
		}
		if indID == "" {
			// Fallback: login_name on individual directly.
			_ = pool.QueryRow(subCtx,
				`SELECT id FROM party.individual WHERE login_name ILIKE $1 LIMIT 1`, value,
			).Scan(&indID)
		}
	case "id":
		indID = value
	default:
		return "", Identity{}, nil, fmt.Errorf("unknown lookup mode: %s", mode)
	}

	if indID == "" {
		return "", Identity{}, nil, nil
	}

	// Full identity row.
	var id Identity
	err := pool.QueryRow(subCtx,
		`SELECT id,
		        COALESCE(full_name, COALESCE(given_name,'') || ' ' || COALESCE(family_name,'')),
		        COALESCE(given_name,''), COALESCE(family_name,''),
		        COALESCE(login_name,''),
		        COALESCE(marital_status,'ACTIVE'),
		        inserted_at
		   FROM party.individual WHERE id = $1`, indID,
	).Scan(&id.ID, &id.FullName, &id.GivenName, &id.FamilyName, &id.Email, &id.Status, &id.CreatedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", Identity{}, nil, fmt.Errorf("fetch identity: %w", err)
	}
	if id.ID == "" {
		id.ID = indID
	}

	// All contact media for the individual (addresses + emails + phones).
	rows, err := pool.Query(subCtx,
		`SELECT COALESCE(email_address,''), COALESCE(phone_number,''),
		        COALESCE(street_number,''), COALESCE(street_name,''),
		        COALESCE(suburb,''), COALESCE(city,''),
		        COALESCE(state_or_province,''), COALESCE(postal_code,''),
		        COALESCE(preferred,false), updated_at
		   FROM party.contact_medium
		  WHERE individual_id = $1
		  ORDER BY preferred DESC NULLS LAST, updated_at DESC
		  LIMIT 25`, indID,
	)
	if err != nil {
		return indID, id, nil, nil
	}
	defer rows.Close()
	var contacts []ContactMedium
	for rows.Next() {
		var c ContactMedium
		_ = rows.Scan(&c.Email, &c.Phone, &c.StreetNumber, &c.StreetName,
			&c.Suburb, &c.City, &c.Province, &c.PostalCode, &c.Preferred, &c.UpdatedAt)
		contacts = append(contacts, c)
	}
	return indID, id, contacts, nil
}

// ── Account (billing_account + account_balance) ──────────────

func fetchAccountBits(ctx context.Context, pool *pgxpool.Pool, individualID string) ([]BillingAccount, []AccountBalance, error) {
	baRows, err := pool.Query(ctx, `
		SELECT id, COALESCE(name,''), COALESCE(state,''),
		       COALESCE(account_type,''), COALESCE(payment_status,''),
		       COALESCE(credit_limit,0),
		       COALESCE(payment_day,0),
		       COALESCE(financial_account_id,''),
		       updated_at
		  FROM account.billing_account
		 WHERE related_party_id = $1
		 ORDER BY updated_at DESC
		 LIMIT 10`, individualID)
	if err != nil {
		return nil, nil, fmt.Errorf("billing_account: %w", err)
	}
	defer baRows.Close()
	var bas []BillingAccount
	for baRows.Next() {
		var b BillingAccount
		if err := baRows.Scan(&b.ID, &b.Name, &b.State, &b.AccountType,
			&b.PaymentStatus, &b.CreditLimit, &b.PaymentDay,
			&b.FinancialAccountID, &b.UpdatedAt); err != nil {
			continue
		}
		bas = append(bas, b)
	}

	baIDs := make([]string, 0, len(bas))
	for _, b := range bas { baIDs = append(baIDs, b.ID) }
	// account_balance can be keyed by related_party_id OR
	// billing_account_id — hit both so we don't miss rows written only
	// against the account pivot.
	balRows, err := pool.Query(ctx, `
		SELECT balance_type, COALESCE(amount,0)::float8,
		       COALESCE(last_invoice_amount,0)::float8,
		       valid_for_from, valid_for_to
		  FROM account.account_balance
		 WHERE related_party_id = $1
		    OR billing_account_id = ANY($2)
		 ORDER BY valid_for_from DESC NULLS LAST
		 LIMIT 20`, individualID, baIDs)
	if err != nil {
		return bas, nil, fmt.Errorf("account_balance: %w", err)
	}
	defer balRows.Close()
	var bals []AccountBalance
	for balRows.Next() {
		var b AccountBalance
		var fromT, toT *time.Time
		if err := balRows.Scan(&b.BalanceType, &b.Amount, &b.LastInvoiceAmount, &fromT, &toT); err != nil {
			continue
		}
		if fromT != nil { b.ValidFrom = *fromT }
		if toT != nil { b.ValidTo = *toT }
		bals = append(bals, b)
	}
	return bas, bals, nil
}

// ── Promises-to-pay ───────────────────────────────────────────

func fetchPromises(ctx context.Context, pool *pgxpool.Pool, individualID string) ([]PromiseToPay, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, COALESCE(status,''),
		       COALESCE(total_amount,0),
		       COALESCE(total_amount_allocated,0),
		       COALESCE(promise_to_pay_balance,0),
		       COALESCE(number_of_payments,0),
		       COALESCE(installment_amount,0),
		       COALESCE(payment_frequency,''),
		       valid_for_from, valid_for_to
		  FROM account.promise_to_pay
		 WHERE customer_id = $1
		 ORDER BY updated_at DESC
		 LIMIT 10`, individualID)
	if err != nil {
		return nil, fmt.Errorf("promise_to_pay: %w", err)
	}
	defer rows.Close()
	var out []PromiseToPay
	for rows.Next() {
		var p PromiseToPay
		var fromT, toT *time.Time
		if err := rows.Scan(&p.ID, &p.Status, &p.TotalAmount, &p.TotalAllocated,
			&p.Balance, &p.NumberOfPayments, &p.InstalmentAmount,
			&p.PaymentFrequency, &fromT, &toT); err != nil {
			continue
		}
		if fromT != nil { p.ValidFrom = *fromT }
		if toT != nil { p.ValidTo = *toT }
		out = append(out, p)
	}
	return out, nil
}

// ── Payments ──────────────────────────────────────────────────

func fetchPaymentsProd(ctx context.Context, pool *pgxpool.Pool, individualID string) ([]Payment, error) {
	rows, err := pool.Query(ctx, `
		SELECT id,
		       COALESCE(total_amount_value,0)::float8,
		       COALESCE(channel,''), COALESCE(status,''),
		       COALESCE(payment_date, status_date, inserted_at)
		  FROM payment.payment
		 WHERE payer_id = $1
		 ORDER BY COALESCE(payment_date, status_date, inserted_at) DESC
		 LIMIT 50`, individualID)
	if err != nil {
		return nil, fmt.Errorf("payment: %w", err)
	}
	defer rows.Close()
	var out []Payment
	for rows.Next() {
		var p Payment
		if err := rows.Scan(&p.ID, &p.Amount, &p.Channel, &p.Status, &p.PaymentDate); err != nil {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// ── Invoices (customer DB) ────────────────────────────────────

// fetchInvoicesByFinAccount pivots through the billing account's
// financial_account_id. invoices.financial_account_id is uuid; the
// billing_account stores it as varchar — so we cast each candidate.
// Invalid UUIDs fall out of the IN list silently.
func fetchInvoicesByFinAccount(ctx context.Context, pool *pgxpool.Pool, finIDs []string) ([]Invoice, error) {
	if len(finIDs) == 0 { return nil, nil }
	// Validate UUID shape client-side so a bad id can't error the whole query.
	valid := make([]string, 0, len(finIDs))
	for _, id := range finIDs {
		if looksLikeUUID(id) { valid = append(valid, id) }
	}
	if len(valid) == 0 { return nil, nil }
	rows, err := pool.Query(ctx, `
		SELECT COALESCE(invoice_number,''),
		       invoice_date, due_date,
		       COALESCE(amount,0)::float8,
		       COALESCE(balance,0)::float8,
		       COALESCE(status,''),
		       COALESCE(source,'')
		  FROM public.invoices
		 WHERE financial_account_id = ANY($1::uuid[])
		 ORDER BY invoice_date DESC NULLS LAST
		 LIMIT 12`, valid)
	if err != nil {
		return nil, fmt.Errorf("invoices: %w", err)
	}
	defer rows.Close()
	var out []Invoice
	for rows.Next() {
		var inv Invoice
		var ivDate, dueDate *time.Time
		if err := rows.Scan(&inv.InvoiceNumber, &ivDate, &dueDate,
			&inv.Amount, &inv.Balance, &inv.Status, &inv.Source); err != nil {
			continue
		}
		if ivDate != nil { inv.InvoiceDate = *ivDate }
		if dueDate != nil { inv.DueDate = *dueDate }
		out = append(out, inv)
	}
	return out, nil
}

func looksLikeUUID(s string) bool {
	if len(s) != 36 { return false }
	for i, r := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' { return false }
			continue
		}
		ok := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
		if !ok { return false }
	}
	return true
}

// ── Services ──────────────────────────────────────────────────

func fetchServices(ctx context.Context, pool *pgxpool.Pool, individualID string) ([]Subscription, error) {
	rows, err := pool.Query(ctx, `
		SELECT id,
		       COALESCE(category,'') || ' · ' || COALESCE(state,''),
		       COALESCE(state,''),
		       COALESCE(start_date, order_date, inserted_at),
		       0
		  FROM service.service_order
		 WHERE related_party_id = $1
		 ORDER BY COALESCE(start_date, order_date, inserted_at) DESC
		 LIMIT 20`, individualID)
	if err != nil {
		return nil, fmt.Errorf("service_order: %w", err)
	}
	defer rows.Close()
	var out []Subscription
	for rows.Next() {
		var s Subscription
		if err := rows.Scan(&s.ID, &s.Name, &s.Status, &s.StartedAt, &s.Price); err != nil {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

// ── Tickets (snowflake middleware DB) ─────────────────────────

func fetchTicketsProd(ctx context.Context, pool *pgxpool.Pool, individualID string) ([]Ticket, error) {
	// snowflake.public.ticket ← entity_ticket ← related_entity(idm_id)
	// The `idm_id` column on related_entity is the text link back to
	// party.individual.id — that's where the bridge between the varchar
	// identity world and the bigint ticket world lives.
	rows, err := pool.Query(ctx, `
		SELECT t.id::text, COALESCE(t.name,''),
		       COALESCE(t.priority,''),
		       t.inserted_at
		  FROM public.ticket t
		  JOIN public.entity_ticket et ON et.ticket_id = t.id
		  JOIN public.related_entity re ON re.id = et.related_entity_id
		 WHERE re.idm_id = $1
		 ORDER BY t.inserted_at DESC
		 LIMIT 20`, individualID)
	if err != nil {
		return nil, fmt.Errorf("ticket: %w", err)
	}
	defer rows.Close()
	var out []Ticket
	for rows.Next() {
		var t Ticket
		if err := rows.Scan(&t.ID, &t.Subject, &t.Status, &t.CreatedAt); err != nil {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

// ── Notifications (communication DB) ──────────────────────────

func fetchRecentSMS(ctx context.Context, pool *pgxpool.Pool, phones []string, individualID string) ([]NotificationEvent, error) {
	// sms_history.msisdn is bigint — strip '+' and cast.
	bigs := make([]int64, 0, len(phones))
	for _, p := range phones {
		digits := keepDigits(p)
		if len(digits) < 9 { continue }
		// Trim to last 12 chars to fit bigint.
		if len(digits) > 12 { digits = digits[len(digits)-12:] }
		var v int64
		_, err := fmt.Sscanf(digits, "%d", &v)
		if err == nil && v > 0 { bigs = append(bigs, v) }
	}
	if len(bigs) == 0 && individualID == "" {
		return nil, nil
	}
	// Narrow to msisdn (indexed bigint) only + 7-day window. The
	// customer_id text path forces a full scan on 213M rows and times
	// out.
	if len(bigs) == 0 {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `
		SELECT 'sms', COALESCE(msisdn::text,''),
		       COALESCE(status::text,''),
		       COALESCE(message,''),
		       inserted_at
		  FROM notification.sms_history
		 WHERE msisdn = ANY($1)
		   AND inserted_at > now() - interval '7 days'
		 ORDER BY inserted_at DESC
		 LIMIT 25`, bigs)
	if err != nil {
		return nil, fmt.Errorf("sms_history: %w", err)
	}
	defer rows.Close()
	var out []NotificationEvent
	for rows.Next() {
		var n NotificationEvent
		if err := rows.Scan(&n.Channel, &n.MSISDN, &n.Status, &n.Message, &n.InsertedAt); err != nil {
			continue
		}
		if len(n.Message) > 160 { n.Message = n.Message[:160] + "…" }
		out = append(out, n)
	}
	return out, nil
}

// ── Products (service DB) ─────────────────────────────────────

// msisdnsOf extracts digit-only MSISDNs from the contact media and
// expands each to every format rain's BSS might have stored:
//   - E164 without plus   ("27744432221")  ← resource_ref.id uses this
//   - local leading-zero  ("0744432221")   ← contact_medium often uses this
//   - bare national       ("744432221")    ← some clusters strip the zero
//
// Input is whatever the user typed ("+27…", "0…", "27…", even with
// dashes/spaces). Output is deduped.
func msisdnsOf(cs []ContactMedium) []string {
	seen := map[string]struct{}{}
	out := []string{}
	add := func(v string) {
		if v == "" { return }
		if _, ok := seen[v]; ok { return }
		seen[v] = struct{}{}
		out = append(out, v)
	}
	for _, c := range cs {
		digits := keepDigits(c.Phone)
		if len(digits) < 9 { continue }
		// The "canonical 9" — last 9 digits. All three forms are built
		// off this, so a phone typed "+27 74 443 2221" and one typed
		// "074 443 2221" collapse to the same three lookups.
		last9 := digits[len(digits)-9:]
		add("27" + last9)  // E164 no plus (resource_ref.id format)
		add("0" + last9)   // local leading-zero
		add(last9)         // bare national
		// Also keep the raw digits in case the BSS stored something
		// weirder like "+27…" with the plus intact.
		add(digits)
	}
	return out
}

// classifyProductFamily maps rain product strings into the three UI
// families: "mobile" (SIM-based), "loop" (LTE CPE), "101" (rain mobile
// handsets/the 101 brand), or "other".
func classifyProductFamily(category, serviceType, name string) string {
	blob := strings.ToLower(category + " " + serviceType + " " + name)
	switch {
	case strings.Contains(blob, "101"):
		return "101"
	case strings.Contains(blob, "loop") || strings.Contains(blob, "lte") ||
		strings.Contains(blob, "router") || strings.Contains(blob, "cpe") ||
		strings.Contains(blob, "home") || strings.Contains(blob, "5g"):
		return "loop"
	case strings.Contains(blob, "mobile") || strings.Contains(blob, "sim") ||
		strings.Contains(blob, "postpaid") || strings.Contains(blob, "prepaid") ||
		strings.Contains(blob, "voice") || strings.Contains(blob, "data"):
		return "mobile"
	}
	return "other"
}

// ── Usage (resource DB) ───────────────────────────────────────

// fetchIMSIsByAccount reads the SIM inventory view to find every
// IMSI bound to any of the customer's billing accounts. IMSI is the
// canonical SIM identifier — ralf.resource_policy (where live usage,
// quota + policy rows live) is keyed by IMSI, NOT by phone number.
//
// We pivot on BOTH financial_account_id (uuid) AND account_number
// (text). Either alone is usually enough, but different rain
// environments populate them inconsistently, so accepting both keeps
// us robust. Invalid UUIDs are filtered client-side to keep the
// query from erroring on bad data. Returns deduped non-zero IMSIs,
// capped at 50 SIMs.
func fetchIMSIsByAccount(ctx context.Context, pool *pgxpool.Pool, finAccts, accountNums []string) ([]int64, error) {
	validFin := make([]string, 0, len(finAccts))
	for _, id := range finAccts {
		if looksLikeUUID(id) {
			validFin = append(validFin, id)
		}
	}
	if len(validFin) == 0 && len(accountNums) == 0 {
		return nil, nil
	}
	// Use two conditions OR'd — either the financial_account_id
	// matches OR the account_number matches. Pass both arrays so the
	// planner can use whichever index exists.
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT imsi
		  FROM public.vw_service_account_state_latest
		 WHERE imsi IS NOT NULL
		   AND (
		         financial_account_id = ANY($1::uuid[])
		      OR account_number = ANY($2::text[])
		   )
		 LIMIT 50`, validFin, accountNums)
	if err != nil {
		return nil, fmt.Errorf("vw_service_account_state_latest: %w", err)
	}
	defer rows.Close()
	var out []int64
	seen := map[int64]struct{}{}
	for rows.Next() {
		var v int64
		if rows.Scan(&v) != nil || v <= 0 {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out, nil
}

// fetchIMSIsByMSISDN is the fallback pivot: hit the SIM inventory
// view directly by msisdn instead of by billing-account. Catches
// the case where a customer's billing_account_id doesn't match the
// view's account_number field (e.g. consolidated accounts, porting
// edge cases, view lag). IMSI is still the target column — we just
// take a different route to it.
//
// Input phones are the customer's contact_medium entries; we expand
// each to the three bigint variants Axiom commonly stores
// (27…, 0…, bare national).
func fetchIMSIsByMSISDN(ctx context.Context, pool *pgxpool.Pool, phones []string) ([]int64, error) {
	bigs := make([]int64, 0, len(phones)*3)
	seenIn := map[int64]struct{}{}
	for _, p := range phones {
		for _, v := range msisdnVariantsBig(keepDigits(p)) {
			if _, ok := seenIn[v]; ok {
				continue
			}
			seenIn[v] = struct{}{}
			bigs = append(bigs, v)
		}
	}
	if len(bigs) == 0 {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT imsi
		  FROM public.vw_service_account_state_latest
		 WHERE imsi IS NOT NULL
		   AND msisdn = ANY($1::bigint[])
		 LIMIT 50`, bigs)
	if err != nil {
		return nil, fmt.Errorf("vw_service_account_state_latest (msisdn): %w", err)
	}
	defer rows.Close()
	var out []int64
	seen := map[int64]struct{}{}
	for rows.Next() {
		var v int64
		if rows.Scan(&v) != nil || v <= 0 {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out, nil
}

// resolveIMSIs is the shared entry point for finding the IMSIs a
// customer owns. Three pivots, tried in order, first hit wins:
//
//   1. billing_account → vw_service_account_state_latest (fastest,
//      covers most accounts)
//   2. contact_medium.phone → view.msisdn (catches SIMs where the
//      billing account number doesn't match the view row)
//   3. service_accounts.subscriber → service_accounts.user_id →
//      view.user_id. This is the definitive mapping: the party id
//      lives on service_accounts.subscriber (varchar), which
//      carries the uuid user_id used as the view's primary
//      identifier. Used by the SIMs that don't share a billing
//      account or phone with the customer record (common for
//      device-only rainOne / 101 bundles).
//
// `source` is just a label for the diagnostic log line so we can
// tell which pivot produced the hit.
//
// Audit (Phase 3, docs/axiom/sim-diagnostics-plan.md): every call
// emits a row to imsi_lookup_audit. The named return + deferred
// writeIMSIAudit captures whichever pivot won.
//
// Phase 2 (eng-review 1B + 2A): returns []IMSISource carrying the
// winning phase per IMSI, plus a fail-closed audit error. If the
// audit write fails, resp.MarkAuditFailed() flips the fail-closed
// flag — LookupProd refuses to serialise Customer360 in that case.
//
// SimDiagnostics is populated once per LookupProd: this helper
// fills resp.SimDiagnostics on first call (when it's empty).
// Subsequent calls (cdr_usage path after usage path, etc.) just
// reuse the data and don't overwrite.
func resolveIMSIs(ctx context.Context, mgr *Manager, connID string, custPool *pgxpool.Pool, resp *Customer360, log *slog.Logger, individualID, source string) (sources []IMSISource) {
	winningPhase := "exhausted"
	defer func() {
		// Populate panel-facing diagnostics once per LookupProd.
		// resolveIMSIs is called by both the cdr_usage and usage
		// fetches — only the first one fills the slice.
		if len(resp.SimDiagnostics) == 0 && len(sources) > 0 {
			resp.SimDiagnostics = append(resp.SimDiagnostics, sources...)
		}
		if mgr != nil {
			if err := writeIMSIAudit(ctx, mgr.LocalDB(), individualID, source, winningPhase, len(sources)); err != nil {
				if log != nil {
					log.Error("imsi audit write FAILED — fail-closed engaged", "error", err, "source", source, "phase", winningPhase)
				}
				if resp != nil {
					resp.MarkAuditFailed()
				}
			}
		}
	}()

	now := time.Now().UTC()
	build := func(phase string, imsis []int64) []IMSISource {
		out := make([]IMSISource, 0, len(imsis))
		for _, v := range imsis {
			out = append(out, IMSISource{IMSI: v, Source: phase, ResolvedAt: now})
		}
		return out
	}

	// Phase 0: manual override. When the operator knows the IMSIs
	// for a customer (common for VIP accounts, internal test
	// accounts, and cases where our pivots can't find the join)
	// they save them via the IMSI override endpoint. LookupProd
	// loads the override onto resp.IMSIOverrides before invoking
	// the fetchers — so it's already here by the time we run.
	if len(resp.IMSIOverrides) > 0 {
		if log != nil {
			log.Info(source+" imsi pivot",
				"individual", individualID,
				"imsis", len(resp.IMSIOverrides), "source", "override")
		}
		winningPhase = "override"
		sources = build("override", append([]int64(nil), resp.IMSIOverrides...))
		return
	}
	finAccts := make([]string, 0, len(resp.BillingAccounts))
	accountNums := make([]string, 0, len(resp.BillingAccounts))
	billingAcctIDs := make([]string, 0, len(resp.BillingAccounts))
	for _, b := range resp.BillingAccounts {
		if b.FinancialAccountID != "" { finAccts = append(finAccts, b.FinancialAccountID) }
		if b.Name != "" { accountNums = append(accountNums, b.Name) }
		if b.ID != "" { billingAcctIDs = append(billingAcctIDs, b.ID) }
	}
	// Primary pivot: product → jt_prod_rs_ref → resource_ref (user's
	// confirmed join path). Runs before the account/msisdn pivots
	// because it's the authoritative TMF path — every SIM on a
	// billing account is a product row linked to a resource_ref.
	if mgr != nil && connID != "" && len(billingAcctIDs) > 0 {
		prodImsis, err := fetchIMSIsViaProductPath(ctx, mgr, connID, billingAcctIDs)
		if err != nil && log != nil {
			log.Warn(source+" imsi (product_path)", "error", err)
		}
		if len(prodImsis) > 0 {
			if log != nil {
				log.Info(source+" imsi pivot",
					"individual", individualID,
					"billing_accounts", len(billingAcctIDs),
					"imsis", len(prodImsis), "source", "product_path")
			}
			winningPhase = "product_path"
			sources = build("product_path", prodImsis)
			return
		}
	}
	// Fallback: billing_account → vw_service_account_state_latest.
	acctImsis, err := fetchIMSIsByAccount(ctx, custPool, finAccts, accountNums)
	if err != nil && log != nil {
		log.Warn(source+" imsi (account)", "error", err)
	}
	if len(acctImsis) > 0 {
		if log != nil {
			log.Info(source+" imsi pivot",
				"individual", individualID,
				"fin_accts", len(finAccts), "account_nums", len(accountNums),
				"imsis", len(acctImsis), "source", "account")
		}
		winningPhase = "view_account"
		sources = build("view_account", acctImsis)
		return
	}
	// Fallback 1: contact_medium phones → view.msisdn.
	phones := phonesOf(resp.Contacts)
	if len(phones) > 0 {
		msisdnImsis, err := fetchIMSIsByMSISDN(ctx, custPool, phones)
		if err != nil && log != nil {
			log.Warn(source+" imsi (msisdn)", "error", err)
		}
		if len(msisdnImsis) > 0 {
			if log != nil {
				log.Info(source+" imsi pivot",
					"individual", individualID, "phones", len(phones),
					"imsis", len(msisdnImsis), "source", "msisdn")
			}
			winningPhase = "view_msisdn"
			sources = build("view_msisdn", msisdnImsis)
			return
		}
	}
	// Fallback 2: service_accounts.subscriber → user_id → view.
	subImsis, err := fetchIMSIsBySubscriber(ctx, custPool, individualID)
	if err != nil && log != nil {
		log.Warn(source+" imsi (subscriber)", "error", err)
	}
	if log != nil {
		log.Info(source+" imsi pivot",
			"individual", individualID,
			"imsis", len(subImsis), "source", "subscriber")
	}
	if len(subImsis) > 0 {
		winningPhase = "view_subscriber"
		sources = build("view_subscriber", subImsis)
	}
	return
}

// imsiInts unwraps IMSISource records into the bare []int64 the
// downstream Athena / resource_policy fetchers expect. Side-step
// for callers that don't care about provenance — most of the
// existing code only needs the IMSI int.
func imsiInts(srcs []IMSISource) []int64 {
	out := make([]int64, 0, len(srcs))
	for _, s := range srcs {
		out = append(out, s.IMSI)
	}
	return out
}

// incidentIDCtxKey is the context.Context key for the active
// incident_id. Phase D2 follow-up: lets us thread incident_id
// from the HTTP handler (or agent dispatcher) all the way down
// to writeIMSIAudit without touching every intermediate signature
// in this file. The Customer 360 handler is the producer; this
// package is the consumer.
type ctxKeyIncidentID struct{}

// WithIncidentID returns a derived context carrying the
// incident_id so audit writes inside the cascade can tag rows.
// Public so the server package + agent dispatcher can set it.
func WithIncidentID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyIncidentID{}, id)
}

// IncidentIDFromContext extracts the value set by WithIncidentID.
// Returns "" when not set — same shape as the optional incident
// columns elsewhere.
func IncidentIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyIncidentID{}).(string); ok {
		return v
	}
	return ""
}

// writeIMSIAudit persists one row per resolveIMSIs call. POPIA
// audit trail per Phase 3 of docs/axiom/sim-diagnostics-plan.md.
// Best-effort in this commit — Phase 2 promotes audit-write
// failure to a fail-closed HTTP 500 by changing resolveIMSIs's
// public return signature. Until then, audit errors log and the
// lookup proceeds.
//
// incident_id is sourced from the context (Phase D2) so callers
// upstream don't need to thread an extra arg through every layer.
func writeIMSIAudit(ctx context.Context, db *sql.DB, individualID, source, winningPhase string, imsiCount int) error {
	if db == nil || individualID == "" {
		return nil
	}
	incidentID := IncidentIDFromContext(ctx)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := db.ExecContext(ctx,
		`INSERT INTO imsi_lookup_audit (individual_id, source, winning_phase, imsi_count, response_code, incident_id) VALUES (?, ?, ?, ?, ?, ?)`,
		individualID, source, winningPhase, imsiCount, 200, incidentID,
	)
	if err != nil {
		return fmt.Errorf("imsi_lookup_audit insert: %w", err)
	}
	return nil
}

// loadIMSIOverrides reads the operator-configured IMSI list for an
// individual from SQLite. Returns an empty slice when none is set.
// Stored as a single pipe-separated string so we don't need a
// child table for a rarely-edited small payload.
func loadIMSIOverrides(ctx context.Context, db *sql.DB, individualID string) []int64 {
	if db == nil || strings.TrimSpace(individualID) == "" {
		return nil
	}
	var raw string
	err := db.QueryRowContext(ctx,
		`SELECT imsis FROM customer_imsi_overrides WHERE customer_id = ?`,
		individualID,
	).Scan(&raw)
	if err != nil || raw == "" {
		return nil
	}
	parts := strings.Split(raw, "|")
	out := make([]int64, 0, len(parts))
	seen := map[int64]struct{}{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		var v int64
		if _, err := fmt.Sscanf(p, "%d", &v); err != nil || v <= 0 {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// ProductPathDebug is the diagnostic struct the debug endpoint
// renders. Populated by fetchIMSIsViaProductPathDebug, which does
// the same work as fetchIMSIsViaProductPath but returns every
// intermediate so we can see WHERE the join is losing data.
type ProductPathDebug struct {
	BillingAccounts []string          `json:"billing_accounts"`
	ProductCount    int               `json:"product_count"`
	ProductIDs      []string          `json:"product_ids"`
	ResourceRefIDs  []string          `json:"resource_ref_ids"`
	ResourceNames   []string          `json:"resource_names_raw"`
	KeptAsIMSIs     []int64           `json:"kept_as_imsis"`
	PhaseErrors     map[string]string `json:"phase_errors"`
}

// FetchIMSIsViaProductPathDebug is the diagnostic twin of
// fetchIMSIsViaProductPath. Exported so customer_routes.go can call
// it from the /imsi-debug endpoint.
func FetchIMSIsViaProductPathDebug(ctx context.Context, mgr *Manager, connID string, billingAccountIDs []string) ProductPathDebug {
	d := ProductPathDebug{BillingAccounts: billingAccountIDs, PhaseErrors: map[string]string{}}
	if len(billingAccountIDs) == 0 {
		return d
	}
	prodPool, _, err := mgr.PoolByIDWithDB(ctx, connID, "product")
	if err != nil {
		d.PhaseErrors["product_pool"] = err.Error()
		return d
	}
	// Dump the joined product_id + resource_ref_id pairs so we can
	// see both the product count and the join-table hits.
	pRows, err := prodPool.Query(ctx, `
		SELECT p.id, jt.resource_ref_id
		  FROM product.product p
		  LEFT JOIN product.jt_prod_rs_ref jt ON jt.product_id = p.id
		 WHERE p.billing_account_id = ANY($1::text[])
		 LIMIT 200`, billingAccountIDs)
	if err != nil {
		d.PhaseErrors["product_join"] = err.Error()
		return d
	}
	prodIDs := map[string]struct{}{}
	refIDs := map[string]struct{}{}
	for pRows.Next() {
		var pid string
		var rrid *string
		if pRows.Scan(&pid, &rrid) != nil {
			continue
		}
		if pid != "" {
			prodIDs[pid] = struct{}{}
		}
		if rrid != nil && *rrid != "" {
			refIDs[*rrid] = struct{}{}
		}
	}
	pRows.Close()
	for p := range prodIDs {
		d.ProductIDs = append(d.ProductIDs, p)
	}
	d.ProductCount = len(prodIDs)
	for r := range refIDs {
		d.ResourceRefIDs = append(d.ResourceRefIDs, r)
	}
	if len(refIDs) == 0 {
		return d
	}
	svcPool, _, err := mgr.PoolByIDWithDB(ctx, connID, "service")
	if err != nil {
		d.PhaseErrors["service_pool"] = err.Error()
		return d
	}
	refIDList := make([]string, 0, len(refIDs))
	for r := range refIDs {
		refIDList = append(refIDList, r)
	}
	nameRows, err := svcPool.Query(ctx, `
		SELECT name
		  FROM service.resource_ref
		 WHERE id = ANY($1::text[])
		 LIMIT 400`, refIDList)
	if err != nil {
		d.PhaseErrors["resource_ref"] = err.Error()
		return d
	}
	defer nameRows.Close()
	seen := map[int64]struct{}{}
	for nameRows.Next() {
		var name string
		if nameRows.Scan(&name) != nil {
			continue
		}
		d.ResourceNames = append(d.ResourceNames, name)
		digits := keepDigits(name)
		if len(digits) < 14 || len(digits) > 16 {
			continue
		}
		var v int64
		if _, err := fmt.Sscanf(digits, "%d", &v); err != nil || v <= 0 {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		d.KeptAsIMSIs = append(d.KeptAsIMSIs, v)
	}
	return d
}

// fetchIMSIsViaProductPath walks the TMF product → resource join
// path the user confirmed:
//
//   billing_account → product.product
//     → product.jt_prod_rs_ref (product_id, resource_ref_id)
//       → service.resource_ref (name = MSISDN or IMSI)
//
// service and product live in DIFFERENT Axiom databases so we
// fan out in two phases:
//   Phase A (product DB):  find resource_ref_id(s) tied to the
//                          customer's product rows via the join table
//   Phase B (service DB):  look up those ids in service.resource_ref;
//                          the `name` field is the SIM identifier
//                          (MSISDN-formatted, IMSI-formatted, or ICCID
//                          depending on the row)
//
// The names are varchar — some rows carry IMSIs (15-digit 655380…),
// others MSISDNs (27816569886). Whichever they are, the caller
// re-queries the SIM inventory view or `ralf.resource_policy` by
// IMSI, which matches on the bigint column. We filter to digit-only
// names that look like IMSIs (14-16 digits) before using.
func fetchIMSIsViaProductPath(ctx context.Context, mgr *Manager, connID string, billingAccountIDs []string) ([]int64, error) {
	if len(billingAccountIDs) == 0 {
		return nil, nil
	}
	// -- Phase A: product DB --------------------------------
	prodPool, _, err := mgr.PoolByIDWithDB(ctx, connID, "product")
	if err != nil {
		return nil, fmt.Errorf("product pool: %w", err)
	}
	rows, err := prodPool.Query(ctx, `
		SELECT DISTINCT jt.resource_ref_id
		  FROM product.product p
		  JOIN product.jt_prod_rs_ref jt ON jt.product_id = p.id
		 WHERE p.billing_account_id = ANY($1::text[])
		   AND jt.resource_ref_id IS NOT NULL
		 LIMIT 200`, billingAccountIDs)
	if err != nil {
		return nil, fmt.Errorf("product → jt_prod_rs_ref: %w", err)
	}
	refIDs := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil && id != "" {
			refIDs = append(refIDs, id)
		}
	}
	rows.Close()
	if len(refIDs) == 0 {
		return nil, nil
	}

	// -- Phase B: service DB --------------------------------
	svcPool, _, err := mgr.PoolByIDWithDB(ctx, connID, "service")
	if err != nil {
		return nil, fmt.Errorf("service pool: %w", err)
	}
	nameRows, err := svcPool.Query(ctx, `
		SELECT name
		  FROM service.resource_ref
		 WHERE id = ANY($1::text[])
		   AND name IS NOT NULL
		   AND name <> ''
		 LIMIT 200`, refIDs)
	if err != nil {
		return nil, fmt.Errorf("resource_ref: %w", err)
	}
	defer nameRows.Close()
	out := make([]int64, 0, 10)
	seen := map[int64]struct{}{}
	for nameRows.Next() {
		var name string
		if nameRows.Scan(&name) != nil {
			continue
		}
		// Keep digit-only 14-16 character strings — that's the
		// IMSI shape. resource_ref.name can also hold ICCIDs
		// (19-22 digits) or IMEIs (15 digits). We can't tell
		// IMSI vs IMEI by length alone so we accept 14-16 and
		// let ralf.resource_policy's `imsi` filter reject
		// non-matches — cheap no-op join.
		digits := keepDigits(name)
		if len(digits) < 14 || len(digits) > 16 {
			continue
		}
		var v int64
		if _, err := fmt.Sscanf(digits, "%d", &v); err != nil || v <= 0 {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out, nil
}

// fetchIMSIsBySubscriber maps party.individual.id (varchar) to the
// uuid user_id via customer.service_accounts.subscriber, then finds
// every IMSI in vw_service_account_state_latest keyed on that
// user_id. Single CTE-free query so it stays fast even on the
// large service_accounts table (~2M+ rows).
func fetchIMSIsBySubscriber(ctx context.Context, pool *pgxpool.Pool, individualID string) ([]int64, error) {
	if strings.TrimSpace(individualID) == "" {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT v.imsi
		  FROM public.vw_service_account_state_latest v
		  JOIN public.service_accounts sa ON sa.user_id = v.user_id
		 WHERE sa.subscriber = $1
		   AND v.imsi IS NOT NULL
		 LIMIT 50`, individualID)
	if err != nil {
		return nil, fmt.Errorf("service_accounts subscriber pivot: %w", err)
	}
	defer rows.Close()
	var out []int64
	seen := map[int64]struct{}{}
	for rows.Next() {
		var v int64
		if rows.Scan(&v) != nil || v <= 0 {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out, nil
}

// fetchUsageProd pulls the most-recent resource_policy row per IMSI
// the customer's SIMs carry. The table has ~7.8M rows — filtering by
// imsi bigint hits the primary index and returns in <200ms. We still
// surface the MSISDN (isdn) on each row so the UI can show the phone
// number next to the quota / load / policy detail.
func fetchUsageProd(ctx context.Context, pool *pgxpool.Pool, imsis []int64) ([]UsageSnapshot, error) {
	if len(imsis) == 0 {
		return nil, nil
	}
	// DISTINCT ON keeps one row per imsi (the newest).
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT ON (imsi)
		       COALESCE(isdn::text,''),
		       imsi::text,
		       COALESCE(imei::text,''),
		       COALESCE(policy_name,''),
		       COALESCE(quota,''),
		       COALESCE(load,''),
		       COALESCE(quota_status,''),
		       COALESCE(service_name,''),
		       COALESCE(ip_address,''),
		       COALESCE(resource_state,''),
		       updated_at
		  FROM ralf.resource_policy
		 WHERE imsi = ANY($1)
		 ORDER BY imsi, updated_at DESC`, imsis)
	if err != nil {
		return nil, fmt.Errorf("resource_policy: %w", err)
	}
	defer rows.Close()
	var out []UsageSnapshot
	for rows.Next() {
		var u UsageSnapshot
		if err := rows.Scan(&u.MSISDN, &u.IMSI, &u.IMEI, &u.PolicyName,
			&u.Quota, &u.Load, &u.QuotaStatus, &u.ServiceName,
			&u.IPAddress, &u.State, &u.UpdatedAt); err != nil {
			continue
		}
		out = append(out, u)
	}
	return out, nil
}

// ── Helpers ──────────────────────────────────────────────────

func phonesOf(cs []ContactMedium) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		if c.Phone != "" { out = append(out, c.Phone) }
	}
	return out
}

func keepDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' { b.WriteRune(r) }
	}
	return b.String()
}

// ── Derived fields ───────────────────────────────────────────

func sumSuccessfulPayments(ps []Payment) float64 {
	var total float64
	for _, p := range ps {
		if strings.EqualFold(p.Status, "SUCCESS") || strings.EqualFold(p.Status, "SUCCEEDED") || strings.EqualFold(p.Status, "PAID") || strings.EqualFold(p.Status, "COMPLETED") {
			total += p.Amount
		}
	}
	return total
}

func ageOf(t time.Time) AccountAge {
	if t.IsZero() { return AccountAge{} }
	d := int(time.Since(t).Hours() / 24)
	years := d / 365
	months := (d % 365) / 30
	hf := ""
	switch {
	case years > 0 && months > 0:
		hf = fmt.Sprintf("%dy %dmo", years, months)
	case years > 0:
		hf = fmt.Sprintf("%dy", years)
	case months > 0:
		hf = fmt.Sprintf("%dmo", months)
	default:
		hf = fmt.Sprintf("%dd", d)
	}
	return AccountAge{Days: d, HumanFriendly: hf, Since: t}
}

func daysSinceLastPaymentProd(ps []Payment) int {
	if len(ps) == 0 { return -1 }
	latest := ps[0].PaymentDate
	for _, p := range ps[1:] {
		if p.PaymentDate.After(latest) { latest = p.PaymentDate }
	}
	if latest.IsZero() { return -1 }
	return int(time.Since(latest).Hours() / 24)
}

func buildHeatmap(ps []Payment) []int {
	out := make([]int, 30)
	now := time.Now().UTC()
	for _, p := range ps {
		if p.PaymentDate.IsZero() { continue }
		days := int(now.Sub(p.PaymentDate).Hours() / 24)
		if days < 0 || days >= 30 { continue }
		out[29-days]++
	}
	return out
}

func computeRiskScoreProd(c *Customer360) RiskScore {
	score := 0
	reasons := []string{}
	// Failed-payment ratio.
	total, failed := 0, 0
	for _, p := range c.Payments {
		total++
		s := strings.ToUpper(p.Status)
		if s != "SUCCESS" && s != "PAID" && s != "COMPLETED" && s != "SUCCEEDED" {
			failed++
		}
	}
	if total > 0 && failed*100/total > 30 {
		score += 30
		reasons = append(reasons, fmt.Sprintf("%d%% payments failed", failed*100/total))
	}
	// Open promise-to-pay.
	for _, p := range c.Promises {
		if strings.EqualFold(p.Status, "BROKEN") || strings.EqualFold(p.Status, "DEFAULTED") {
			score += 40
			reasons = append(reasons, "broken promise-to-pay")
			break
		}
	}
	// Recent tickets.
	recent := 0
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	for _, t := range c.Tickets {
		if t.CreatedAt.After(cutoff) { recent++ }
	}
	if recent >= 3 {
		score += 15
		reasons = append(reasons, fmt.Sprintf("%d tickets in 30d", recent))
	}
	// Dunning state on any billing account.
	for _, b := range c.BillingAccounts {
		if strings.Contains(strings.ToUpper(b.PaymentStatus), "SUSPEND") ||
			strings.Contains(strings.ToUpper(b.State), "SUSPEND") {
			score += 25
			reasons = append(reasons, "account suspended")
			break
		}
	}
	if score > 100 { score = 100 }
	band := "low"
	switch {
	case score >= 70: band = "high"
	case score >= 30: band = "medium"
	}
	reason := ""
	if len(reasons) > 0 { reason = strings.Join(reasons, " · ") }
	return RiskScore{Value: score, Band: band, Reason: reason}
}

func buildTimelineProd(c *Customer360) []TimelineEvent {
	evs := []TimelineEvent{}
	if !c.Identity.CreatedAt.IsZero() {
		evs = append(evs, TimelineEvent{At: c.Identity.CreatedAt, Type: "created", Label: "Account created"})
	}
	for _, p := range c.Payments {
		t := "payment"
		if s := strings.ToUpper(p.Status); s != "SUCCESS" && s != "PAID" && s != "COMPLETED" && s != "SUCCEEDED" {
			t = "payment_failed"
		}
		evs = append(evs, TimelineEvent{
			At: p.PaymentDate, Type: t,
			Label: fmt.Sprintf("Payment %.2f via %s", p.Amount, p.Channel),
			Detail: p.Status,
		})
	}
	for _, pr := range c.Promises {
		evs = append(evs, TimelineEvent{
			At: pr.ValidFrom, Type: "status_change",
			Label: fmt.Sprintf("Promise to pay %.2f (%d×)", pr.TotalAmount, pr.NumberOfPayments),
			Detail: pr.Status,
		})
	}
	for _, t := range c.Tickets {
		evs = append(evs, TimelineEvent{
			At: t.CreatedAt, Type: "ticket_opened",
			Label: t.Subject,
			Detail: t.Status,
		})
	}
	// newest-first
	for i := range evs {
		for j := i + 1; j < len(evs); j++ {
			if evs[j].At.After(evs[i].At) {
				evs[i], evs[j] = evs[j], evs[i]
			}
		}
	}
	if len(evs) > 40 { evs = evs[:40] }
	return evs
}
