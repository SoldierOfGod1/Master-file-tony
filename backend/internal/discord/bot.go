package discord

import (
	"bytes"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"

	"github.com/SoldierOfGod1/command-centre/internal/chat"
	"github.com/SoldierOfGod1/command-centre/internal/event"
)

// PINStore holds the validated PIN and its expiry time.
type PINStore struct {
	pin       string
	expiresAt time.Time
	timeout   time.Duration
	mu        sync.Mutex
}

// SetPIN stores a verified PIN with TTL.
func (ps *PINStore) SetPIN(pin string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.pin = pin
	ps.expiresAt = time.Now().Add(ps.timeout)
}

// IsValid returns true if a PIN was recently validated and has not expired.
func (ps *PINStore) IsValid() bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.pin != "" && time.Now().Before(ps.expiresAt)
}

// Bot is the Discord bot that bridges slash commands to the chat system.
type Bot struct {
	session       *discordgo.Session
	authorizedUID string
	queueManager  *chat.QueueManager
	db            *sql.DB
	log           *slog.Logger
	bus           *event.Bus
	pinStore      *PINStore
	activeConvos  map[string]string // channelID -> conversationID
	mu            sync.RWMutex
}

// NewBot creates and configures a Discord bot. It does NOT open the session;
// call Start() for that.
func NewBot(
	token string,
	authorizedUID string,
	qm *chat.QueueManager,
	db *sql.DB,
	log *slog.Logger,
	bus *event.Bus,
	pinTimeout time.Duration,
) (*Bot, error) {
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("failed to create discord session: %w", err)
	}

	b := &Bot{
		session:       session,
		authorizedUID: authorizedUID,
		queueManager:  qm,
		db:            db,
		log:           log,
		bus:           bus,
		pinStore:      &PINStore{timeout: pinTimeout},
		activeConvos:  make(map[string]string),
	}

	return b, nil
}

// Start opens the websocket connection and registers slash commands.
func (b *Bot) Start() error {
	b.session.AddHandler(b.handleInteraction)

	if err := b.session.Open(); err != nil {
		return fmt.Errorf("failed to open discord session: %w", err)
	}

	if err := b.registerCommands(); err != nil {
		b.session.Close()
		return fmt.Errorf("failed to register slash commands: %w", err)
	}

	b.log.Info("discord bot started", "user", b.session.State.User.Username)
	return nil
}

// Stop gracefully closes the Discord session.
func (b *Bot) Stop() error {
	b.log.Info("discord bot stopping")
	return b.session.Close()
}

// registerCommands registers the slash commands with Discord.
func (b *Bot) registerCommands() error {
	commands := []*discordgo.ApplicationCommand{
		{
			Name:        "chat",
			Description: "Send a message to Claude",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "message",
					Description: "The message to send",
					Required:    true,
				},
			},
		},
		{
			Name:        "ask",
			Description: "Ask a specific agent persona (backend, frontend, security, ai, orchestrator)",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "agent",
					Description: "Which agent persona should respond?",
					Required:    true,
					Choices: []*discordgo.ApplicationCommandOptionChoice{
						{Name: "Orchestrator (01) — plan + route", Value: "orchestrator"},
						{Name: "Backend (04) — APIs, services, DB code", Value: "backend"},
						{Name: "Frontend (07) — React, CSS, UX", Value: "frontend"},
						{Name: "Security (03) — audit, review, hardening", Value: "security"},
						{Name: "AI / ML (10) — LLM, embeddings, RAG", Value: "ai"},
					},
				},
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "message",
					Description: "Your question or task for this agent",
					Required:    true,
				},
			},
		},
		{
			Name:        "new",
			Description: "Create a new conversation",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "project_dir",
					Description: "Project directory path",
					Required:    true,
				},
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "title",
					Description: "Conversation title",
					Required:    false,
				},
			},
		},
		{
			Name:        "switch",
			Description: "Switch active conversation",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "conv_id",
					Description: "Conversation ID to switch to",
					Required:    true,
				},
			},
		},
		{
			Name:        "history",
			Description: "Show last 10 messages in active conversation",
		},
		{
			Name:        "projects",
			Description: "List all projects",
		},
		{
			Name:        "status",
			Description: "Show agents, tasks, and KPI summary",
		},
		{
			Name:        "export",
			Description: "Export active conversation as markdown file",
		},
		{
			Name:        "pin",
			Description: "Authenticate with a PIN for full tool access",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "code",
					Description: "Your PIN code",
					Required:    true,
				},
			},
		},
	}

	appID := b.session.State.User.ID
	for _, cmd := range commands {
		if _, err := b.session.ApplicationCommandCreate(appID, "", cmd); err != nil {
			return fmt.Errorf("failed to register command %s: %w", cmd.Name, err)
		}
	}

	b.log.Info("slash commands registered", "count", len(commands))
	return nil
}

