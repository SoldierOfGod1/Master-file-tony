// Package axiomapi is a thin client over the rain Axiom HTTP API
// hosted at api.sit.rain.co.za. It's deliberately small — one
// endpoint per file, all read-only, all rate-limited.
//
// Why a separate package from `customer/` and `darknoc/`: the Axiom
// HTTP API is a different upstream from both Postgres BSS and
// ClickHouse telemetry. Slot it next to them, not inside them, so
// the surface stays tidy.
package axiomapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/SoldierOfGod1/command-centre/internal/ratelimit"
)

// Client is the operator-configured Axiom HTTP API endpoint. Only
// one base URL today, but the struct is shaped to take an optional
// bearer token (most rain SIT APIs are open from the VPN; if/when
// auth is added we drop the token in here without touching callers).
type Client struct {
	baseURL string
	hc      *http.Client
	log     *slog.Logger
	token   string

	// Rate-gate every outbound call. Defaults to 5 rps with a 10
	// burst — Cybertron / Customer 360 fanning out to multiple
	// MSISDNs in a session shouldn't exceed this; a runaway loop
	// gets throttled instead of taking the upstream down.
	limiter *ratelimit.Limiter
}

// NewClient wires the client to its base URL. host should be the
// scheme+host (e.g. "https://api.sit.rain.co.za") with no trailing
// slash; the package adds the path.
func NewClient(baseURL string, log *slog.Logger) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		hc:      &http.Client{Timeout: 10 * time.Second},
		log:     log.With("component", "axiomapi"),
		limiter: ratelimit.New("axiom-api", 10, 5),
	}
}

// SetToken attaches a bearer token (Authorization: Bearer ...) to
// every request. Nil-safe — empty token means no header is sent.
func (c *Client) SetToken(token string) {
	if c == nil {
		return
	}
	c.token = strings.TrimSpace(token)
}

// SetRateLimit overrides the default 5 rps / 10 burst.
func (c *Client) SetRateLimit(burst int, perSecond float64) {
	if c == nil {
		return
	}
	c.limiter = ratelimit.New("axiom-api", burst, perSecond)
}

// Limiter exposes the limiter for admin / observability surfaces.
func (c *Client) Limiter() *ratelimit.Limiter {
	if c == nil {
		return nil
	}
	return c.limiter
}

// DailyUsageResponse mirrors the actual rain Axiom HTTP API shape
// observed at api.sit.rain.co.za. Three parallel fields:
//
//	date        — sorted array of "YYYY-MM-DD" strings (30-day window)
//	actualUsage — keyed by SERVICE TYPE (e.g. "GPRS", "MMS"). Each value
//	              is an array of bytes parallel to `date` — element i
//	              corresponds to date[i]. NOT a date-keyed dict.
//	              Empty {} means no traffic in window — quiet customer,
//	              not an error.
//	events      — same shape as actualUsage; non-data events.
type DailyUsageResponse struct {
	Date        []string                   `json:"date"`
	ActualUsage map[string]json.RawMessage `json:"actualUsage"`
	Events      map[string]json.RawMessage `json:"events"`
}

// DayUsage is one parsed row of the series — a date with byte total.
// Up/Down populated when the upstream splits by direction.
type DayUsage struct {
	Date  string `json:"date"`
	Bytes int64  `json:"bytes"`
	Up    int64  `json:"up,omitempty"`
	Down  int64  `json:"down,omitempty"`
}

// UsageSummary is the 4-KPI bundle the Customer 360 "Usage Overview"
// tile renders: Total / Avg-per-active-day / Active days / Peak day.
// Series carries the full per-day breakdown so the trend chart below
// the tile uses the same fetch instead of round-tripping twice.
//
// Source identifies which upstream the numbers came from. Today the
// route can dispatch to either the rain Axiom HTTP API or the GaussDB
// fact-CDR table — the chip on the UI tile reads this field so the
// operator knows what they're looking at without flipping a setting.
type UsageSummary struct {
	MSISDN         string     `json:"msisdn"`
	Source         string     `json:"source,omitempty"` // "axiom-api" | "gaussdb"
	WindowDays     int        `json:"window_days"`
	FirstDay       string     `json:"first_day,omitempty"`
	LastDay        string     `json:"last_day,omitempty"`
	TotalBytes     int64      `json:"total_bytes"`
	AvgDailyBytes  int64      `json:"avg_daily_bytes"`
	PeakDailyBytes int64      `json:"peak_daily_bytes"`
	PeakDay        string     `json:"peak_day,omitempty"`
	ActiveDays     int        `json:"active_days"`
	Series         []DayUsage `json:"series"`
}

