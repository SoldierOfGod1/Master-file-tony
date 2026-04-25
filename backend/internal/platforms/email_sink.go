package platforms

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// EmailSink is an AlertSink that sends an email on every Critical /
// P1 alert. Designed as a thin wrapper that's combined with the
// SQLAlertSink via MultiSink — it never replaces persistence.
//
// Why this exists: the 2026-04-24 Axiom outage produced rows in
// service_alerts and an open Incident in the SQLite incidents table
// — but no email, SMS, or external notification fired because no
// such pipeline existed. Operators not staring at the dashboard
// never knew. This closes that gap.
//
// Configuration (all via env, no config file):
//   RAIN_ALERT_SMTP_HOST  hostname of SMTP server (required)
//   RAIN_ALERT_SMTP_PORT  port (default 587)
//   RAIN_ALERT_SMTP_USER  SMTP username (required)
//   RAIN_ALERT_SMTP_PASS  SMTP password / app token (required)
//   RAIN_ALERT_FROM       From: header (default = SMTP_USER)
//   RAIN_ALERT_TO         comma-separated recipients (required)
//   RAIN_ALERT_MIN_SEVERITY  one of info/warning/critical/p1
//                            (default critical — matches the
//                            existing incident-creation threshold)
//   RAIN_ALERT_DEDUP_MINUTES  suppress duplicate alerts for the
//                             same (service, kind) within this window
//                             (default 30 minutes)
//
// If any required env var is missing, EmailSink is a no-op — safe
// in dev, no email spam from CI runs.
type EmailSink struct {
	log *slog.Logger
	cfg EmailConfig
	// dedupe key: serviceID + kind. Value: last sent time. Wrapped
	// in a small mutex; alert volume is low, no need for a TTL map.
	mu       sync.Mutex
	lastSent map[string]time.Time
}

type EmailConfig struct {
	Host        string
	Port        int
	User        string
	Pass        string
	From        string
	To          []string
	MinSeverity Severity
	DedupWindow time.Duration
	// Sender is injected for testing; nil uses the real SMTP client.
	Sender func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error
}

// NewEmailSinkFromEnv loads config from RAIN_ALERT_* env vars.
// Returns nil + a non-fatal warning when config is incomplete so
// main.go can fall back to SQL-only alerting cleanly.
func NewEmailSinkFromEnv(log *slog.Logger) (*EmailSink, error) {
	cfg, err := loadEmailConfigFromEnv()
	if err != nil {
		return nil, err
	}
	return NewEmailSink(log, cfg), nil
}

// NewEmailSink builds a configured sink. Useful for tests with a
// mock Sender; production code should call NewEmailSinkFromEnv.
func NewEmailSink(log *slog.Logger, cfg EmailConfig) *EmailSink {
	if cfg.DedupWindow == 0 {
		cfg.DedupWindow = 30 * time.Minute
	}
	if cfg.MinSeverity == "" {
		cfg.MinSeverity = SeverityCritical
	}
	return &EmailSink{
		log:      log,
		cfg:      cfg,
		lastSent: make(map[string]time.Time),
	}
}

// Emit implements AlertSink. Filters by severity, dedupes within
// the configured window, then sends.
func (e *EmailSink) Emit(ctx context.Context, a Alert, prev *Status, cur Status) {
	if e == nil {
		return
	}
	if severityWeight(a.Severity) < severityWeight(e.cfg.MinSeverity) {
		return
	}
	if !e.shouldSend(a) {
		return
	}
	subject := fmt.Sprintf("[%s] %s — %s", strings.ToUpper(string(a.Severity)), a.ServiceID, a.Message)
	body := buildEmailBody(a, prev, cur)
	if err := e.send(subject, body); err != nil {
		e.log.Warn("email alert send failed",
			"error", err, "service", a.ServiceID, "kind", a.Kind, "severity", a.Severity)
		return
	}
	e.log.Info("email alert sent",
		"to", e.cfg.To, "service", a.ServiceID, "kind", a.Kind, "severity", a.Severity)
}

// shouldSend gates duplicate alerts within the dedup window.
// Matches by (service_id, kind) — same kind on the same service
// firing twice in quick succession is almost always the same root
// cause and would just spam the inbox.
func (e *EmailSink) shouldSend(a Alert) bool {
	key := a.ServiceID + "|" + a.Kind
	e.mu.Lock()
	defer e.mu.Unlock()
	last, ok := e.lastSent[key]
	if ok && time.Since(last) < e.cfg.DedupWindow {
		return false
	}
	e.lastSent[key] = time.Now()
	return true
}