// handleInteraction is the central dispatcher for all slash command events.
func (b *Bot) handleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	// Security: only the authorized user may interact.
	// In guild channels the user is in i.Member.User; in DMs the user is in i.User.
	var userID string
	switch {
	case i.Member != nil && i.Member.User != nil:
		userID = i.Member.User.ID
	case i.User != nil:
		userID = i.User.ID
	}
	if userID == "" || userID != b.authorizedUID {
		return
	}

	data := i.ApplicationCommandData()

	switch data.Name {
	case "chat":
		b.handleChat(s, i)
	case "ask":
		b.handleAsk(s, i)
	case "new":
		b.handleNew(s, i)
	case "switch":
		b.handleSwitch(s, i)
	case "history":
		b.handleHistory(s, i)
	case "projects":
		b.handleProjects(s, i)
	case "status":
		b.handleStatus(s, i)
	case "export":
		b.handleExport(s, i)
	case "pin":
		b.handlePIN(s, i)
	}
}

// --- Slash command handlers ---

func (b *Bot) handleChat(s *discordgo.Session, i *discordgo.InteractionCreate) {
	message := i.ApplicationCommandData().Options[0].StringValue()
	channelID := i.ChannelID

	b.mu.RLock()
	convID, hasConvo := b.activeConvos[channelID]
	b.mu.RUnlock()

	if !hasConvo {
		b.respond(s, i, "No active conversation. Use `/new <project_dir>` first.")
		return
	}

	// Acknowledge — execution may take a while.
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		b.log.Error("failed to defer response", "error", err)
		return
	}

	req := chat.ExecuteRequest{
		ConversationID: convID,
		Prompt:         message,
		ProjectDir:     b.getProjectDir(convID),
		HasPIN:         b.pinStore.IsValid(),
	}

	content, err := b.queueManager.Submit(req)
	if err != nil {
		b.editDeferred(s, i, fmt.Sprintf("Error: %v", err))
		return
	}

	if len(content) > 2000 {
		// Send as .md file attachment for long responses.
		b.editDeferredWithFile(s, i, "Response attached (too long for a message).", "response.md", content)
		return
	}

	b.editDeferred(s, i, content)
}

// handleAsk routes a message to a specific agent persona. Identical plumbing
// to handleChat but sets ExecuteRequest.Agent from the first option so the
// executor appends the persona's system prompt.
func (b *Bot) handleAsk(s *discordgo.Session, i *discordgo.InteractionCreate) {
	opts := i.ApplicationCommandData().Options
	agent := chat.AgentSlug(opts[0].StringValue())
	message := opts[1].StringValue()
	channelID := i.ChannelID

	b.mu.RLock()
	convID, hasConvo := b.activeConvos[channelID]
	b.mu.RUnlock()

	if !hasConvo {
		b.respond(s, i, "No active conversation. Use `/new <project_dir>` first.")
		return
	}

	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		b.log.Error("failed to defer response", "error", err)
		return
	}

	req := chat.ExecuteRequest{
		ConversationID: convID,
		Prompt:         message,
		ProjectDir:     b.getProjectDir(convID),
		HasPIN:         b.pinStore.IsValid(),
		Agent:          agent,
	}

	content, err := b.queueManager.Submit(req)
	if err != nil {
		b.editDeferred(s, i, fmt.Sprintf("Error: %v", err))
		return
	}

	header := fmt.Sprintf("**[%s]**\n", strings.ToUpper(string(agent)))
	reply := header + content

	if len(reply) > 2000 {
		b.editDeferredWithFile(s, i, header+"Response attached (too long for a message).", "response.md", content)
		return
	}

	b.editDeferred(s, i, reply)
}