// DailyUsageRow is the legacy shape the original /usage/daily route
// returns. Kept so any caller still parsing the old `rows` array
// keeps working — the new summary route is the better surface.
type DailyUsageRow struct {
	Day        string         `json:"day,omitempty"`
	MSISDN     string         `json:"msisdn,omitempty"`
	BytesIn    int64          `json:"bytes_in,omitempty"`
	BytesOut   int64          `json:"bytes_out,omitempty"`
	SecondsIn  int64          `json:"seconds_in,omitempty"`
	SecondsOut int64          `json:"seconds_out,omitempty"`
	SMSCount   int64          `json:"sms_count,omitempty"`
	Extra      map[string]any `json:"extra,omitempty"`
}

// DailyUsage hits
//   {baseURL}/axiom/usage-online/fact-cdr-analytics/daily-usage?msisdn=X
// and returns the parsed JSON array. The upstream curl shape:
//
//	curl -sS \
//	  -w "\n\n--- HTTP %{http_code} | time=%{time_total}s ---\n" \
//	  -H "Accept: */*" \
//	  "https://api.sit.rain.co.za/axiom/usage-online/fact-cdr-analytics/daily-usage?msisdn=$MSISDN"
//
// Returns the raw JSON-decoded body when the upstream shape doesn't
// match our DailyUsageRow expectations — caller can inspect either.
func (c *Client) DailyUsage(ctx context.Context, msisdn string) ([]DailyUsageRow, []byte, error) {
	if c == nil || c.baseURL == "" {
		return nil, nil, errors.New("axiomapi: client not configured (set AXIOM_API_BASE_URL)")
	}
	msisdn = strings.TrimSpace(msisdn)
	if msisdn == "" {
		return nil, nil, errors.New("msisdn required")
	}

	if c.limiter != nil {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, nil, fmt.Errorf("axiom-api rate-limited: %w", err)
		}
	}

	q := url.Values{}
	q.Set("msisdn", msisdn)
	endpoint := c.baseURL + "/axiom/usage-online/fact-cdr-analytics/daily-usage?" + q.Encode()

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "*/*")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	start := time.Now()
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("axiom-api: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	c.log.Info("axiom-api daily-usage",
		"msisdn_len", len(msisdn), "http", resp.StatusCode,
		"latency_ms", time.Since(start).Milliseconds(),
		"bytes", len(body))

	if resp.StatusCode != http.StatusOK {
		return nil, body, fmt.Errorf("axiom-api %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	// Try array shape first. If it isn't an array, return the raw
	// body and let the caller decide.
	var rows []DailyUsageRow
	if err := json.Unmarshal(body, &rows); err == nil {
		return rows, body, nil
	}
	return nil, body, nil
}

// Summary calls DailyUsage and computes the 4-KPI rollup the Customer
// 360 Usage Overview tile renders. Robust to three observed upstream
// shapes for actualUsage values:
//
//  1. Raw number per day:                {"2026-04-01": 123456789}
//  2. Object with direction split:       {"2026-04-01": {"upload":N,"download":M}}
//  3. Object with total field:           {"2026-04-01": {"total": N}}
//
// Whatever the shape, we extract a per-day Bytes total and compute:
//
//	Total          = sum of Bytes across the series
//	Active Days    = count of days where Bytes > 0
//	Avg Daily      = Total / Active Days  (zero when no active days —
//	                 avoids div-by-zero showing as NaN in the UI)
//	Peak Daily     = max Bytes across the series, with the date
//
// Empty actualUsage returns a zero-valued summary with the correct
// window — UI tile shows 0 GB / 0 mB / 0 days / 0 GB which is the
// honest answer for a quiet customer.
func (c *Client) Summary(ctx context.Context, msisdn string) (UsageSummary, []byte, error) {
	out := UsageSummary{MSISDN: msisdn, Source: "axiom-api", Series: []DayUsage{}}
	_, raw, err := c.DailyUsage(ctx, msisdn)
	if err != nil {
		return out, raw, err
	}
	if len(raw) == 0 {
		return out, raw, nil
	}

	var resp DailyUsageResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		// Upstream returned a shape we don't recognise — leave
		// summary zero and let the caller inspect raw.
		return out, raw, nil
	}
	out.WindowDays = len(resp.Date)
	if out.WindowDays > 0 {
		out.FirstDay = resp.Date[0]
		out.LastDay = resp.Date[out.WindowDays-1]
	}

	// actualUsage is keyed by service type ("GPRS", etc.) and each
	// value is an ARRAY of bytes parallel to resp.Date. Walk every
	// service type's array, sum into per-index buckets, then collapse
	// into the per-day series. The previous implementation indexed by
	// date string and silently produced zeros for every customer.
	dailyBytes := make([]int64, out.WindowDays)
	dailyUp := make([]int64, out.WindowDays)
	dailyDown := make([]int64, out.WindowDays)

	for _, raw := range resp.ActualUsage {
		var arr []json.RawMessage
		if err := json.Unmarshal(raw, &arr); err != nil {
			// Unexpected non-array shape for this service type — skip
			// so a single rogue field can't zero out the whole rollup.
			continue
		}
		n := len(arr)
		if n > out.WindowDays {
			n = out.WindowDays
		}
		for i := 0; i < n; i++ {
			total, up, down := parseUsageValue(arr[i])
			dailyBytes[i] += total
			dailyUp[i] += up
			dailyDown[i] += down
		}
	}

	out.Series = make([]DayUsage, 0, out.WindowDays)
	for i, day := range resp.Date {
		du := DayUsage{
			Date:  day,
			Bytes: dailyBytes[i],
			Up:    dailyUp[i],
			Down:  dailyDown[i],
		}
		out.Series = append(out.Series, du)
		out.TotalBytes += du.Bytes
		if du.Bytes > 0 {
			out.ActiveDays++
		}
		if du.Bytes > out.PeakDailyBytes {
			out.PeakDailyBytes = du.Bytes
			out.PeakDay = day
		}
	}
	if out.ActiveDays > 0 {
		out.AvgDailyBytes = out.TotalBytes / int64(out.ActiveDays)
	}
	return out, raw, nil
}

