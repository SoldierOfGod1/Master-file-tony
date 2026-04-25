package chat

import (
	"regexp"
	"strings"
)

// Intent is the high-level category we route a user prompt into.
// Phase A1 of the agent-orchestrator plan in the 2026-04-25 CEO
// review. The classifier is intentionally pattern-based: cheap,
// deterministic, runs in microseconds, and never spawns Claude
// just to decide whether to spawn Claude.
//
// Routing decisions downstream (Phase A2):
//   - IntentCustomerLookup: try the existing /api/v1/customer/360
//     directly with the extracted identifier. Fall back to Claude
//     if extraction fails or the lookup returns nothing useful.
//   - IntentSystemStatus: serve from /api/v1/platforms/* without
//     touching Claude at all.
//   - IntentDataQuery: structured DB question (e.g. "how many
//     payments yesterday"). Phase A2 builds the tool catalogue
//     so the agent loop can answer with native tool calls.
//   - IntentCodeTask: spawn Claude CLI as today (executor.go).
//   - IntentUnclear: ask the user one clarifying question or
//     fall through to Claude with a "low-confidence" log line.
type Intent string

const (
	IntentCustomerLookup Intent = "customer_lookup"
	IntentSystemStatus   Intent = "system_status"
	IntentDataQuery      Intent = "data_query"
	IntentCodeTask       Intent = "code_task"
	IntentUnclear        Intent = "unclear"
)

// IntentResult carries the classification plus optional extracted
// arguments — e.g. the email or phone that a customer-lookup
// intent should target. Confidence is 0..1; the chat handler can
// gate on it (low confidence falls through to Claude rather than
// guessing the route).
type IntentResult struct {
	Intent     Intent            `json:"intent"`
	Confidence float64           `json:"confidence"`
	Reason     string            `json:"reason"`
	Args       map[string]string `json:"args,omitempty"`
}

// regexes are compiled once at init to keep the classifier hot-path
// allocation-free. Patterns are deliberately conservative — false
// positives route a code-task to a customer lookup which would feel
// dumb; false negatives just spend a Claude roundtrip we'd otherwise
// save. Optimise for precision over recall.
var (
	emailRE = regexp.MustCompile(`(?i)\b([a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,})\b`)
	// MSISDN: SA mobile numbers — 10 digits starting with 0, or
	// +27 / 27 prefix. Permissive on internal whitespace + dashes.
	msisdnRE = regexp.MustCompile(`(?:^|[^\d])(?:\+?27|0)\s*[\d\s\-]{8,12}(?:[^\d]|$)`)
	// IMSI: 14-15 digit number. Plain digits, no separators.
	imsiRE = regexp.MustCompile(`\b\d{14,15}\b`)
	// UUID — used as customer ID in some flows.
	uuidRE = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
)

// keyword bags. Order matters loosely: more specific phrases first.
// Each entry is lowercased; the matcher lowercases the prompt once
// and substring-checks. Keep these short and ops-flavoured —
// engineering verbs go to code-task by default.
var (
	customerLookupKeywords = []string{
		"customer", "look up", "lookup", "find user", "find account",
		"who is", "show me account", "show account",
		"why was", "why did", "decline", "declined", "payment failed",
		"churn", "ltv", "lifetime value", "balance for",
		"sim swap", "sim diagnost", "imsi for", "msisdn for",
	}
	systemStatusKeywords = []string{
		"is the system", "system status", "is axiom up", "is axiom down",
		"is gaussdb", "is the api up", "uptime", "health check",
		"any incidents", "any alerts", "what's broken", "whats broken",
		"is it down", "is anything down",
	}
	dataQueryKeywords = []string{
		"how many", "count of", "total ", "sum of", "average ",
		"top 10", "top 5", "top 20",
		"yesterday", "last week", "last 7 days", "last 30 days",
		"this month", "this quarter",
	}
	codeTaskKeywords = []string{
		"fix the", "fix a bug", "fix this", "implement",
		"refactor", "rename", "add a function", "add an endpoint",
		"write a test", "write tests", "build me", "build a",
		"add support for", "make a component", "create a component",
		"create a hook", "create a script", "set up", "wire up",
		"deploy", "ship it", "open a pr", "create a pr",
	}
)

