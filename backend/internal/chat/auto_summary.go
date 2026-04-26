package chat

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// AutoSummariser runs at conversation close and writes a single
// memory entry capturing what was learned. Phase D1 follow-up.
//
// Two paths:
//   1. ANTHROPIC_API_KEY set + AgentClient available → ask the
//      model for a tight 1-2 sentence summary.
//   2. No API key → deterministic fallback: take the user's
//      first prompt + the last assistant reply (truncated) and
//      stitch them. Cheap, always works, never costs money.
//
// In both cases the entry is tagged kind='note' and attributed
// to the conversation's user_id. Anonymous conversations are
// skipped — there's no per-user bucket to recall from later.
type AutoSummariser struct {
	db      *sql.DB
	log     *slog.Logger
	agent   *AgentClient // optional; nil = deterministic fallback only
	timeout time.Duration
}

func NewAutoSummariser(db *sql.DB, log *slog.Logger, agent *AgentClient) *AutoSummariser {
	return &AutoSummariser{
		db:      db,
		log:     log,
		agent:   agent,
		timeout: 30 * time.Second,
	}
}

// SummariseAndStore runs synchronously. Callers should fire it
// from a goroutine to avoid blocking the archive HTTP response.
// Returns the persisted memory id, or 0 with a log warning if
// anything fails. Never errors the caller — auto-summary is
// best-effort by design.
func (s *AutoSummariser) SummariseAndStore(ctx context.Context, conversationID string) {
	if s == nil || s.db == nil || conversationID == "" {
		return
	}
	userID, msgs := s.fetchConversation(conversationID)
	if userID == "" || len(msgs) == 0 {
		// Anonymous or empty — nothing to remember.
		return
	}
	body := s.summarise(ctx, msgs)
	if body == "" {
		return
	}
	if id, err := WriteMemory(s.db, userID, "note", body); err != nil {
		if s.log != nil {
			s.log.Warn("auto-summary write", "error", err, "conv", conversationID)
		}
	} else if s.log != nil {
		s.log.Info("auto-summary persisted",
			"conv", conversationID, "user", userID, "memory_id", id, "len", len(body))
	}
}

func (s *AutoSummariser) fetchConversation(id string) (userID string, messages []convMessage) {
	if s.db == nil {
		return "", nil
	}
	_ = s.db.QueryRow(`SELECT user_id FROM conversations WHERE id = ?`, id).Scan(&userID)
	if userID == "" {
		return "", nil
	}
	rows, err := s.db.Query(
		`SELECT role, content FROM messages WHERE conversation_id = ? ORDER BY id ASC LIMIT 200`,
		id,
	)
	if err != nil {
		return userID, nil
	}
	defer rows.Close()
	for rows.Next() {
		var m convMessage
		if err := rows.Scan(&m.Role, &m.Content); err != nil {
			continue
		}
		messages = append(messages, m)
	}
	return userID, messages
}

type convMessage struct {
	Role    string
	Content string
}

// summarise prefers the API path when wired, falls back to a
// deterministic stitcher otherwise. Both return a short body
// suitable for FormatForPrompt's bullet rendering.
func (s *AutoSummariser) summarise(ctx context.Context, msgs []convMessage) string {
	if s.agent != nil {
		if body := s.summariseViaAPI(ctx, msgs); body != "" {
			return body
		}
		// Fall through to deterministic if the API call failed —
		// don't lose the memory just because Anthropic flaked.
	}
	return s.summariseDeterministic(msgs)
}

// summariseViaAPI sends the conversation to Anthropic with a
// tight system prompt asking for one sentence. Bounded by
// s.timeout so a stuck call can't hang the archive flow.
func (s *AutoSummariser) summariseViaAPI(ctx context.Context, msgs []convMessage) string {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	const sys = "You are summarising a closed ops chat for the agent's long-term memory. " +
		"Output ONE short sentence (max 200 chars) capturing the load-bearing finding, decision, " +
		"or preference learned during the conversation. No fluff, no quoting, no markdown."

	// Build a compact transcript — include only role + content,
	// drop system messages.
	var b strings.Builder
	for _, m := range msgs {
		if m.Role == "system" {
			continue
		}
		b.WriteString(strings.ToUpper(m.Role[:1]))
		b.WriteString(": ")
		b.WriteString(strings.TrimSpace(m.Content))
		b.WriteString("\n")
	}
	transcript := b.String()
	if len(transcript) > 16_000 {
		transcript = transcript[len(transcript)-16_000:]
	}

	final, _, err := s.agent.Run(ctx, AgentRunOptions{
		SystemPrompt: sys,
		UserPrompt:   "Transcript:\n" + transcript,
		// Anonymous user — auto-summary doesn't go through the
		// approval gate or per-user memory write from inside the
		// loop. We persist with the conversation's user_id ourselves.
		UserID: "",
	})
	if err != nil {
		if s.log != nil {
			s.log.Info("auto-summary API path failed; falling back",
				"error", err)
		}
		return ""
	}
	final = strings.TrimSpace(final)
	if len(final) > 240 {
		final = final[:240]
	}
	return final
}

// summariseDeterministic stitches together the first user prompt
// and the last assistant reply. Cheap, useful, never wrong about
// what the conversation was about — though it can miss the
// "what was decided" beat the API path catches.
func (s *AutoSummariser) summariseDeterministic(msgs []convMessage) string {
	var firstUser, lastAssistant string
	for _, m := range msgs {
		switch m.Role {
		case "user":
			if firstUser == "" {
				firstUser = strings.TrimSpace(m.Content)
			}
		case "assistant":
			lastAssistant = strings.TrimSpace(m.Content)
		}
	}
	if firstUser == "" && lastAssistant == "" {
		return ""
	}
	if len(firstUser) > 120 {
		firstUser = firstUser[:120] + "…"
	}
	if len(lastAssistant) > 120 {
		lastAssistant = lastAssistant[:120] + "…"
	}
	return fmt.Sprintf("Asked: %q. Concluded: %s", firstUser, lastAssistant)
}
