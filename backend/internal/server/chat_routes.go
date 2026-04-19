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

	// Chat execution
	mux.HandleFunc("POST /api/v1/chat", api.handleChat)

	// Chat config
	mux.HandleFunc("GET /api/v1/chat/config", api.handleGetChatConfig)
	mux.HandleFunc("PUT /api/v1/chat/config", api.handleUpdateChatConfig)
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
