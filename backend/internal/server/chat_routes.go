package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/SoldierOfGod1/command-centre/internal/chat"
)

// RegisterChatRoutes registers all chat-related endpoints on the given mux.
func RegisterChatRoutes(mux *http.ServeMux, api *API) {
	// Conversations CRUD
	mux.HandleFunc("GET /api/v1/conversations", api.handleListConversations)
	mux.HandleFunc("POST /api/v1/conversations", api.handleCreateConversation)
	mux.HandleFunc("GET /api/v1/conversations/{id}", api.handleGetConversation)
	mux.HandleFunc("PUT /api/v1/conversations/{id}", api.handleUpdateConversation)
	mux.HandleFunc("DELETE /api/v1/conversations/{id}", api.handleDeleteConversation)

	// Conversation export
	mux.HandleFunc("GET /api/v1/conversations/{id}/export", api.handleExportConversation)

	// Real token usage per conversation (backs the Context Gauge)
	mux.HandleFunc("GET /api/v1/conversations/{id}/usage", api.handleConversationUsage)

	// Chat execution
	mux.HandleFunc("POST /api/v1/chat", api.handleChat)

	// Phase A1 — intent classifier. Cheap (no LLM, regex+keyword
	// match), so the frontend can call it on every keystroke /
	// after debounce to show "I think you want X" before commit.
	// See backend/internal/chat/intent.go for the rules.
	mux.HandleFunc("POST /api/v1/chat/classify", api.handleClassifyIntent)

	// Phase A3 — hybrid agent route. Goes through the dispatcher
	// which picks API path (tool-use loop) vs CLI path based on
	// the classifier output + agent availability. Falls back to
	// the existing /chat behaviour when no API key configured.
	mux.HandleFunc("POST /api/v1/chat/agent", api.handleChatAgent)

	// Chat config
	mux.HandleFunc("GET /api/v1/chat/config", api.handleGetChatConfig)
	mux.HandleFunc("PUT /api/v1/chat/config", api.handleUpdateChatConfig)
}

// handleClassifyIntent runs ClassifyIntent over a prompt and
// returns the structured result. Used by the frontend to render
// "I'll route this to <intent>" badges and by Phase A2's tool
// catalogue to short-circuit Claude when a fast path is available.
func (a *API) handleClassifyIntent(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Prompt string `json:"prompt"`
	}
	if err := readJSON(r, &body); err != nil {
		jsonError(w, 400, "invalid json")
		return
	}
	jsonOK(w, chat.ClassifyIntent(body.Prompt))
}

// handleChatAgent runs the Phase A3 dispatcher: classifier picks
// API tool-use loop or CLI subprocess, both stream into the same
// event bus. Returns the final answer text. Falls back to 503 if
// the dispatcher isn't wired (env not configured).
func (a *API) handleChatAgent(w http.ResponseWriter, r *http.Request) {
	if a.Dispatcher == nil {
		jsonError(w, 503, "agent dispatcher not configured (set ANTHROPIC_API_KEY to enable)")
		return
	}
	var body struct {
		ConversationID string `json:"conversationId"`
		Message        string `json:"message"`
		PIN            string `json:"pin"`
		ProjectDir     string `json:"projectDir"`
	}
	if err := readJSON(r, &body); err != nil {
		jsonError(w, 400, "invalid json")
		return
	}
	if body.ConversationID == "" || body.Message == "" {
		jsonError(w, 400, "conversationId and message are required")
		return
	}
	hasPIN := false
	if body.PIN != "" {
		var pinHash string
		if err := a.DB.QueryRow("SELECT pin_hash FROM chat_config WHERE id = 1").Scan(&pinHash); err == nil && pinHash != "" {
			hasPIN = chat.VerifyPIN(body.PIN, pinHash)
		}
	}
	resp, err := a.Dispatcher.Dispatch(r.Context(), chat.ExecuteRequest{
		ConversationID: body.ConversationID,
		Prompt:         body.Message,
		ProjectDir:     body.ProjectDir,
		HasPIN:         hasPIN,
	})
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("agent dispatch failed: %v", err))
		return
	}
	jsonOK(w, map[string]any{"response": resp})
}

// ── Conversations ────────────────────────────────────────

func (a *API) handleListConversations(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	conversations, err := chat.ListConversations(a.DB, status)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("failed to list conversations: %v", err))
		return
	}
	jsonOK(w, conversations)
}

