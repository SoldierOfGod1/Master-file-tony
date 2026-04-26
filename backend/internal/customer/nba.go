package customer

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

// NBA (Next Best Action) engine. Three phases per spec §3–§6:
//
//   1. Eligibility — which actions are legal/operational/commercial for this customer
//   2. Ranking     — expected-value score per eligible action
//   3. Policy      — apply cooldowns (7 days per action type by default) + cap at 4 recs
//
// The output is a slice of Recommendation objects ready to store +
// render. Rules are authored so reason codes are always human-readable
// — this is the UI's "why am I seeing this?" source of truth.

// ActionDef is the static catalogue entry for one possible action.
// Eligibility predicates and expected-value formulas are attached at
// the struct level so rankRecommendations can iterate uniformly.
type ActionDef struct {
	Type        string  // retention_offer | collections_action | upsell | service_action
	Title       string
	Description string
	Channel     string  // sms | email | call | agent
	BaseValue   float64 // ZAR upside if accepted (rough estimate)
	Cost        float64 // ZAR cost to offer / execute
	Kind        string  // short stable key for cooldown lookup

	// Eligible returns (ok, reasonCodes). When ok=false the action is
	// dropped entirely. When ok=true the reasonCodes are appended to
	// the recommendation's reason_codes so the agent sees why it's
	// ranked here.
	Eligible func(v *Customer360, pred *Predictions) (bool, []string)

	// ExpectedValue computes the ranking score. Higher = show first.
	// Typical formula: BaseValue × predicted-uplift × survival-factor.
	ExpectedValue func(v *Customer360, pred *Predictions) float64
}