// parseUsageValue handles the three observed actualUsage value
// shapes. Returns (total, up, down) in bytes.
func parseUsageValue(v json.RawMessage) (total, up, down int64) {
	// Try plain number first — fastest path.
	var n json.Number
	if err := json.Unmarshal(v, &n); err == nil {
		if i, ierr := n.Int64(); ierr == nil {
			return i, 0, 0
		}
		if f, ferr := n.Float64(); ferr == nil {
			return int64(f), 0, 0
		}
	}
	// Try object with up/down or total fields. Tolerant of various
	// upstream key conventions — we've seen "upload"/"download",
	// "ul"/"dl", and explicit "total".
	var obj map[string]json.Number
	if err := json.Unmarshal(v, &obj); err == nil {
		readNum := func(keys ...string) int64 {
			for _, k := range keys {
				if x, ok := obj[k]; ok {
					if i, ierr := x.Int64(); ierr == nil {
						return i
					}
					if f, ferr := x.Float64(); ferr == nil {
						return int64(f)
					}
				}
			}
			return 0
		}
		up = readNum("upload", "up", "ul", "uplink", "tx")
		down = readNum("download", "down", "dl", "downlink", "rx")
		total = readNum("total", "bytes", "sum")
		if total == 0 {
			total = up + down
		}
		return total, up, down
	}
	return 0, 0, 0
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