func (a *API) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title      string `json:"title"`
		ProjectDir string `json:"projectDir"`
		Source     string `json:"source"`
	}
	if err := readJSON(r, &body); err != nil {
		jsonError(w, 400, "invalid json")
		return
	}
	if body.Title == "" {
		jsonError(w, 400, "title is required")
		return
	}

	id := fmt.Sprintf("conv-%d", time.Now().UnixMilli())

	source := body.Source
	if source == "" {
		source = "web"
	}

	if err := chat.CreateConversation(a.DB, id, body.Title, body.ProjectDir, source); err != nil {
		jsonError(w, 500, fmt.Sprintf("failed to create conversation: %v", err))
		return
	}

	a.Bus.PublishJSON("chat.conversation", map[string]string{"id": id, "action": "created"})
	jsonOK(w, map[string]string{"id": id})
}

func (a *API) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	conv, err := chat.GetConversation(a.DB, id)
	if err != nil {
		jsonError(w, 404, "conversation not found")
		return
	}
	jsonOK(w, conv)
}

func (a *API) handleUpdateConversation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var body struct {
		Title      *string `json:"title"`
		Status     *string `json:"status"`
		ProjectDir *string `json:"projectDir"`
	}
	if err := readJSON(r, &body); err != nil {
		jsonError(w, 400, "invalid json")
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if body.Title != nil {
		a.DB.Exec("UPDATE conversations SET title=?, updated_at=? WHERE id=?", *body.Title, now, id)
	}
	if body.Status != nil {
		a.DB.Exec("UPDATE conversations SET status=?, updated_at=? WHERE id=?", *body.Status, now, id)
	}
	if body.ProjectDir != nil {
		a.DB.Exec("UPDATE conversations SET project_dir=?, updated_at=? WHERE id=?", *body.ProjectDir, now, id)
	}

	a.Bus.PublishJSON("chat.conversation", map[string]string{"id": id, "action": "updated"})
	jsonOK(w, map[string]string{"id": id, "status": "updated"})
}

func (a *API) handleDeleteConversation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	now := time.Now().UTC().Format(time.RFC3339)
	a.DB.Exec("UPDATE conversations SET status='archived', updated_at=? WHERE id=?", now, id)

	a.Bus.PublishJSON("chat.conversation", map[string]string{"id": id, "action": "archived"})
	jsonOK(w, map[string]string{"id": id, "status": "archived"})
}

// ── Export ────────────────────────────────────────────────

func (a *API) handleExportConversation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "md"
	}

	conv, err := chat.GetConversation(a.DB, id)
	if err != nil {
		jsonError(w, 404, "conversation not found")
		return
	}

	title, _ := conv["title"].(string)
	messages, _ := conv["messages"].([]map[string]any)

	switch format {
	case "json":
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.json"`, id))
		json.NewEncoder(w).Encode(conv)

	default: // md
		var md strings.Builder
		md.WriteString(fmt.Sprintf("# %s\n\n", title))
		for _, msg := range messages {
			role, _ := msg["role"].(string)
			content, _ := msg["content"].(string)
			switch role {
			case "user":
				md.WriteString(fmt.Sprintf("**User:** %s\n\n", content))
			case "assistant":
				md.WriteString(fmt.Sprintf("**Soldier of God:** %s\n\n", content))
			default:
				md.WriteString(fmt.Sprintf("**%s:** %s\n\n", role, content))
			}
		}

		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.md"`, id))
		w.Write([]byte(md.String()))
	}
}

// ── Chat execution ───────────────────────────────────────

func (a *API) handleChat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ConversationID string `json:"conversationId"`
		Message        string `json:"message"`
		PIN            string `json:"pin"`
	}
	if err := readJSON(r, &body); err != nil {
		jsonError(w, 400, "invalid json")
		return
	}
	if body.ConversationID == "" || body.Message == "" {
		jsonError(w, 400, "conversationId and message are required")
		return
	}

	// Validate conversation exists.
	conv, err := chat.GetConversation(a.DB, body.ConversationID)
	if err != nil || conv == nil {
		jsonError(w, 404, "conversation not found")
		return
	}

	// Verify PIN if provided.
	hasPIN := false
	if body.PIN != "" {
		var pinHash string
		err := a.DB.QueryRow("SELECT pin_hash FROM chat_config WHERE id = 1").Scan(&pinHash)
		if err != nil || pinHash == "" {
			jsonError(w, 403, "PIN verification failed: no PIN configured")
			return
		}
		if !chat.VerifyPIN(body.PIN, pinHash) {
			jsonError(w, 403, "invalid PIN")
			return
		}
		hasPIN = true
	}

	// Resolve project directory: use conversation's projectDir or fall back to config default.
	projectDir, _ := conv["projectDir"].(string)
	if projectDir == "" {
		var defaultDir string
		a.DB.QueryRow("SELECT default_project_dir FROM chat_config WHERE id = 1").Scan(&defaultDir)
		projectDir = defaultDir
	}

	if a.QueueMgr == nil {
		jsonError(w, 500, "chat queue manager not initialised")
		return
	}

	response, err := a.QueueMgr.Submit(chat.ExecuteRequest{
		ConversationID: body.ConversationID,
		Prompt:         body.Message,
		ProjectDir:     projectDir,
		HasPIN:         hasPIN,
	})
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("chat execution failed: %v", err))
		return
	}

	jsonOK(w, map[string]any{
		"conversationId": body.ConversationID,
		"response":       response,
	})
}

