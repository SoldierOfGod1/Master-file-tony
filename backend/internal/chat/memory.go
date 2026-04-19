package chat

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/SoldierOfGod1/command-centre/internal/agents"
)

// appendAgentMemory records a one-line lesson under the agent's memory file
// after a successful /ask exchange. Uses a heuristic summary of the prompt +
// first line of the response so the agent has a searchable history of prior
// interactions without needing a separate LLM pass.
//
// Silently no-ops when the request wasn't agent-routed or the global agent
// file can't be located — memory is a nice-to-have, not a hard requirement.
func appendAgentMemory(log *slog.Logger, req ExecuteRequest, response string) {
	if req.Agent == "" {
		return
	}
	fileName, ok := agentMarkdownFile[req.Agent]
	if !ok {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	agentPath := filepath.Join(home, ".claude", "agents", fileName)
	if _, err := os.Stat(agentPath); err != nil {
		return
	}

	note := summariseExchange(req.Prompt, response)
	if note == "" {
		return
	}

	scanner := agents.New("")
	if err := scanner.AppendMemory(agentPath, note); err != nil {
		log.Warn("append agent memory", "agent", string(req.Agent), "error", err)
		return
	}
	log.Info("agent memory appended",
		"agent", string(req.Agent),
		"conversationId", req.ConversationID,
	)
}

// summariseExchange compacts a prompt+response into one line suitable for
// append-only memory. Truncates aggressively so the memory file stays
// readable over hundreds of exchanges.
func summariseExchange(prompt, response string) string {
	p := collapseWhitespace(prompt)
	r := firstSentence(collapseWhitespace(response))
	if p == "" && r == "" {
		return ""
	}
	if len(p) > 140 {
		p = p[:140] + "…"
	}
	if len(r) > 220 {
		r = r[:220] + "…"
	}
	return "Q: " + p + " → A: " + r
}

func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// firstSentence returns up to the first sentence-ish chunk (first period,
// newline, or colon) so memory entries stay to-the-point rather than
// dumping an entire multi-paragraph answer.
func firstSentence(s string) string {
	for i, ch := range s {
		if ch == '.' || ch == '\n' || ch == '!' || ch == '?' {
			if i > 20 {
				return s[:i+1]
			}
		}
	}
	return s
}