func (b *Bot) handleNew(s *discordgo.Session, i *discordgo.InteractionCreate) {
	opts := i.ApplicationCommandData().Options
	projectDir := opts[0].StringValue()

	title := "Discord Conversation"
	if len(opts) > 1 {
		title = opts[1].StringValue()
	}

	convID := uuid.New().String()
	if err := chat.CreateConversation(b.db, convID, title, projectDir, "discord"); err != nil {
		b.respond(s, i, fmt.Sprintf("Failed to create conversation: %v", err))
		return
	}

	b.mu.Lock()
	b.activeConvos[i.ChannelID] = convID
	b.mu.Unlock()

	b.bus.PublishJSON("chat.conversation.created", map[string]string{
		"id":         convID,
		"title":      title,
		"projectDir": projectDir,
		"source":     "discord",
	})

	b.respond(s, i, fmt.Sprintf("Conversation created.\n**ID:** `%s`\n**Title:** %s\n**Project:** %s", convID, title, projectDir))
}

func (b *Bot) handleSwitch(s *discordgo.Session, i *discordgo.InteractionCreate) {
	convID := i.ApplicationCommandData().Options[0].StringValue()

	// Verify the conversation exists.
	conv, err := chat.GetConversation(b.db, convID)
	if err != nil {
		b.respond(s, i, fmt.Sprintf("Conversation not found: %v", err))
		return
	}

	b.mu.Lock()
	b.activeConvos[i.ChannelID] = convID
	b.mu.Unlock()

	title, _ := conv["title"].(string)
	b.respond(s, i, fmt.Sprintf("Switched to conversation `%s` (%s)", convID, title))
}

func (b *Bot) handleHistory(s *discordgo.Session, i *discordgo.InteractionCreate) {
	b.mu.RLock()
	convID, hasConvo := b.activeConvos[i.ChannelID]
	b.mu.RUnlock()

	if !hasConvo {
		b.respond(s, i, "No active conversation. Use `/new <project_dir>` first.")
		return
	}

	rows, err := b.db.Query(
		`SELECT role, content, created_at FROM messages
		 WHERE conversation_id = ? ORDER BY id DESC LIMIT 10`, convID,
	)
	if err != nil {
		b.respond(s, i, fmt.Sprintf("Failed to query history: %v", err))
		return
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var role, content, createdAt string
		if err := rows.Scan(&role, &content, &createdAt); err != nil {
			continue
		}
		// Truncate long messages in the summary.
		if len(content) > 200 {
			content = content[:200] + "..."
		}
		lines = append(lines, fmt.Sprintf("**[%s] %s**: %s", createdAt, role, content))
	}

	if len(lines) == 0 {
		b.respond(s, i, "No messages in this conversation yet.")
		return
	}

	// Reverse so oldest is first.
	for left, right := 0, len(lines)-1; left < right; left, right = left+1, right-1 {
		lines[left], lines[right] = lines[right], lines[left]
	}

	b.respond(s, i, strings.Join(lines, "\n"))
}

func (b *Bot) handleProjects(s *discordgo.Session, i *discordgo.InteractionCreate) {
	rows, err := b.db.Query(
		`SELECT id, name, status, description FROM projects ORDER BY name ASC`,
	)
	if err != nil {
		b.respond(s, i, fmt.Sprintf("Failed to list projects: %v", err))
		return
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var id, name, status, desc string
		if err := rows.Scan(&id, &name, &status, &desc); err != nil {
			continue
		}
		lines = append(lines, fmt.Sprintf("- **%s** (`%s`) — %s — %s", name, id, status, desc))
	}

	if len(lines) == 0 {
		b.respond(s, i, "No projects found.")
		return
	}

	b.respond(s, i, "**Projects:**\n"+strings.Join(lines, "\n"))
}