// ── Chat config ──────────────────────────────────────────

func (a *API) handleGetChatConfig(w http.ResponseWriter, r *http.Request) {
	var discordToken, discordUserID, defaultProjectDir string
	var pinTimeoutMinutes int
	err := a.DB.QueryRow(
		`SELECT discord_token, discord_user_id, default_project_dir, pin_timeout_minutes
		 FROM chat_config WHERE id = 1`,
	).Scan(&discordToken, &discordUserID, &defaultProjectDir, &pinTimeoutMinutes)
	if err != nil {
		// Return empty config if none exists yet.
		jsonOK(w, map[string]any{
			"discordToken":      "",
			"discordUserId":     "",
			"defaultProjectDir": "",
			"pinTimeoutMinutes": 0,
		})
		return
	}

	// Redact discord token — show only last 4 chars.
	redacted := ""
	if len(discordToken) > 4 {
		redacted = "****" + discordToken[len(discordToken)-4:]
	} else if discordToken != "" {
		redacted = "****"
	}

	jsonOK(w, map[string]any{
		"discordToken":      redacted,
		"discordUserId":     discordUserID,
		"defaultProjectDir": defaultProjectDir,
		"pinTimeoutMinutes": pinTimeoutMinutes,
	})
}

func (a *API) handleUpdateChatConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DiscordToken      *string `json:"discordToken"`
		DiscordUserID     *string `json:"discordUserId"`
		PIN               *string `json:"pin"`
		DefaultProjectDir *string `json:"defaultProjectDir"`
		PINTimeoutMinutes *int    `json:"pinTimeoutMinutes"`
	}
	if err := readJSON(r, &body); err != nil {
		jsonError(w, 400, "invalid json")
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)

	if body.DiscordToken != nil {
		a.DB.Exec("UPDATE chat_config SET discord_token=?, updated_at=? WHERE id=1", *body.DiscordToken, now)
	}
	if body.DiscordUserID != nil {
		a.DB.Exec("UPDATE chat_config SET discord_user_id=?, updated_at=? WHERE id=1", *body.DiscordUserID, now)
	}
	if body.PIN != nil {
		hash := chat.HashPIN(*body.PIN)
		a.DB.Exec("UPDATE chat_config SET pin_hash=?, updated_at=? WHERE id=1", hash, now)
	}
	if body.DefaultProjectDir != nil {
		a.DB.Exec("UPDATE chat_config SET default_project_dir=?, updated_at=? WHERE id=1", *body.DefaultProjectDir, now)
	}
	if body.PINTimeoutMinutes != nil {
		a.DB.Exec("UPDATE chat_config SET pin_timeout_minutes=?, updated_at=? WHERE id=1", *body.PINTimeoutMinutes, now)
	}

	jsonOK(w, map[string]string{"status": "updated"})
}

// handleConversationUsage returns real token totals for one conversation.
// Reads from cost_records (written by chat.RecordUsage after each Claude
// CLI run). Drives the real-valued Context Gauge in ChatPage.
func (a *API) handleConversationUsage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonError(w, http.StatusBadRequest, "conversation id required")
		return
	}
	var input, output, total int
	var cost float64
	err := a.DB.QueryRow(
		`SELECT COALESCE(SUM(input_tokens),0),
		        COALESCE(SUM(output_tokens),0),
		        COALESCE(SUM(tokens_used),0),
		        COALESCE(SUM(amount_zar),0)
		 FROM cost_records WHERE conversation_id=?`, id,
	).Scan(&input, &output, &total, &cost)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Also surface the most recent model hint so the UI picks the correct
	// context-window ceiling without a second round-trip.
	var model string
	_ = a.DB.QueryRow(
		`SELECT model_name FROM cost_records
		 WHERE conversation_id=? AND model_name != ''
		 ORDER BY id DESC LIMIT 1`, id,
	).Scan(&model)

	jsonOK(w, map[string]any{
		"conversation_id": id,
		"input_tokens":    input,
		"output_tokens":   output,
		"total_tokens":    total,
		"amount_zar":      cost,
		"model":           model,
	})
}
