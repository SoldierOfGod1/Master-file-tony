package chat

import (
	"database/sql"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Pricing (USD per million tokens) — rough Anthropic list prices as of
// 2026. Tuned for dashboard KPI estimation only; don't reuse for billing.
var pricingPerMillion = map[string]struct{ in, out float64 }{
	"opus":       {15.00, 75.00},
	"sonnet":     {3.00, 15.00},
	"haiku":      {0.80, 4.00},
	"unknown":    {3.00, 15.00}, // default to sonnet pricing for unknown models
}

// UsageRecord captures one LLM call's footprint. Parsed from Claude CLI
// output; written to cost_records so /api/v1/costs + /api/v1/kpis can
// report on it without calling the CLI again.
type UsageRecord struct {
	ConversationID string
	Model          string
	InputTokens    int
	OutputTokens   int
	AmountZAR      float64
	UserID         string // Phase B3 — per-user budget attribution
	IncidentID     string // Phase D2 — spend attribution to the active incident
}

// Regex variants — different Claude CLI versions print usage slightly
// differently. We try each; first match wins. Case-insensitive.
var usagePatterns = []*regexp.Regexp{
	// Canonical: "Usage: input=123 output=456"
	regexp.MustCompile(`(?i)usage:?\s*input[=:\s]+(\d+)[\s,]+output[=:\s]+(\d+)`),
	// JSON-ish: "input_tokens": 123, "output_tokens": 456
	regexp.MustCompile(`(?i)"input_tokens"\s*:\s*(\d+)[^}]*"output_tokens"\s*:\s*(\d+)`),
	regexp.MustCompile(`(?i)"output_tokens"\s*:\s*(\d+)[^}]*"input_tokens"\s*:\s*(\d+)`),
	// Simple labels: "Input tokens: 123\nOutput tokens: 456"
	regexp.MustCompile(`(?i)input[\s_]tokens[=:\s]+(\d+)[\s\S]{0,200}?output[\s_]tokens[=:\s]+(\d+)`),
}

// ParseUsage scans the combined stdout + stderr blob for a usage marker
// and returns the extracted counts. Returns ok=false when no marker is
// found (not an error — just means this Claude CLI build isn't emitting one).
func ParseUsage(output string) (inputTokens, outputTokens int, ok bool) {
	for _, re := range usagePatterns {
		m := re.FindStringSubmatch(output)
		if m == nil {
			continue
		}
		// First pattern has input then output; the reversed JSON pattern has
		// output then input. Determine order from the regex string.
		a, _ := strconv.Atoi(m[1])
		b, _ := strconv.Atoi(m[2])
		if strings.Contains(re.String(), `"output_tokens"\s*:\s*(\d+)[^}]*"input_tokens"`) {
			return b, a, true
		}
		return a, b, true
	}
	return 0, 0, false
}

// EstimateCostZAR converts token counts to an estimated cost in Rand using
// the per-million pricing table. ZAR conversion pinned at 18.5 ZAR/USD —
// close enough for a dashboard tile; the user can adjust the constant.
const ZARPerUSD = 18.5

func EstimateCostZAR(model string, input, output int) float64 {
	key := strings.ToLower(model)
	if key == "" {
		key = "unknown"
	}
	// Fuzzy match: "claude-opus-4-7" → "opus".
	for fam := range pricingPerMillion {
		if strings.Contains(key, fam) {
			key = fam
			break
		}
	}
	p, ok := pricingPerMillion[key]
	if !ok {
		p = pricingPerMillion["unknown"]
	}
	usd := (float64(input)/1_000_000)*p.in + (float64(output)/1_000_000)*p.out
	return usd * ZARPerUSD
}

// detectModelHint scans the output for a model identifier (e.g.
// "claude-opus-4-7", "sonnet") so pricing can be applied appropriately.
// Defaults to "unknown" which maps to sonnet-tier pricing.
var modelHintRe = regexp.MustCompile(`(?i)claude-(opus|sonnet|haiku)-?[\w.-]*`)

func detectModelHint(output string) string {
	if m := modelHintRe.FindString(output); m != "" {
		return strings.ToLower(m)
	}
	return "unknown"
}

// RecordUsage writes one UsageRecord into the cost_records table. Safe to
// call with zero tokens — it becomes a no-op so callers don't need to
// pre-filter.
func RecordUsage(db *sql.DB, log *slog.Logger, rec UsageRecord) {
	if rec.InputTokens == 0 && rec.OutputTokens == 0 {
		return
	}
	totalTokens := rec.InputTokens + rec.OutputTokens
	modelName := rec.Model
	if modelName == "" {
		modelName = "unknown"
	}
	today := time.Now().UTC().Format("2006-01-02")
	_, err := db.Exec(
		`INSERT INTO cost_records
			(date, model_name, amount_zar, tokens_used,
			 conversation_id, input_tokens, output_tokens, user_id, incident_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		today, modelName, rec.AmountZAR, totalTokens,
		rec.ConversationID, rec.InputTokens, rec.OutputTokens, rec.UserID, rec.IncidentID,
	)
	if err != nil {
		log.Warn("record usage", "error", err)
		return
	}
	log.Info("recorded cost",
		"model", modelName,
		"tokens", totalTokens,
		"zar", fmt.Sprintf("%.2f", rec.AmountZAR),
		"conversationId", rec.ConversationID,
	)
}