// actionCatalogue is the v1 library (spec §6). Keeping the list
// compact + declarative means expanding it later is trivial.
var actionCatalogue = []ActionDef{
	// ---- RETENTION ----
	{
		Type: "retention_offer", Kind: "retention_bonus_data",
		Title: "Offer bonus 10GB retention bundle",
		Description: "One-off bonus to reduce churn risk this month.",
		Channel: "sms", BaseValue: 185.40, Cost: 49.00,
		Eligible: func(v *Customer360, p *Predictions) (bool, []string) {
			if p == nil {
				return false, nil
			}
			if p.Churn30Day < 0.4 {
				return false, nil
			}
			if !hasMSISDN(v) {
				return false, nil
			}
			return true, []string{fmt.Sprintf("churn_30d %.0f%%", p.Churn30Day*100), "has active MSISDN"}
		},
		ExpectedValue: func(v *Customer360, p *Predictions) float64 {
			if p == nil {
				return 0
			}
			return 185.40 * p.Churn30Day * 0.25 // 25% uplift assumption for retention offer
		},
	},
	{
		Type: "retention_offer", Kind: "retention_discount",
		Title: "Discounted bundle upgrade",
		Description: "Next-month discount on upgrade — locks 30-day retention.",
		Channel: "agent", BaseValue: 240.00, Cost: 80.00,
		Eligible: func(v *Customer360, p *Predictions) (bool, []string) {
			if p == nil || p.Churn30Day < 0.35 {
				return false, nil
			}
			if v.LifetimeValue < 1000 {
				return false, nil
			}
			return true, []string{"churn risk + established tenure"}
		},
		ExpectedValue: func(v *Customer360, p *Predictions) float64 {
			if p == nil {
				return 0
			}
			return 240.00 * p.Churn30Day * 0.20
		},
	},
	{
		Type: "retention_offer", Kind: "retention_callback",
		Title: "Schedule retention specialist callback",
		Description: "Book a 1:1 callback with retention team.",
		Channel: "call", BaseValue: 300.00, Cost: 40.00,
		Eligible: func(v *Customer360, p *Predictions) (bool, []string) {
			if p == nil || p.Churn30Day < 0.5 {
				return false, nil
			}
			return true, []string{"very high churn signal — human intervention recommended"}
		},
		ExpectedValue: func(v *Customer360, p *Predictions) float64 {
			if p == nil {
				return 0
			}
			return 300.00 * p.Churn30Day * 0.30
		},
	},
	{
		Type: "retention_offer", Kind: "apology_credit",
		Title: "Proactive apology + service credit",
		Description: "Recent tickets cluster — send apology SMS with a goodwill credit.",
		Channel: "sms", BaseValue: 120.00, Cost: 50.00,
		Eligible: func(v *Customer360, p *Predictions) (bool, []string) {
			// Recent tickets as a driver regardless of churn score.
			cutoff := time.Now().Add(-30 * 24 * time.Hour)
			recent := 0
			for _, t := range v.Tickets {
				if t.CreatedAt.After(cutoff) {
					recent++
				}
			}
			if recent < 2 {
				return false, nil
			}
			if !hasMSISDN(v) {
				return false, nil
			}
			return true, []string{fmt.Sprintf("%d tickets in last 30d", recent)}
		},
		ExpectedValue: func(v *Customer360, p *Predictions) float64 {
			return 120.00 * 0.35
		},
	},

	// ---- COLLECTIONS ----
	{
		Type: "collections_action", Kind: "reminder_sms",
		Title: "Send payment reminder SMS",
		Description: "Balance outstanding — low-friction nudge via SMS.",
		Channel: "sms", BaseValue: 250.00, Cost: 5.00,
		Eligible: func(v *Customer360, p *Predictions) (bool, []string) {
			if p == nil || p.PaymentDefault30d < 0.3 {
				return false, nil
			}
			if !hasMSISDN(v) {
				return false, nil
			}
			return true, []string{fmt.Sprintf("payment default risk %.0f%%", p.PaymentDefault30d*100)}
		},
		ExpectedValue: func(v *Customer360, p *Predictions) float64 {
			if p == nil {
				return 0
			}
			return 250.00 * p.PaymentDefault30d * 0.40
		},
	},
	{
		Type: "collections_action", Kind: "payment_arrangement",
		Title: "Offer payment arrangement",
		Description: "Split the outstanding over 2–3 instalments.",
		Channel: "agent", BaseValue: 400.00, Cost: 30.00,
		Eligible: func(v *Customer360, p *Predictions) (bool, []string) {
			if p == nil || p.PaymentDefault30d < 0.45 {
				return false, nil
			}
			// Don't offer again if a promise is already active or broken.
			for _, pr := range v.Promises {
				if strings.EqualFold(pr.Status, "ACTIVE") || strings.EqualFold(pr.Status, "OPEN") {
					return false, nil
				}
			}
			return true, []string{"high default risk — instalment plan offered"}
		},
		ExpectedValue: func(v *Customer360, p *Predictions) float64 {
			if p == nil {
				return 0
			}
			return 400.00 * p.PaymentDefault30d * 0.50
		},
	},
	{
		Type: "collections_action", Kind: "soft_suspension_warning",
		Title: "Send soft-suspension warning",
		Description: "Warn customer of imminent soft suspension if balance remains.",
		Channel: "sms", BaseValue: 180.00, Cost: 5.00,
		Eligible: func(v *Customer360, p *Predictions) (bool, []string) {
			if p == nil || p.PaymentDefault30d < 0.6 {
				return false, nil
			}
			return true, []string{"severe default risk — final-step warning"}
		},
		ExpectedValue: func(v *Customer360, p *Predictions) float64 {
			if p == nil {
				return 0
			}
			return 180.00 * p.PaymentDefault30d * 0.30
		},
	},

	// ---- GROWTH / UPSELL ----
	{
		Type: "upsell", Kind: "upsell_larger_bundle",
		Title: "Upsell to larger bundle",
		Description: "Usage trending toward quota — bigger bundle fits the pattern.",
		Channel: "sms", BaseValue: 320.00, Cost: 10.00,
		Eligible: func(v *Customer360, p *Predictions) (bool, []string) {
			if p == nil || p.UpsellPropensity < 0.4 {
				return false, nil
			}
			if p.Churn30Day > 0.3 {
				return false, nil // don't upsell an at-risk customer
			}
			if !hasMSISDN(v) {
				return false, nil
			}
			return true, []string{fmt.Sprintf("upsell propensity %.0f%%", p.UpsellPropensity*100), "low churn risk"}
		},
		ExpectedValue: func(v *Customer360, p *Predictions) float64 {
			if p == nil {
				return 0
			}
			return 320.00 * p.UpsellPropensity * 0.20
		},
	},
	{
		Type: "upsell", Kind: "family_plan",
		Title: "Offer family / shared plan",
		Description: "Neighbours on same address — family plan uplift.",
		Channel: "agent", BaseValue: 500.00, Cost: 30.00,
		Eligible: func(v *Customer360, p *Predictions) (bool, []string) {
			if p == nil || p.UpsellPropensity < 0.35 {
				return false, nil
			}
			if len(v.Neighbours) == 0 {
				return false, nil
			}
			return true, []string{fmt.Sprintf("%d same-address residents", len(v.Neighbours))}
		},
		ExpectedValue: func(v *Customer360, p *Predictions) float64 {
			if p == nil {
				return 0
			}
			return 500.00 * p.UpsellPropensity * 0.18
		},
	},
	{
		Type: "upsell", Kind: "auto_pay",
		Title: "Recommend auto-pay enrolment",
		Description: "Reduces future payment friction + churn risk.",
		Channel: "sms", BaseValue: 90.00, Cost: 3.00,
		Eligible: func(v *Customer360, p *Predictions) (bool, []string) {
			// Only when there HAVE been failed payments in the past but
			// the customer isn't currently in deep default.
			failedRatio := paymentFailureRatio(v)
			if failedRatio < 0.1 || failedRatio > 0.5 {
				return false, nil
			}
			if !hasMSISDN(v) {
				return false, nil
			}
			return true, []string{fmt.Sprintf("%.0f%% payment failure history — auto-pay reduces churn", failedRatio*100)}
		},
		ExpectedValue: func(v *Customer360, p *Predictions) float64 {
			return 90.00 * 0.40
		},
	},

	// ---- SERVICE ----
	{
		Type: "service_action", Kind: "reopen_related_ticket",
		Title: "Reopen recent unresolved ticket",
		Description: "Ticket cluster detected — surface oldest unresolved item.",
		Channel: "agent", BaseValue: 70.00, Cost: 15.00,
		Eligible: func(v *Customer360, p *Predictions) (bool, []string) {
			if len(v.Tickets) < 2 {
				return false, nil
			}
			return true, []string{fmt.Sprintf("%d recent tickets — review before new contact", len(v.Tickets))}
		},
		ExpectedValue: func(v *Customer360, p *Predictions) float64 {
			return 70.00 * 0.50
		},
	},
	{
		Type: "service_action", Kind: "fast_track_case",
		Title: "Fast-track this case",
		Description: "High-value customer with open issue — route to senior agent.",
		Channel: "agent", BaseValue: 200.00, Cost: 40.00,
		Eligible: func(v *Customer360, p *Predictions) (bool, []string) {
			if v.LifetimeValue < 5000 {
				return false, nil
			}
			if len(v.Tickets) == 0 {
				return false, nil
			}
			return true, []string{"high-value customer with open tickets"}
		},
		ExpectedValue: func(v *Customer360, p *Predictions) float64 {
			return 200.00 * 0.55
		},
	},
}