func (b *Bot) handleStatus(s *discordgo.Session, i *discordgo.InteractionCreate) {
	var agentCount, taskCount int
	_ = b.db.QueryRow(`SELECT COUNT(*) FROM agents`).Scan(&agentCount)
	_ = b.db.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&taskCount)

	var activeAgents int
	_ = b.db.QueryRow(`SELECT COUNT(*) FROM agents WHERE status = 'active'`).Scan(&activeAgents)

	var pendingTasks int
	_ = b.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE col != 'done'`).Scan(&pendingTasks)

	var trustScore int
	_ = b.db.QueryRow(`SELECT trust_score FROM security_state WHERE id = 1`).Scan(&trustScore)

	summary := fmt.Sprintf(
		"**Command Centre Status**\n"+
			"Agents: %d total, %d active\n"+
			"Tasks: %d total, %d pending\n"+
			"Security Trust Score: %d%%",
		agentCount, activeAgents, taskCount, pendingTasks, trustScore,
	)

	b.respond(s, i, summary)
}

func (b *Bot) handleExport(s *discordgo.Session, i *discordgo.InteractionCreate) {
	b.mu.RLock()
	convID, hasConvo := b.activeConvos[i.ChannelID]
	b.mu.RUnlock()

	if !hasConvo {
		b.respond(s, i, "No active conversation to export.")
		return
	}

	conv, err := chat.GetConversation(b.db, convID)
	if err != nil {
		b.respond(s, i, fmt.Sprintf("Failed to get conversation: %v", err))
		return
	}

	title, _ := conv["title"].(string)
	messages, _ := conv["messages"].([]map[string]any)

	var md strings.Builder
	md.WriteString(fmt.Sprintf("# %s\n\n", title))
	md.WriteString(fmt.Sprintf("**Conversation ID:** %s\n\n---\n\n", convID))

	for _, msg := range messages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		createdAt, _ := msg["createdAt"].(string)
		md.WriteString(fmt.Sprintf("### %s (%s)\n\n%s\n\n---\n\n", role, createdAt, content))
	}

	// Acknowledge then send file.
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		b.log.Error("failed to defer response", "error", err)
		return
	}

	filename := fmt.Sprintf("conversation-%s.md", convID[:8])
	b.editDeferredWithFile(s, i, "Conversation exported.", filename, md.String())
}

func (b *Bot) handlePIN(s *discordgo.Session, i *discordgo.InteractionCreate) {
	code := i.ApplicationCommandData().Options[0].StringValue()

	// Fetch pin_hash from chat_config.
	var pinHash string
	err := b.db.QueryRow(`SELECT pin_hash FROM chat_config WHERE id = 1`).Scan(&pinHash)
	if err != nil || pinHash == "" {
		b.respond(s, i, "PIN not configured. Set it in Command Centre settings.")
		return
	}

	if !chat.VerifyPIN(code, pinHash) {
		b.respond(s, i, "Invalid PIN.")
		return
	}

	b.pinStore.SetPIN(code)

	minutes := int(b.pinStore.timeout.Minutes())
	b.respond(s, i, fmt.Sprintf("PIN accepted. Full tool access granted for %d minutes.", minutes))

	b.bus.PublishJSON("chat.pin.authenticated", map[string]any{
		"source":  "discord",
		"expires": b.pinStore.expiresAt.Format(time.RFC3339),
	})
}

// --- Response helpers ---

func (b *Bot) respond(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
		},
	}); err != nil {
		b.log.Error("failed to respond to interaction", "error", err)
	}
}

func (b *Bot) editDeferred(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	if _, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: &content,
	}); err != nil {
		b.log.Error("failed to edit deferred response", "error", err)
	}
}

func (b *Bot) editDeferredWithFile(s *discordgo.Session, i *discordgo.InteractionCreate, content, filename, fileContent string) {
	if _, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: &content,
		Files: []*discordgo.File{
			{
				Name:        filename,
				ContentType: "text/markdown",
				Reader:      bytes.NewReader([]byte(fileContent)),
			},
		},
	}); err != nil {
		b.log.Error("failed to edit deferred response with file", "error", err)
	}
}

// getProjectDir looks up the project_dir for a conversation.
func (b *Bot) getProjectDir(convID string) string {
	var dir string
	_ = b.db.QueryRow(`SELECT project_dir FROM conversations WHERE id = ?`, convID).Scan(&dir)
	return dir
}
