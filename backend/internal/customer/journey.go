package customer

import (
	"strings"
	"time"
)

// computeJourneyStage derives a lifecycle marker from customer
// attributes we already have. Decision order mirrors the spec:
//
//   1. Onboarding  — tenure < 14d
//   2. Activation  — tenure < 60d AND has first successful payment
//   3. Recovery    — currently in arrears / suspended / broken PTP
//   4. Friction    — high recent support load OR elevated churn signals
//   5. Retention   — mid-high churn score but no hard failure yet
//   6. Loyalty     — tenure > 365d AND clean signals
//   7. Growth      — default for healthy mid-tenure customers
//
// The result is returned with a short list of triggering_events so the
// UI can render "why Friction?" next to the stage label.
func computeJourneyStage(v *Customer360, pred *Predictions) *JourneyStage {
	if v == nil {
		return nil
	}
	now := time.Now().UTC()
	days := v.AccountAge.Days
	triggers := []string{}

	// Suspended / broken PTP / billing arrears first — these dominate.
	for _, b := range v.BillingAccounts {
		if strings.Contains(strings.ToUpper(b.PaymentStatus), "SUSPEND") ||
			strings.Contains(strings.ToUpper(b.State), "SUSPEND") {
			triggers = append(triggers, "billing account suspended")
			return stage("Recovery", triggers, v.Identity.CreatedAt)
		}
	}
	for _, p := range v.Promises {
		if strings.EqualFold(p.Status, "BROKEN") || strings.EqualFold(p.Status, "DEFAULTED") {
			triggers = append(triggers, "broken promise-to-pay")
			return stage("Recovery", triggers, now)
		}
	}
	if v.DaysSinceLastPayment > 45 {
		triggers = append(triggers, "45+ days since last successful payment")
		return stage("Recovery", triggers, now)
	}

	// Very new customer → Onboarding.
	if days > 0 && days < 14 {
		triggers = append(triggers, "less than 14 days since account created")
		return stage("Onboarding", triggers, v.Identity.CreatedAt)
	}

	// Recent but with first payment → Activation.
	if days >= 14 && days < 60 {
		hasFirstPayment := false
		for _, p := range v.Payments {
			if strings.EqualFold(p.Status, "SUCCESS") || strings.EqualFold(p.Status, "PAID") {
				hasFirstPayment = true
				break
			}
		}
		if hasFirstPayment {
			triggers = append(triggers, "activated within first 60 days")
			return stage("Activation", triggers, now)
		}
		triggers = append(triggers, "no successful payment yet — still activating")
		return stage("Onboarding", triggers, v.Identity.CreatedAt)
	}

	// High churn score → Retention or Friction.
	if pred != nil {
		if pred.Churn30Day >= 0.55 {
			triggers = append(triggers, "high 30-day churn probability")
			return stage("Retention", triggers, now)
		}
		if pred.Churn30Day >= 0.35 {
			triggers = append(triggers, "elevated churn risk")
			return stage("Friction", triggers, now)
		}
	}

	// Ticket cluster in last 30d → Friction.
	cutoff := now.Add(-30 * 24 * time.Hour)
	recent := 0
	for _, t := range v.Tickets {
		if t.CreatedAt.After(cutoff) {
			recent++
		}
	}
	if recent >= 3 {
		triggers = append(triggers, "3+ support tickets in last 30 days")
		return stage("Friction", triggers, now)
	}

	// Long tenure + clean → Loyalty.
	if days >= 365 && (pred == nil || pred.Churn30Day < 0.15) {
		triggers = append(triggers, "over 12 months tenure, low risk")
		return stage("Loyalty", triggers, v.Identity.CreatedAt)
	}

	// Otherwise, healthy mid-tenure → Growth.
	triggers = append(triggers, "healthy mid-tenure customer")
	return stage("Growth", triggers, now)
}

func stage(name string, triggers []string, enteredAt time.Time) *JourneyStage {
	return &JourneyStage{
		Stage:            name,
		EnteredAt:        enteredAt.UTC().Format(time.RFC3339),
		TriggeringEvents: triggers,
	}
}
