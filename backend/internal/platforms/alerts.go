package platforms

import (
	"fmt"
	"time"
)

// Severity ordering: Info < Warning < Critical < P1.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
	SeverityP1       Severity = "p1"
)

// severityWeight maps a Severity to a sortable integer so the UI
// can order feeds consistently and the incident layer can decide
// whether a new alert should escalate an open incident.
func severityWeight(s Severity) int {
	switch s {
	case SeverityP1:
		return 4
	case SeverityCritical:
		return 3
	case SeverityWarning:
		return 2
	case SeverityInfo:
		return 1
	}
	return 0
}

// Alert is one observation about a service's health. Multiple
// alerts per tick are possible (e.g. latency + SSL at once). The
// sink decides how to deduplicate and whether to escalate.
type Alert struct {
	ID        string    `json:"id"`
	ServiceID string    `json:"service_id"`
	Kind      string    `json:"kind"`     // "consecutive_failures" | "dns_failed" | ...
	Severity  Severity  `json:"severity"`
	Message   string    `json:"message"`
	Cause     string    `json:"cause,omitempty"`
	NextStep  string    `json:"next_step,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// evaluateAlerts compares prev and cur, emits zero or more Alerts.
// Most rules are stateless on the current status; a few inspect the
// streak on cur (already updated by monitor.commit).
func evaluateAlerts(t Target, prev *Status, cur Status, prevOK bool) []Alert {
	out := make([]Alert, 0, 4)
	now := cur.CheckedAt

	axiomRelated := isAxiomRelated(t)

	// ---- Reachability ----
	if cur.State == "down" {
		switch {
		case cur.FailureStreak == 1:
			out = append(out, Alert{
				ServiceID: t.ID, Kind: "first_failure",
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("%s failed a single check", t.Name),
				Cause:    "Transient network blip or single upstream hiccup.",
				NextStep: "Re-check next tick; escalate if streak hits 3.",
				CreatedAt: now,
			})
		case cur.FailureStreak >= 3:
			sev := SeverityCritical
			if axiomRelated {
				sev = SeverityP1
			}
			out = append(out, Alert{
				ServiceID: t.ID, Kind: "consecutive_failures",
				Severity: sev,
				Message:  fmt.Sprintf("%s down (%d consecutive failures)", t.Name, cur.FailureStreak),
				Cause:    rootCauseFor(cur),
				NextStep: nextStepFor(cur, axiomRelated),
				CreatedAt: now,
			})
		}
	}

	// ---- DNS ----
	if !cur.DNS.Resolved && cur.DNS.Error != "" {
		out = append(out, Alert{
			ServiceID: t.ID, Kind: "dns_failed",
			Severity: SeverityCritical,
			Message:  fmt.Sprintf("DNS failed for %s: %s", t.Name, cur.DNS.Error),
			Cause:    "DNS resolution failure.",
			NextStep: "Check DNS, ingress, gateway, host availability.",
			CreatedAt: now,
		})
	}

	// ---- TLS expiry ----
	if cur.TLS.ExpiresAt != (time.Time{}) {
		d := cur.TLS.DaysToExpiry
		switch {
		case d <= 0:
			out = append(out, Alert{
				ServiceID: t.ID, Kind: "tls_expired",
				Severity: SeverityCritical,
				Message:  fmt.Sprintf("%s TLS certificate is EXPIRED", t.Name),
				Cause:    fmt.Sprintf("cert expired on %s", cur.TLS.ExpiresAt.Format("2006-01-02")),
				NextStep: "Renew certificate immediately; browser users already blocked.",
				CreatedAt: now,
			})
		case d <= 7:
			out = append(out, Alert{
				ServiceID: t.ID, Kind: "tls_expiring_soon",
				Severity: SeverityCritical,
				Message:  fmt.Sprintf("%s TLS cert expires in %d day(s)", t.Name, d),
				NextStep: "Escalate to platform team — renew before cutoff.",
				CreatedAt: now,
			})
		case d <= 14:
			out = append(out, Alert{
				ServiceID: t.ID, Kind: "tls_expiring",
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("%s TLS cert expires in %d days", t.Name, d),
				NextStep: "Schedule cert renewal.",
				CreatedAt: now,
			})
		}
	}

	// ---- Content validation (soft failure with HTTP 2xx) ----
	if cur.Content.Checked && (!cur.Content.TitleOK || !cur.Content.BodyOK) && cur.HTTPCode < 400 {
		sev := SeverityCritical
		if !axiomRelated && t.Criticality != CriticalityTop {
			sev = SeverityWarning
		}
		out = append(out, Alert{
			ServiceID: t.ID, Kind: "content_validation_failed",
			Severity: sev,
			Message:  fmt.Sprintf("%s loads but validation failed", t.Name),
			Cause:    cur.Content.Error,
			NextStep: "Check auth provider, content render path, upstream templating.",
			CreatedAt: now,
		})
	}

	// ---- Upstream 502/504 ----
	if cur.HTTPCode == 502 || cur.HTTPCode == 504 {
		out = append(out, Alert{
			ServiceID: t.ID, Kind: "upstream_error",
			Severity: SeverityCritical,
			Message:  fmt.Sprintf("%s returned %d", t.Name, cur.HTTPCode),
			Cause:    "Gateway error — upstream app or dependency failing.",
			NextStep: "Check upstream app health + its DB / auth dependencies.",
			CreatedAt: now,
		})
	}

	// Recovery note — pair with incident-layer auto-mitigate.
	if cur.State == "up" && prev != nil && prev.State != "up" && prevOK {
		out = append(out, Alert{
			ServiceID: t.ID, Kind: "recovered",
			Severity: SeverityInfo,
			Message:  fmt.Sprintf("%s recovered", t.Name),
			CreatedAt: now,
		})
	}

	return out
}

func rootCauseFor(s Status) string {
	switch {
	case !s.DNS.Resolved:
		return "DNS not resolving — host unreachable."
	case s.HTTPCode == 502 || s.HTTPCode == 504:
		return "Upstream gateway error."
	case s.HTTPCode >= 500:
		return fmt.Sprintf("Server error %d.", s.HTTPCode)
	case s.Error != "":
		return s.Error
	}
	return "Service reachability degraded."
}

func nextStepFor(s Status, axiom bool) string {
	if axiom {
		return "Axiom primary DB unreachable — inspect connection pool, DB host, storage, " +
			"replication, and dependent apps (Risk Portal, Assisted Sales)."
	}
	if !s.DNS.Resolved {
		return "Check DNS record, ingress, gateway, host availability."
	}
	if s.HTTPCode >= 500 {
		return "Check upstream app/service dependencies."
	}
	return "Check app health + dependency chain."
}

func isAxiomRelated(t Target) bool {
	// Axiom DB health objects have Group == "database" + ID prefix axiom-;
	// this catches HTTP services whose owner / group identify them as
	// Axiom-dependent too (Risk Portal, Assisted Sales).
	if t.Group == "database" {
		return true
	}
	switch t.ID {
	case "risk-portal", "assisted-sales", "station", "rainmaker-web":
		return true
	}
	return false
}
