package chat

import (
	"database/sql"
	"sort"
	"strings"
	"time"
)

// Patterns is the cross-user aggregate ops telemetry surface
// promised in the 2026-04-26 article-review follow-up. Originally
// deferred for InfoSec review; this version ships with two
// privacy guards baked in:
//
//   1. Counts only — no message bodies, no user IDs, no
//      conversation contents are ever returned.
//   2. k-anonymity (k=3) — any aggregate derived from per-user
//      data is suppressed below 3 distinct contributing users so
//      a single operator can't be re-identified from a long-tail
//      keyword.
//
// Caller still has to be RAIN_SUPPORT_L2-gated at the route layer;
// these guards are defence in depth, not a substitute for the
// envelope. Returned struct is JSON-shaped for direct response.
type Patterns struct {
	// ConversationsByDay is daily conversation count for the
	// last 30 days, oldest first. Day strings are YYYY-MM-DD UTC.
	ConversationsByDay []DayCount `json:"conversations_by_day"`

	// MemoryByKind is memory-row count grouped by kind across all
	// users. Tiny cardinality, no privacy concern.
	MemoryByKind []KindCount `json:"memory_by_kind"`

	// ActiveUsers7d is the count of distinct user_ids that opened
	// or contributed to a conversation in the last 7 days. Returns
	// 0 when fewer than 3 distinct users contributed (k-anon).
	ActiveUsers7d int `json:"active_users_7d"`
	// ActiveUsers7dSuppressed is true when the underlying count
	// existed but was suppressed under k-anon. Lets the UI render
	// "<3" instead of pretending nothing happened.
	ActiveUsers7dSuppressed bool `json:"active_users_7d_suppressed"`

	// TopKeywordStems are the most frequent ≥4-char alpha tokens
	// across all agent_memory bodies, suppressed to those that
	// appear in ≥3 distinct user buckets. This is the only "what
	// are people talking about" signal, deliberately bounded.
	TopKeywordStems []KeywordCount `json:"top_keyword_stems"`

	// GeneratedAt is the server timestamp at aggregation time so
	// the UI can render a freshness chip.
	GeneratedAt time.Time `json:"generated_at"`
}

type DayCount struct {
	Day   string `json:"day"`
	Count int    `json:"count"`
}

type KindCount struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

type KeywordCount struct {
	Stem        string `json:"stem"`
	Occurrences int    `json:"occurrences"`
	UserBuckets int    `json:"user_buckets"`
}

const kAnonMin = 3

// AggregatePatterns runs all four queries serially. Each query is
// cheap (counts on indexed columns) so we don't bother parallelising.
// Returns a fully-populated Patterns struct even when some sections
// are empty / suppressed.
func AggregatePatterns(db *sql.DB) Patterns {
	out := Patterns{GeneratedAt: time.Now().UTC()}
	if db == nil {
		return out
	}
	out.ConversationsByDay = conversationsByDay(db)
	out.MemoryByKind = memoryByKind(db)
	out.ActiveUsers7d, out.ActiveUsers7dSuppressed = activeUsers7d(db)
	out.TopKeywordStems = topKeywordStems(db)
	return out
}

func conversationsByDay(db *sql.DB) []DayCount {
	rows, err := db.Query(`
		SELECT substr(created_at, 1, 10) AS day, COUNT(*) AS c
		FROM conversations
		WHERE created_at >= datetime('now', '-30 days')
		GROUP BY day
		ORDER BY day ASC
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []DayCount
	for rows.Next() {
		var d DayCount
		if err := rows.Scan(&d.Day, &d.Count); err == nil {
			out = append(out, d)
		}
	}
	return out
}

func memoryByKind(db *sql.DB) []KindCount {
	rows, err := db.Query(`
		SELECT kind, COUNT(*) AS c
		FROM agent_memory
		GROUP BY kind
		ORDER BY c DESC
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []KindCount
	for rows.Next() {
		var k KindCount
		if err := rows.Scan(&k.Kind, &k.Count); err == nil {
			out = append(out, k)
		}
	}
	return out
}

// activeUsers7d returns the count and a "suppressed" flag — when
// fewer than kAnonMin distinct users contributed we return 0,true
// so the UI can render "<3" without exposing the raw small N.
func activeUsers7d(db *sql.DB) (int, bool) {
	var n int
	err := db.QueryRow(`
		SELECT COUNT(DISTINCT user_id)
		FROM conversations
		WHERE user_id <> ''
		  AND created_at >= datetime('now', '-7 days')
	`).Scan(&n)
	if err != nil {
		return 0, false
	}
	if n > 0 && n < kAnonMin {
		return 0, true
	}
	return n, false
}

// topKeywordStems pulls every memory body, tokenises into ≥4-char
// alpha-only stems, counts occurrences AND distinct user buckets
// per stem, and returns the top 20 stems whose user_buckets ≥ 3.
//
// k-anon gate: a stem only surfaces when at least 3 different
// users have it in their memory — so a single user's quirky
// vocabulary can't leak into the aggregate view.
func topKeywordStems(db *sql.DB) []KeywordCount {
	rows, err := db.Query(`SELECT user_id, body FROM agent_memory`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	type stemAgg struct {
		occ   int
		users map[string]struct{}
	}
	tally := make(map[string]*stemAgg)

	for rows.Next() {
		var user, body string
		if err := rows.Scan(&user, &body); err != nil {
			continue
		}
		seenInRow := make(map[string]struct{})
		for _, tok := range tokenise(body) {
			if _, dup := seenInRow[tok]; dup {
				continue
			}
			seenInRow[tok] = struct{}{}
			s, ok := tally[tok]
			if !ok {
				s = &stemAgg{users: map[string]struct{}{}}
				tally[tok] = s
			}
			s.occ++
			if user != "" {
				s.users[user] = struct{}{}
			}
		}
	}

	out := make([]KeywordCount, 0, len(tally))
	for stem, agg := range tally {
		if len(agg.users) < kAnonMin {
			continue
		}
		out = append(out, KeywordCount{
			Stem:        stem,
			Occurrences: agg.occ,
			UserBuckets: len(agg.users),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Occurrences != out[j].Occurrences {
			return out[i].Occurrences > out[j].Occurrences
		}
		return out[i].Stem < out[j].Stem
	})
	if len(out) > 20 {
		out = out[:20]
	}
	return out
}

// tokenise lower-cases, splits on non-letters, drops stop-words and
// short tokens. Cheap and good enough for fleet-wide keyword tally;
// no need for a real stemmer or NLP library.
func tokenise(s string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() >= 4 {
			tok := strings.ToLower(b.String())
			if !stopWords[tok] {
				out = append(out, tok)
			}
		}
		b.Reset()
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// stopWords blocks the most-common conversational filler so the
// top-N list isn't 80% "user", "asked", "with". Keep tight; this
// is for noise reduction, not censorship.
var stopWords = map[string]bool{
	"this": true, "that": true, "with": true, "from": true, "have": true,
	"been": true, "were": true, "they": true, "them": true, "what": true,
	"when": true, "user": true, "about": true, "their": true, "would": true,
	"could": true, "asked": true, "concluded": true, "agent": true,
	"there": true, "which": true, "after": true, "before": true,
}