// ClassifyIntent runs the prompt through every signal in priority
// order and returns the highest-confidence match. Always returns
// a usable result (never nil) — at worst IntentUnclear with the
// raw prompt summary in Reason.
//
// Decision order:
//   1. Customer-identifier extraction wins above all else (an email
//      in the prompt is a near-certain signal — even "fix the bug
//      reported by alice@rain.co.za" is rerouted; the customer is
//      the entry point for the answer).
//   2. System-status keywords — short-circuit before anything else
//      because these are cheap to answer.
//   3. Code-task keywords — preserve existing chat behaviour.
//   4. Data-query keywords — needs Phase A2 tool catalogue to
//      actually answer; for now logs the intent and falls through.
//   5. Default unclear.
func ClassifyIntent(prompt string) IntentResult {
	if strings.TrimSpace(prompt) == "" {
		return IntentResult{Intent: IntentUnclear, Confidence: 0, Reason: "empty prompt"}
	}
	low := strings.ToLower(prompt)
	args := map[string]string{}

	// Customer identifier — email beats msisdn beats imsi beats uuid.
	if m := emailRE.FindStringSubmatch(prompt); len(m) > 1 {
		args["email"] = m[1]
		return IntentResult{
			Intent: IntentCustomerLookup, Confidence: 0.95,
			Reason: "email address detected", Args: args,
		}
	}
	if m := msisdnRE.FindString(prompt); m != "" {
		// Strip non-digits for downstream matching against
		// vw_service_account_state_latest.
		digits := stripNonDigits(m)
		// SA MSISDNs land at 10 (local 0xx) or 11-12 (+27xx).
		if len(digits) >= 9 && len(digits) <= 12 {
			args["msisdn"] = digits
			return IntentResult{
				Intent: IntentCustomerLookup, Confidence: 0.85,
				Reason: "msisdn detected", Args: args,
			}
		}
	}
	if m := uuidRE.FindString(prompt); m != "" {
		args["customer_id"] = m
		return IntentResult{
			Intent: IntentCustomerLookup, Confidence: 0.8,
			Reason: "uuid detected (likely customer_id)", Args: args,
		}
	}
	if m := imsiRE.FindString(prompt); m != "" {
		args["imsi"] = m
		return IntentResult{
			Intent: IntentCustomerLookup, Confidence: 0.75,
			Reason: "imsi detected", Args: args,
		}
	}

	// Code-task keywords first — verbs ("fix", "refactor",
	// "implement") are semantically heavier than noun-only signals
	// ("uptime", "customer"). When both match, the action wins.
	if hits := countMatches(low, codeTaskKeywords); hits > 0 {
		return IntentResult{
			Intent: IntentCodeTask, Confidence: confFor(hits),
			Reason: "code-task keyword(s) matched",
		}
	}

	// Data-query patterns ("how many", "top 10", "average") next —
	// they're usually anchored on aggregation verbs that override
	// "customer" / "decline" surface words.
	if hits := countMatches(low, dataQueryKeywords); hits > 0 {
		return IntentResult{
			Intent: IntentDataQuery, Confidence: confFor(hits),
			Reason: "data-query keyword(s) matched",
		}
	}

	// System-status keywords.
	if hits := countMatches(low, systemStatusKeywords); hits > 0 {
		return IntentResult{
			Intent: IntentSystemStatus, Confidence: confFor(hits),
			Reason: "status keyword(s) matched",
		}
	}

	// Customer-lookup keywords without an extracted identifier.
	// Cap confidence at 0.55 so the chat handler always asks the
	// user for the identifier rather than guessing — downstream
	// can't /customer/360 without one.
	if hits := countMatches(low, customerLookupKeywords); hits > 0 {
		conf := confFor(hits) - 0.25
		if conf > 0.55 {
			conf = 0.55
		}
		if conf < 0 {
			conf = 0
		}
		return IntentResult{
			Intent: IntentCustomerLookup, Confidence: conf,
			Reason: "customer keyword(s) matched, no identifier extracted",
		}
	}

	return IntentResult{Intent: IntentUnclear, Confidence: 0.0, Reason: "no signals matched"}
}

func countMatches(haystack string, bag []string) int {
	hits := 0
	for _, kw := range bag {
		if strings.Contains(haystack, kw) {
			hits++
		}
	}
	return hits
}

// confFor maps a hit count to a confidence score with diminishing
// returns. 1 hit = 0.65, 2 = 0.78, 3 = 0.85, 4+ = 0.9.
func confFor(hits int) float64 {
	switch {
	case hits <= 0:
		return 0
	case hits == 1:
		return 0.65
	case hits == 2:
		return 0.78
	case hits == 3:
		return 0.85
	default:
		return 0.9
	}
}

func stripNonDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