// rankRecommendations is the public entry point. It runs all three
// NBA phases, inserts each presented recommendation into the SQLite
// store, and returns the ordered slice the API serialises. Passing
// nil db disables persistence — useful in tests.
func rankRecommendations(ctx context.Context, db *sql.DB, v *Customer360, pred *Predictions, individualID string) []Recommendation {
	if v == nil {
		return nil
	}
	cooldowns := loadCooldowns(ctx, db, individualID)

	// Phase 1 + 2: eligibility + ranking in one pass.
	type scored struct {
		def     ActionDef
		value   float64
		reasons []string
	}
	var candidates []scored
	for _, a := range actionCatalogue {
		if _, blocked := cooldowns[a.Kind]; blocked {
			continue // Phase 3 — cooldown
		}
		ok, reasons := a.Eligible(v, pred)
		if !ok {
			continue
		}
		val := a.ExpectedValue(v, pred)
		if val <= 0 {
			continue
		}
		candidates = append(candidates, scored{a, val, reasons})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].value > candidates[j].value
	})

	// Phase 3 cap: at most 4 recommendations surfaced per lookup.
	maxRecs := 4
	if len(candidates) > maxRecs {
		candidates = candidates[:maxRecs]
	}

	// Materialise + persist.
	now := time.Now().UTC()
	out := make([]Recommendation, 0, len(candidates))
	for i, c := range candidates {
		id := fmt.Sprintf("rec_%s_%d_%d", individualID, now.UnixNano(), i)
		rec := Recommendation{
			ID:             id,
			CustomerID:     individualID,
			Type:           c.def.Type,
			Title:          c.def.Title,
			Description:    c.def.Description,
			Channel:        c.def.Channel,
			PriorityRank:   i + 1,
			ExpectedValue:  round2(c.value),
			CostEstimate:   c.def.Cost,
			ReasonCodes:    c.reasons,
			Status:         "presented",
			CreatedAt:      now.Format(time.RFC3339),
		}
		out = append(out, rec)
		persistRecommendation(ctx, db, rec, c.def.Kind)
	}
	return out
}

