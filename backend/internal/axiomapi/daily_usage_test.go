package axiomapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeAxiom returns an httptest.Server that always returns the given body
// at the daily-usage path. Use this so tests don't reach api.sit.rain.co.za.
func fakeAxiom(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/axiom/usage-online/fact-cdr-analytics/daily-usage") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestSummary_FromGuideFixture pins the exact response shape the rain
// Axiom HTTP API returns in production (lifted from a real call to
// daily-usage?msisdn=279609802816656 in April 2026). Numbers must roll
// up to ~1.27 GB total / 29 active days / peak on 2026-04-04 — the
// regression that motivated this fix produced all zeros.
func TestSummary_FromGuideFixture(t *testing.T) {
	body := `{
		"date":["2026-04-01","2026-04-02","2026-04-03","2026-04-04","2026-04-05",
		        "2026-04-06","2026-04-07","2026-04-08","2026-04-09","2026-04-10",
		        "2026-04-11","2026-04-12","2026-04-13","2026-04-14","2026-04-15",
		        "2026-04-16","2026-04-17","2026-04-18","2026-04-19","2026-04-20",
		        "2026-04-21","2026-04-22","2026-04-23","2026-04-24","2026-04-25",
		        "2026-04-26","2026-04-27","2026-04-28","2026-04-29","2026-04-30"],
		"actualUsage":{"GPRS":[4190372,5807717,4131064,302252077,4028945,4421159,
		                       4138690,4690975,5171708,4227743,8004020,298730341,
		                       3530997,9206978,4811268,5522825,3922780,3594688,
		                       3648598,289470558,9508172,3234774,4047793,4152000,
		                       3523622,5232803,4401201,4097741,282786448,0]},
		"events":{"GPRS":[3,5,4,4,4,4,4,4,7,4,4,4,4,4,4,4,4,4,4,4,4,4,6,4,4,4,4,4,5,0]}
	}`
	srv := fakeAxiom(t, body)
	c := NewClient(srv.URL, quietLogger())

	out, _, err := c.Summary(context.Background(), "279609802816656")
	if err != nil {
		t.Fatalf("Summary returned err: %v", err)
	}
	if out.WindowDays != 30 {
		t.Errorf("WindowDays=%d want 30", out.WindowDays)
	}
	if out.FirstDay != "2026-04-01" || out.LastDay != "2026-04-30" {
		t.Errorf("window edges wrong: %s..%s", out.FirstDay, out.LastDay)
	}
	if out.TotalBytes < 1_200_000_000 || out.TotalBytes > 1_300_000_000 {
		t.Errorf("TotalBytes=%d want ~1.27e9 (regression: parser was returning 0)", out.TotalBytes)
	}
	if out.ActiveDays != 29 {
		t.Errorf("ActiveDays=%d want 29 (only 2026-04-30 is zero)", out.ActiveDays)
	}
	if out.PeakDay != "2026-04-04" {
		t.Errorf("PeakDay=%q want 2026-04-04", out.PeakDay)
	}
	if out.PeakDailyBytes != 302252077 {
		t.Errorf("PeakDailyBytes=%d want 302252077", out.PeakDailyBytes)
	}
	if len(out.Series) != 30 {
		t.Errorf("Series len=%d want 30", len(out.Series))
	}
	// avg = total / active_days; loose bound to avoid pinning rounding
	if out.AvgDailyBytes < 40_000_000 || out.AvgDailyBytes > 50_000_000 {
		t.Errorf("AvgDailyBytes=%d want ~43 MB", out.AvgDailyBytes)
	}
}

// TestSummary_EmptyActualUsageStaysZero is the "quiet customer" case —
// the upstream returns the 30-day window but with no traffic. Must
// return zero metrics with the full series, NOT an error.
func TestSummary_EmptyActualUsageStaysZero(t *testing.T) {
	body := `{
		"date":["2026-04-01","2026-04-02","2026-04-03"],
		"actualUsage":{},
		"events":{}
	}`
	srv := fakeAxiom(t, body)
	c := NewClient(srv.URL, quietLogger())

	out, _, err := c.Summary(context.Background(), "279000000000000")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.TotalBytes != 0 || out.ActiveDays != 0 || out.PeakDailyBytes != 0 {
		t.Errorf("quiet customer should be zero, got %+v", out)
	}
	if len(out.Series) != 3 {
		t.Errorf("Series len=%d want 3 (one row per date even when bytes=0)", len(out.Series))
	}
}

// TestSummary_MultipleServiceTypesAccumulate proves we sum across
// service types per day, not pick one and ignore the rest. Future-
// proofs against the upstream adding e.g. SMS / MMS arrays.
func TestSummary_MultipleServiceTypesAccumulate(t *testing.T) {
	body := `{
		"date":["2026-04-01","2026-04-02"],
		"actualUsage":{
			"GPRS":[100,200],
			"MMS": [10, 20]
		},
		"events":{}
	}`
	srv := fakeAxiom(t, body)
	c := NewClient(srv.URL, quietLogger())

	out, _, err := c.Summary(context.Background(), "x")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.TotalBytes != 330 { // 100+200+10+20
		t.Errorf("TotalBytes=%d want 330", out.TotalBytes)
	}
	// Each day's bytes should be the sum across types.
	if out.Series[0].Bytes != 110 || out.Series[1].Bytes != 220 {
		t.Errorf("per-day sum wrong: day0=%d day1=%d (want 110, 220)",
			out.Series[0].Bytes, out.Series[1].Bytes)
	}
	if out.PeakDay != "2026-04-02" || out.PeakDailyBytes != 220 {
		t.Errorf("peak wrong: %s=%d", out.PeakDay, out.PeakDailyBytes)
	}
}

// TestSummary_UnknownActualUsageShape_NoCrash defends against a future
// upstream change that puts a non-array under a service-type key. The
// rogue type should be skipped, not crash the rollup.
func TestSummary_UnknownActualUsageShape_NoCrash(t *testing.T) {
	body := `{
		"date":["2026-04-01","2026-04-02"],
		"actualUsage":{
			"GPRS":[100,200],
			"BROKEN":"this-is-not-an-array"
		},
		"events":{}
	}`
	srv := fakeAxiom(t, body)
	c := NewClient(srv.URL, quietLogger())

	out, _, err := c.Summary(context.Background(), "x")
	if err != nil {
		t.Fatalf("rogue field should not error, got: %v", err)
	}
	if out.TotalBytes != 300 {
		t.Errorf("rogue field should be skipped silently, GPRS still totals 300; got %d", out.TotalBytes)
	}
}

// TestSummary_ArrayShorterThanDate handles the edge case where a
// service-type array has fewer entries than the date list. Must not
// panic; missing entries treated as zero.
func TestSummary_ArrayShorterThanDate(t *testing.T) {
	body := `{
		"date":["2026-04-01","2026-04-02","2026-04-03"],
		"actualUsage":{"GPRS":[100,200]},
		"events":{}
	}`
	srv := fakeAxiom(t, body)
	c := NewClient(srv.URL, quietLogger())

	out, _, err := c.Summary(context.Background(), "x")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.TotalBytes != 300 {
		t.Errorf("TotalBytes=%d want 300", out.TotalBytes)
	}
	if out.Series[2].Bytes != 0 {
		t.Errorf("missing trailing day should be zero, got %d", out.Series[2].Bytes)
	}
}

// TestParseUsageValue_TableDriven pins the three element shapes that
// can appear inside an actualUsage[serviceType] array: plain number,
// {up,down} object, {total} object, and unknown.
func TestParseUsageValue_TableDriven(t *testing.T) {
	tests := []struct {
		name              string
		raw               string
		wantTotal         int64
		wantUp, wantDown  int64
	}{
		{name: "plain int", raw: `12345`, wantTotal: 12345},
		{name: "plain float", raw: `12345.7`, wantTotal: 12345},
		{name: "up/down object", raw: `{"upload":100,"download":200}`,
			wantTotal: 300, wantUp: 100, wantDown: 200},
		{name: "total object", raw: `{"total":999}`, wantTotal: 999},
		{name: "ul/dl shorthand", raw: `{"ul":5,"dl":7}`,
			wantTotal: 12, wantUp: 5, wantDown: 7},
		{name: "unknown shape", raw: `"oops"`, wantTotal: 0},
		{name: "null", raw: `null`, wantTotal: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			total, up, down := parseUsageValue(json.RawMessage(tc.raw))
			if total != tc.wantTotal {
				t.Errorf("total=%d want %d", total, tc.wantTotal)
			}
			if up != tc.wantUp {
				t.Errorf("up=%d want %d", up, tc.wantUp)
			}
			if down != tc.wantDown {
				t.Errorf("down=%d want %d", down, tc.wantDown)
			}
		})
	}
}
