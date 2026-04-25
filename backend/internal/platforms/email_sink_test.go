package platforms

import (
	"context"
	"io"
	"log/slog"
	"net/smtp"
	"strings"
	"sync"
	"testing"
	"time"
)

// captureSender intercepts smtp.SendMail-style calls so tests can
// assert on subject/body without standing up a real SMTP server.
type captureSender struct {
	mu    sync.Mutex
	calls []capturedMail
}

type capturedMail struct {
	addr string
	from string
	to   []string
	msg  string
}

func (c *captureSender) Send(addr string, _ smtp.Auth, from string, to []string, msg []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, capturedMail{addr: addr, from: from, to: append([]string(nil), to...), msg: string(msg)})
	return nil
}

func newQuietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func makeAlert(sev Severity, kind string) Alert {
	return Alert{
		ServiceID: "axiom-prod",
		Kind:      kind,
		Severity:  sev,
		Message:   "axiom-prod down (3 consecutive failures)",
		Cause:     "DB host unreachable.",
		NextStep:  "Inspect connection pool.",
		CreatedAt: time.Date(2026, 4, 24, 14, 0, 0, 0, time.UTC),
	}
}

func makeStatus(state string) Status {
	return Status{
		State:         state,
		LatencyMS:     150,
		HTTPCode:      0,
		FailureStreak: 3,
		CheckedAt:     time.Date(2026, 4, 24, 14, 0, 0, 0, time.UTC),
		Error:         "dial tcp: connect: connection refused",
	}
}

func TestEmailSink_SendsOnCritical(t *testing.T) {
	cap := &captureSender{}
	sink := NewEmailSink(newQuietLogger(), EmailConfig{
		Host: "smtp.example.com", Port: 587,
		User: "alerts@example.com", Pass: "x",
		From: "alerts@example.com",
		To:   []string{"ops@example.com"},
		MinSeverity: SeverityCritical,
		DedupWindow: time.Hour,
		Sender:      cap.Send,
	})
	sink.Emit(context.Background(), makeAlert(SeverityCritical, "consecutive_failures"), nil, makeStatus("down"))
	if len(cap.calls) != 1 {
		t.Fatalf("expected 1 send, got %d", len(cap.calls))
	}
	got := cap.calls[0]
	if got.from != "alerts@example.com" {
		t.Errorf("From mismatch: got %q", got.from)
	}
	if !contains(got.to, "ops@example.com") {
		t.Errorf("To missing recipient: got %v", got.to)
	}
	if !strings.Contains(got.msg, "Subject: [CRITICAL] axiom-prod") {
		t.Errorf("subject missing or wrong: %q", got.msg)
	}
	if !strings.Contains(got.msg, "DB host unreachable.") {
		t.Errorf("body missing cause: %q", got.msg)
	}
	if !strings.Contains(got.msg, "Failure streak: 3") {
		t.Errorf("body missing failure streak: %q", got.msg)
	}
}

func TestEmailSink_FiltersBelowMinSeverity(t *testing.T) {
	cap := &captureSender{}
	sink := NewEmailSink(newQuietLogger(), EmailConfig{
		Host: "h", Port: 587, User: "u", Pass: "p",
		From: "u", To: []string{"r"},
		MinSeverity: SeverityCritical,
		Sender:      cap.Send,
	})
	sink.Emit(context.Background(), makeAlert(SeverityWarning, "first_failure"), nil, makeStatus("down"))
	sink.Emit(context.Background(), makeAlert(SeverityInfo, "recovered"), nil, makeStatus("up"))
	if len(cap.calls) != 0 {
		t.Errorf("expected zero sends below threshold, got %d", len(cap.calls))
	}
}