// hasMSISDN tells eligibility predicates whether SMS / MSISDN-based
// actions are sensible. We require at least one non-empty phone
// number in contact_medium.
func hasMSISDN(v *Customer360) bool {
	for _, c := range v.Contacts {
		if strings.TrimSpace(c.Phone) != "" {
			return true
		}
	}
	return false
}

func paymentFailureRatio(v *Customer360) float64 {
	var failed, total int
	for _, p := range v.Payments {
		total++
		s := strings.ToUpper(p.Status)
		if s != "SUCCESS" && s != "PAID" && s != "COMPLETED" && s != "SUCCEEDED" {
			failed++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(failed) / float64(total)
}

// loadCooldowns looks up which action kinds have been actioned within
// the last 7 days for this customer — returns a set so Phase 3 can
// skip anything currently in cooldown. Best-effort: on DB error we
// return an empty set (no cooldowns enforced).
func loadCooldowns(ctx context.Context, db *sql.DB, individualID string) map[string]struct{} {
	out := map[string]struct{}{}
	if db == nil || individualID == "" {
		return out
	}
	cutoff := time.Now().Add(-7 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT kind FROM customer_recommendations
		 WHERE customer_id = ?
		   AND status IN ('accepted','dismissed','snoozed')
		   AND created_at >= ?`,
		individualID, cutoff,
	)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		if rows.Scan(&k) == nil {
			out[k] = struct{}{}
		}
	}
	return out
}

func persistRecommendation(ctx context.Context, db *sql.DB, r Recommendation, kind string) {
	if db == nil {
		return
	}
	reasons := strings.Join(r.ReasonCodes, "|")
	_, _ = db.ExecContext(ctx, `
		INSERT INTO customer_recommendations
		  (id, customer_id, type, kind, title, description, channel,
		   priority_rank, expected_value, cost_estimate, reason_codes,
		   status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.CustomerID, r.Type, kind, r.Title, r.Description, r.Channel,
		r.PriorityRank, r.ExpectedValue, r.CostEstimate, reasons,
		r.Status, r.CreatedAt,
	)
}
