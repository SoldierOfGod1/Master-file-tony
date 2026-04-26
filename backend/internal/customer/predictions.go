package customer

import (
	"math"
	"strings"
	"time"
)

// computePredictions is v1 rules-based scoring. It produces shapes
// compatible with a future ML upgrade (xgboost churn, GBM LTV,
// uplift RF for NBA) so the UI + storage contracts don't change
// when models arrive. Every score carries human-readable reason
// codes so the Explainability panel has honest content.
//
// Inputs are the already-populated Customer360 fields — no
// additional DB calls. This keeps the extra work cheap (no new
// queries against Axiom, no new replication-lag risk).
func computePredictions(v *Customer360) *Predictions {
	if v == nil {
		return nil
	}
	now := time.Now().UTC()

	// -------- churn signals --------
	var (
		churnRisk   float64
		reasons     []string
		failedCount int
		totalCount  int
	)

	// 1) Payment failure ratio over the last N payments.
	for _, p := range v.Payments {
		totalCount++
		s := strings.ToUpper(p.Status)
		if s != "SUCCESS" && s != "PAID" && s != "COMPLETED" && s != "SUCCEEDED" {
			failedCount++
		}
	}
	if totalCount > 0 {
		pct := float64(failedCount) / float64(totalCount)
		if pct >= 0.5 {
			churnRisk += 0.35
			reasons = append(reasons, "over 50% payment failure ratio")
		} else if pct >= 0.3 {
			churnRisk += 0.20
			reasons = append(reasons, "elevated payment failure ratio")
		}
	}

	// 2) Days since last successful payment.
	daysSincePayment := v.DaysSinceLastPayment
	switch {
	case daysSincePayment > 60:
		churnRisk += 0.30
		reasons = append(reasons, "no successful payment in 60+ days")
	case daysSincePayment > 30:
		churnRisk += 0.15
		reasons = append(reasons, "no successful payment in 30+ days")
	}

	// 3) Broken promise-to-pay.
	for _, p := range v.Promises {
		if strings.EqualFold(p.Status, "BROKEN") || strings.EqualFold(p.Status, "DEFAULTED") {
			churnRisk += 0.25
			reasons = append(reasons, "broken promise-to-pay")
			break
		}
	}

	// 4) Ticket cluster in the last 30 days.
	recentTickets := 0
	cutoff := now.Add(-30 * 24 * time.Hour)
	for _, t := range v.Tickets {
		if t.CreatedAt.After(cutoff) {
			recentTickets++
		}
	}
	if recentTickets >= 3 {
		churnRisk += 0.15
		reasons = append(reasons, "3+ tickets in last 30 days")
	}

	// 5) Suspended account state.
	for _, b := range v.BillingAccounts {
		if strings.Contains(strings.ToUpper(b.PaymentStatus), "SUSPEND") ||
			strings.Contains(strings.ToUpper(b.State), "SUSPEND") {
			churnRisk += 0.20
			reasons = append(reasons, "billing account suspended")
			break
		}
	}

	// 6) Usage drop / no usage at all.
	if len(v.CDRUsage) == 0 && len(v.Usage) == 0 {
		churnRisk += 0.10
		reasons = append(reasons, "no recent usage data")
	}

	churn30 := clamp01(churnRisk)
	// 60/90 day horizons: monotonically higher than 30-day. Rule: add
	// 8pp / 15pp on top unless already near ceiling.
	churn60 := clamp01(churn30 + 0.08)
	churn90 := clamp01(churn30 + 0.15)

	// -------- payment default (subset of churn signals, collections-focused) --------
	paymentDefault := 0.0
	if totalCount > 0 {
		paymentDefault += float64(failedCount) / float64(totalCount) * 0.5
	}
	for _, p := range v.Promises {
		if strings.EqualFold(p.Status, "BROKEN") {
			paymentDefault += 0.30
			break
		}
	}
	// Outstanding balance as fraction of last-invoice amount.
	for _, b := range v.Balances {
		if b.LastInvoiceAmount > 0 && b.Amount < 0 {
			owed := -b.Amount
			if ratio := owed / b.LastInvoiceAmount; ratio > 1.5 {
				paymentDefault += 0.20
			} else if ratio > 0.9 {
				paymentDefault += 0.10
			}
			break
		}
	}
	if daysSincePayment > 45 {
		paymentDefault += 0.15
	}
	paymentDefault = clamp01(paymentDefault)

	// -------- LTV (12-month expected) --------
	// Approach: avg monthly revenue × 12 × survival probability.
	last3 := sumPaymentsSince(v.Payments, now.Add(-90*24*time.Hour))
	monthly := last3 / 3.0
	survival := 1.0 - (0.7*churn30 + 0.3*churn90)
	if survival < 0.05 {
		survival = 0.05
	}
	ltv12m := monthly * 12.0 * survival

	// -------- upsell propensity --------
	upsell := 0.0
	// Baseline: if churn is low AND payments are clean, upsell is plausible.
	if churn30 < 0.25 && failedCount == 0 && totalCount >= 3 {
		upsell += 0.35
		reasons = append(reasons, "low risk + clean payment history")
	}
	// Has products but no upgrades / no unlimited plan? proxy via usage hitting quota.
	for _, u := range v.Usage {
		if strings.Contains(strings.ToLower(u.QuotaStatus), "near") ||
			strings.Contains(strings.ToLower(u.QuotaStatus), "depleted") {
			upsell += 0.20
			reasons = append(reasons, "usage at or near quota — upgrade candidate")
			break
		}
	}
	// Good tenure signals stability.
	if v.AccountAge.Days > 365 {
		upsell += 0.10
	}
	upsell = clamp01(upsell)

	// Deduplicate reasons while preserving order.
	reasons = dedupeStrings(reasons)

	return &Predictions{
		Churn30Day:        round2(churn30),
		Churn60Day:        round2(churn60),
		Churn90Day:        round2(churn90),
		PaymentDefault30d: round2(paymentDefault),
		LTV12mExpected:    round2(ltv12m),
		UpsellPropensity:  round2(upsell),
		Confidence:        0.6, // rules get a fixed confidence; ML will replace
		ReasonCodes:       reasons,
		ModelVersion:      "rules_v1",
		ComputedAt:        now.Format(time.RFC3339),
	}
}

func sumPaymentsSince(payments []Payment, since time.Time) float64 {
	var total float64
	for _, p := range payments {
		if p.PaymentDate.Before(since) {
			continue
		}
		s := strings.ToUpper(p.Status)
		if s == "SUCCESS" || s == "PAID" || s == "COMPLETED" || s == "SUCCEEDED" {
			total += p.Amount
		}
	}
	return total
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func dedupeStrings(ss []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