func TestEmailSink_DedupesWithinWindow(t *testing.T) {
	cap := &captureSender{}
	sink := NewEmailSink(newQuietLogger(), EmailConfig{
		Host: "h", Port: 587, User: "u", Pass: "p",
		From: "u", To: []string{"r"},
		MinSeverity: SeverityCritical,
		DedupWindow: 30 * time.Minute,
		Sender:      cap.Send,
	})
	a := makeAlert(SeverityCritical, "consecutive_failures")
	sink.Emit(context.Background(), a, nil, makeStatus("down"))
	sink.Emit(context.Background(), a, nil, makeStatus("down"))
	sink.Emit(context.Background(), a, nil, makeStatus("down"))
	if len(cap.calls) != 1 {
		t.Errorf("expected 1 send (deduped), got %d", len(cap.calls))
	}
	// Different kind on the same service should NOT dedupe.
	other := makeAlert(SeverityCritical, "tls_expiring_soon")
	sink.Emit(context.Background(), other, nil, makeStatus("up"))
	if len(cap.calls) != 2 {
		t.Errorf("expected 2 sends after different-kind alert, got %d", len(cap.calls))
	}
}

func TestEmailSink_NilIsNoOp(t *testing.T) {
	var sink *EmailSink
	// Must not panic when env vars are missing and main.go falls
	// back to nil — tested explicitly because sink.Emit gets a
	// pointer receiver and must guard.
	sink.Emit(context.Background(), makeAlert(SeverityP1, "x"), nil, makeStatus("down"))
}

func TestMultiSink_FansOut(t *testing.T) {
	captured := []*captureSinkInner{{}, {}}
	multi := NewMultiSink(captured[0], captured[1])
	multi.Emit(context.Background(), makeAlert(SeverityCritical, "k"), nil, makeStatus("down"))
	for i, c := range captured {
		if c.count != 1 {
			t.Errorf("inner sink %d: expected 1 emit, got %d", i, c.count)
		}
	}
}

func TestMultiSink_NilSinkIgnored(t *testing.T) {
	c := &captureSinkInner{}
	multi := NewMultiSink(nil, c, nil)
	multi.Emit(context.Background(), makeAlert(SeverityCritical, "k"), nil, makeStatus("down"))
	if c.count != 1 {
		t.Errorf("expected 1 emit, got %d", c.count)
	}
}

type captureSinkInner struct{ count int }

func (c *captureSinkInner) Emit(_ context.Context, _ Alert, _ *Status, _ Status) { c.count++ }

func TestLoadEmailConfigFromEnv_MissingFails(t *testing.T) {
	t.Setenv("RAIN_ALERT_SMTP_HOST", "")
	t.Setenv("RAIN_ALERT_SMTP_USER", "")
	t.Setenv("RAIN_ALERT_SMTP_PASS", "")
	t.Setenv("RAIN_ALERT_TO", "")
	if _, err := loadEmailConfigFromEnv(); err == nil {
		t.Fatal("expected error for missing env vars")
	}
}

func TestLoadEmailConfigFromEnv_HappyPath(t *testing.T) {
	t.Setenv("RAIN_ALERT_SMTP_HOST", "smtp.example.com")
	t.Setenv("RAIN_ALERT_SMTP_USER", "alerts@example.com")
	t.Setenv("RAIN_ALERT_SMTP_PASS", "secret")
	t.Setenv("RAIN_ALERT_TO", "ops@example.com, second@example.com")
	t.Setenv("RAIN_ALERT_FROM", "alerts@example.com")
	t.Setenv("RAIN_ALERT_MIN_SEVERITY", "warning")
	t.Setenv("RAIN_ALERT_DEDUP_MINUTES", "15")
	cfg, err := loadEmailConfigFromEnv()
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if cfg.Host != "smtp.example.com" || cfg.Port != 587 {
		t.Errorf("host/port wrong: %+v", cfg)
	}
	if len(cfg.To) != 2 || cfg.To[1] != "second@example.com" {
		t.Errorf("To list wrong: %v", cfg.To)
	}
	if cfg.MinSeverity != SeverityWarning {
		t.Errorf("min severity wrong: %v", cfg.MinSeverity)
	}
	if cfg.DedupWindow != 15*time.Minute {
		t.Errorf("dedup window wrong: %v", cfg.DedupWindow)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
