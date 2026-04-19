# Design: Chat & Discord Integration for Command Centre

## Context

The Command Centre ("Soldier of God") needs an integrated AI chat capability powered by Claude Code CLI, accessible from both the Command Centre UI and Discord. This enables the user to send instructions to Claude from the dashboard (primary) or remotely via Discord (mobile/away), with full agent mode and shared conversation history.

## Understanding Summary

- **What**: Built-in AI chat in Command Centre + Discord bot, both powered by `claude -p` CLI
- **Why**: AI-assisted development from dashboard + remote control from mobile via Discord
- **Who**: Single user (Baptista Manuel) — sole operator of "Soldier of God" orchestrator
- **Cost model**: Zero additional cost — piggybacks on existing Claude Code subscription
- **Security**: Discord restricted to one user ID + PIN for destructive actions
- **History**: Persistent in SQLite, exportable as markdown/JSON

## Architecture

```
Command Centre UI ──┐
                    ├──▶ Go Backend (:8080)
Discord Bot ────────┘        │
                             ├── internal/chat/executor.go  → spawns `claude -p`
                             ├── internal/chat/queue.go     → per-project queue
                             ├── internal/discord/bot.go    → discordgo goroutine
                             ├── SQLite (conversations + messages tables)
                             └── WebSocket hub (stream responses)
```

## Database Schema

### conversations
| Column | Type | Description |
|--------|------|-------------|
| id | TEXT PK | `conv-{timestamp}` |
| title | TEXT | Auto-generated or user-set |
| project_dir | TEXT | Working directory for this conversation |
| source | TEXT | `ui` or `discord` |
| status | TEXT | `active`, `archived` |
| created_at | TEXT | ISO 8601 |
| updated_at | TEXT | ISO 8601 |

### messages
| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER PK | Auto-increment |
| conversation_id | TEXT FK | References conversations |
| role | TEXT | `user`, `assistant`, `system` |
| content | TEXT | Message text |
| source | TEXT | `ui`, `discord` |
| metadata | TEXT | JSON — tokens, duration_ms, files_changed |
| created_at | TEXT | ISO 8601 |

### chat_config
| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER PK | Always 1 |
| discord_token | TEXT | Encrypted bot token |
| discord_user_id | TEXT | Authorized Discord user ID |
| pin_hash | TEXT | Hashed PIN for destructive actions |
| default_project_dir | TEXT | Default working directory |
| pin_timeout_minutes | INTEGER | PIN validity duration (default: 15) |

## API Endpoints

### Conversation Management
| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/conversations` | List conversations (filter: status, project) |
| POST | `/api/v1/conversations` | Create conversation (title, project_dir) |
| GET | `/api/v1/conversations/{id}` | Get conversation with messages |
| PUT | `/api/v1/conversations/{id}` | Update title, status, project_dir |
| DELETE | `/api/v1/conversations/{id}` | Archive conversation |
| GET | `/api/v1/conversations/{id}/export` | Export as md or json |

### Chat
| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/chat` | Send message, triggers Claude CLI |

Request body:
```json
{
  "conversationId": "conv-123",
  "message": "Add error handling to server.go",
  "pin": "1234"
}
```

### Chat Config
| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/chat/config` | Get settings (tokens redacted) |
| PUT | `/api/v1/chat/config` | Update settings |

## Claude CLI Execution

### Executor (`internal/chat/executor.go`)
1. Receives prompt + working directory
2. Builds command: `claude -p "{prompt}" --output-format stream-json --cwd {dir}`
3. Without PIN: adds `--allowedTools Read,Glob,Grep,WebFetch` (read-only)
4. With valid PIN: full tool access
5. Spawns process, streams stdout via WebSocket
6. Saves final response to messages table

### Queue Manager (`internal/chat/queue.go`)
- Per-project channel (map of `projectDir → chan`)
- One goroutine per active project, processes sequentially
- Different projects run in parallel
- Queue depth limit: 5 per project (429 if full)

## Discord Bot

### Library: `discordgo`

### Slash Commands
| Command | Description |
|---------|-------------|
| `/chat <message>` | Send prompt to active conversation |
| `/new <project_dir> [title]` | Start new conversation |
| `/switch <conversation_id>` | Switch active conversation |
| `/history` | Last 10 messages |
| `/projects` | List project directories |
| `/status` | Command Centre status |
| `/export` | Export conversation as .md file |
| `/pin <code>` | Unlock full agent mode (15 min TTL) |

### Security
- Only responds to configured `discord_user_id`
- PIN via `/pin` command, stored in memory with configurable TTL
- Bot runs as goroutine within Go backend process

### Response Handling
- < 2000 chars: Discord message with markdown
- >= 2000 chars: .md file attachment
- Streaming: "Thinking..." message, edited with final response

## Frontend — ChatPage

New page in React app with sidebar navigation (MessageSquare icon).

### Layout
- Left panel (250px): conversation list grouped by project, [+ New Chat] button
- Main area: message bubbles (user=right/rain-blue, assistant=left/glass-card, system=centered)
- Input bar: text area, project selector dropdown, PIN toggle (lock icon)

### Features
- Real-time streaming via WebSocket `chat.stream` events
- Source badges (UI/Discord) on each message
- File changes indicator (collapsible list)
- Metadata (duration, tokens) after each response
- Export button per conversation
- Orchestrator identity: responses labeled "Soldier of God"

## Settings Page — Chat Config Section

| Setting | Type | Description |
|---------|------|-------------|
| Discord Bot Token | Password field | Encrypted in DB, never exposed in API |
| Discord User ID | Text field | Your Discord ID |
| Security PIN | Password field | Hashed, requires old PIN to change |
| Default Project Dir | Dropdown | Pre-populated from projects table |
| Discord Status | Read-only | Green/red dot + reconnect button |
| Bot Invite URL | Read-only + copy | Auto-generated OAuth2 URL |
| PIN Timeout | Number (minutes) | Default: 15 |

## Decision Log

| # | Decision | Alternatives | Rationale |
|---|----------|-------------|-----------|
| 1 | Claude Code CLI as backend | Anthropic API, MCP bridge | Zero cost, existing subscription |
| 2 | Single Go binary | Separate microservice | Single user, minimal complexity |
| 3 | Full agent mode | Chat only, read-only | Remote code editing needed |
| 4 | UI + Discord shared history | UI only | Full experience both places |
| 5 | PIN for destructive actions | User ID only, no security | Balanced security |
| 6 | --allowedTools restriction | Pause mid-execution | Simpler, no interruption |
| 7 | 15-min PIN TTL | Per-message, session-based | Convenient for work bursts |
| 8 | Per-project queue | Global queue, fully parallel | CLI directory limitation |
| 9 | Persistent + exportable | Session only | Searchable, survives restarts |
| 10 | discordgo in goroutine | Separate bot process | Single process, shared DB |
| 11 | Orchestrator: "Soldier of God" | Generic "Assistant" | Established identity |

## Implementation Order

1. Database migrations (conversations, messages, chat_config tables)
2. Chat executor (claude CLI subprocess management)
3. Queue manager (per-project queuing)
4. Chat API endpoints (conversations CRUD + /chat)
5. WebSocket chat events (chat.stream, chat.complete, chat.pin_required)
6. Discord bot (discordgo integration + slash commands)
7. ChatPage.tsx (React UI)
8. Settings page updates (chat config section)
9. Export functionality (md/json)
10. Testing and verification