func (e *EmailSink) send(subject, body string) error {
	from := e.cfg.From
	if from == "" {
		from = e.cfg.User
	}
	addr := net.JoinHostPort(e.cfg.Host, strconv.Itoa(e.cfg.Port))
	auth := smtp.PlainAuth("", e.cfg.User, e.cfg.Pass, e.cfg.Host)
	headers := []string{
		"From: " + from,
		"To: " + strings.Join(e.cfg.To, ", "),
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
	}
	msg := []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + body)
	send := e.cfg.Sender
	if send == nil {
		send = smtp.SendMail
	}
	return send(addr, auth, from, e.cfg.To, msg)
}

// buildEmailBody renders a plain-text alert email. Lead with the
// punchline, follow with cause + next step, end with raw status
// detail so an operator can triage from the email alone without
// reaching for a laptop.
func buildEmailBody(a Alert, prev *Status, cur Status) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", a.Message)
	fmt.Fprintf(&b, "Severity      : %s\n", strings.ToUpper(string(a.Severity)))
	fmt.Fprintf(&b, "Service       : %s\n", a.ServiceID)
	fmt.Fprintf(&b, "Kind          : %s\n", a.Kind)
	fmt.Fprintf(&b, "Created       : %s\n", a.CreatedAt.Format(time.RFC3339))
	if a.Cause != "" {
		fmt.Fprintf(&b, "Cause         : %s\n", a.Cause)
	}
	if a.NextStep != "" {
		fmt.Fprintf(&b, "Next step     : %s\n", a.NextStep)
	}
	b.WriteString("\n--- Current status ---\n")
	fmt.Fprintf(&b, "State         : %s\n", cur.State)
	if cur.HTTPCode > 0 {
		fmt.Fprintf(&b, "HTTP code     : %d\n", cur.HTTPCode)
	}
	fmt.Fprintf(&b, "Latency (ms)  : %d\n", cur.LatencyMS)
	fmt.Fprintf(&b, "Failure streak: %d\n", cur.FailureStreak)
	if cur.Error != "" {
		fmt.Fprintf(&b, "Error         : %s\n", cur.Error)
	}
	if prev != nil {
		fmt.Fprintf(&b, "\nPrevious state: %s (latency %d ms)\n", prev.State, prev.LatencyMS)
	}
	b.WriteString("\n— Soldier of God platform monitor")
	return b.String()
}

func loadEmailConfigFromEnv() (EmailConfig, error) {
	cfg := EmailConfig{
		Host: strings.TrimSpace(os.Getenv("RAIN_ALERT_SMTP_HOST")),
		User: strings.TrimSpace(os.Getenv("RAIN_ALERT_SMTP_USER")),
		Pass: os.Getenv("RAIN_ALERT_SMTP_PASS"),
		From: strings.TrimSpace(os.Getenv("RAIN_ALERT_FROM")),
	}
	to := strings.TrimSpace(os.Getenv("RAIN_ALERT_TO"))
	if cfg.Host == "" || cfg.User == "" || cfg.Pass == "" || to == "" {
		return EmailConfig{}, errors.New("email alerts disabled: set RAIN_ALERT_SMTP_HOST, _USER, _PASS, _TO")
	}
	for _, addr := range strings.Split(to, ",") {
		if s := strings.TrimSpace(addr); s != "" {
			cfg.To = append(cfg.To, s)
		}
	}
	port := 587
	if v := strings.TrimSpace(os.Getenv("RAIN_ALERT_SMTP_PORT")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n < 65536 {
			port = n
		}
	}
	cfg.Port = port
	cfg.MinSeverity = SeverityCritical
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("RAIN_ALERT_MIN_SEVERITY"))); v != "" {
		switch v {
		case "info", "warning", "critical", "p1":
			cfg.MinSeverity = Severity(v)
		}
	}
	cfg.DedupWindow = 30 * time.Minute
	if v := strings.TrimSpace(os.Getenv("RAIN_ALERT_DEDUP_MINUTES")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.DedupWindow = time.Duration(n) * time.Minute
		}
	}
	return cfg, nil
}

// MultiSink fans an emit to several sinks. Used so SQL persistence
// + email send can both run on every tick without either one
// caring about the other.
type MultiSink struct {
	sinks []AlertSink
}

func NewMultiSink(sinks ...AlertSink) *MultiSink {
	out := &MultiSink{}
	for _, s := range sinks {
		if s == nil {
			continue
		}
		out.sinks = append(out.sinks, s)
	}
	return out
}

func (m *MultiSink) Emit(ctx context.Context, a Alert, prev *Status, cur Status) {
	if m == nil {
		return
	}
	for _, s := range m.sinks {
		// Each sink runs synchronously on the same goroutine the
		// monitor uses. SMTP can take a couple of seconds; if that
		// becomes a problem in production the wrapper here is
		// where we'd add a buffered worker. Not premature for v1.
		s.Emit(ctx, a, prev, cur)
	}
}

// suppressTLSVerify is here as a placeholder so the import of
// crypto/tls isn't dropped by goimports. SMTP servers with self-
// signed certs aren't supported in the v1 path; revisit if rain's
// internal SMTP needs it.
var _ = (*tls.Config)(nil)
