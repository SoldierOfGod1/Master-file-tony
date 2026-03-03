# CLAUDE_CODE.md — Orchestration Contract (rain, v9.0)

This file defines how all Claude Code agents operate together.

---

## 0) Core Philosophy

1. **Composability** — Small, focused agents and tools that combine freely. One agent = one responsibility.
2. **Explicitness** — No magic. Every decision is traceable to a spec, ADR, or project brief.
3. **Safety & Observability** — Structured logs, metrics, tracing on every service. Pre-flight checks before destructive ops.
4. **Minimal Breakage Surface** — Version tools and APIs early. Never break a running contract without an ADR.
5. **Documentation-First** — If it isn't written down, it doesn't exist. Specs before code, ADRs before refactors.

---

## 1) Modes of Operation

- **Mode A — Claude Code Only (default)**
  Agent-01 orchestrates from PROJECT_PLAN.md, spawning specialist agents in parallel.
  Terminal stays minimal; logs/agents.jsonl is the heartbeat file.

- **Mode B — SDK Orchestrator (optional)**
  Use agentctl.py + spawn.plan.yaml to fan-out tasks via SDK.
  Same rules apply.

---

## 2) Speaking & Status Protocol

All agents (02–13) must follow:

```
[AGENT-XX#i STARTING: <short task>]
... concise steps, file paths, small diffs ...
ARTIFACTS:
- <path/to/file>
[AGENT-XX#i COMPLETE]
```

- **Terminal STATUS one-liners only**
  e.g. `STATUS: 04#2 tests pass — user-service`

- **Heartbeat JSONL** (`logs/agents.jsonl`):
  ```json
  {"ts":"2026-03-03T12:00:00Z","agent":"04","inst":2,"status":"working","task":"user-service APIs","elapsed":"00:04:12"}
  ```

- **Auto-commit hand-off**: write <=72-char one-liner to `.claude/commits/AGENT-XX#i.msg`.

---

## 3) Planning Gate (Agent-01 only)

Before any code:
1. Implementation Plan (ENGINEERING_LOG.md section 4).
2. Task Assignment Table (ENGINEERING_LOG.md section 5).
3. Spawn Plan.

No coding until this gate is approved.

---

## 4) Spawn / Sync / Merge

- Only Agent-01 may spawn.
- Specialists never spawn others.
- SYNC: Agent-01 summarises progress, updates ENGINEERING_LOG.md.
- MERGE blocked until Agent-03 + Agent-12 are green.

---

## 5) Scaling Policy

| Agent | Max Instances |
|-------|---------------|
| 01 Orchestrator | 1 |
| 02 Product/UX | 3 |
| 03 Quality/Security | 4 |
| 04 Backend | 6 |
| 05 Data Platform | 6 |
| 06 DevOps | 3 |
| 07 Frontend | 6 |
| 08 Viz/Mapping | 4 |
| 09 Integration | 3 |
| 10 AI/ML | 5 |
| 11 Data Science | 8 |
| 12 Test/Perf | 20 |
| 13 Cloud Migration | 2* |

*Agent-13 is ONLY spawned when user explicitly requests cloud migration

---

## 6) MCP Integration Rules

- All MCP servers are declared in `.mcp.json` at project root.
- Every MCP tool must have: `purpose`, `input_schema`, `output_schema`, 3+ `input_examples`.
- Prefer **HTTP transport** (`streamable-http`) over stdio for production tools.
- Use **verb-noun** naming: `create-user`, `validate-schema`, `run-migration`.
- After any MCP configuration change, run `/mcp list` to verify.
- Never put long MCP tool definitions in CLAUDE.md — reference `.mcp.json`.
- See `.claude/rules/mcp-conventions.md` for full conventions.

---

## 7) Skills & Hooks Contract

### Skills
- Reusable logic modules in `.claude/Skills/`.
- Naming: kebab-case, domain-prefixed (e.g., `rain-auth-validate`).
- One skill = one responsibility. Compose for complex workflows.
- Register in ENGINEERING_LOG.md Skills Registry.

### Hooks
- **PreToolUse**: Block dangerous ops via `safety-check.sh` / `safety-check.ps1`.
- **PostToolUse**: Auto-format, lint after tool execution.
- **OnError**: Capture stack, write to `.claude/log/errors.jsonl`, notify orchestrator.
- **Never silently fail** — if a hook errors, it must log and exit non-zero.
- See `.claude/hooks/README.md` for full documentation.

---

## 8) Platform Building Rules

- **One CLAUDE.md per domain** — if the project has multiple services, each can have its own CLAUDE.md within its directory.
- **Observability required** — every service must implement structured JSON logging, expose `/health` endpoint, and emit metrics. See `.claude/rules/observability.md`.
- **Max 3 subagent nesting levels** — Agent-01 spawns agents, but agents must not spawn sub-sub-agents deeper than 3 levels.
- **Version tools early** — APIs, MCP tools, and schemas get a version from day one (`v1/`, `v2/`).

---

## 9) Anti-Patterns & Gotchas

| Anti-Pattern | Why It's Bad | Do This Instead |
|-------------|-------------|-----------------|
| Long MCP defs in CLAUDE.md | Bloats context window | Put in `.mcp.json` |
| Vibe-coding (no spec) | Untraceable decisions | Spec or ADR first |
| Unvalidated tool output | Silent corruption | Validate against `schema/` |
| 8+ tool calls without compact | Context degradation | Run `/compact` after 8 calls |
| Fabricated schemas | Data contract drift | Generate from source of truth |
| Skipping observability | Blind in production | Structured logs + health check |
| Force-push without ADR | Lost history | ADR + normal push |

---

## 10) Evidence & Logs

ENGINEERING_LOG.md must track:
- Implementation Plan (section 4)
- Spawn Plan (section 5)
- Work Log (section 6)
- Testing Evidence (section 7)
- Approvals (section 8)
- Release Notes (section 9)
- Rollback (section 10)
- **Observability** (section 12) — confirm structured logging, metrics, health checks deployed

---

## 11) Orchestrator Control Header

Each PROJECT_PLAN.md must include:

```orchestrator
ROLE=Agent-01
PROTOCOL=SPAWN,SYNC,MERGE,HALT
PARALLEL=true
HEARTBEAT_SECONDS=30
TERMINAL_OUTPUT=minimal
STREAM_CODE_TO_TERMINAL=false
LOG_FILE=logs/agents.jsonl
SPECS=CLAUDE_CODE.md,.claude/agents/*.md,.claude/spec/**/*.md,.claude/spec/**/*.yaml
GATES=Agent-03,Agent-12
MODE=PLAN_ONLY
```

---

## 12) Security & Secrets

- Never output secrets or tokens.
- Redact PII unless explicitly approved.
- See `.claude/rules/core-safety.md` for full safety rules.
- See `.claude/SYSTEM_PROTECTION.md` for blocked operations.

---

## 13) Failure Handling

- If plan invalid or caps exceeded: HALT, checklist, REPLAN.
- If gates fail: no merge. Respawn focused fixes.

---

## 14) Mid-Flow Control Tokens

- **HALT** — stop immediately, save state.
- **REPLAN** — re-read plan + specs, draft new plan.
- **RESUME** — continue from saved checkpoint.
- **OK_TO_SPAWN** — approve plan execution.
- **UPDATE** — read update brief (00-vX-update.md), spawn incremental changes.
- **RESET** — clear state and restart from PROJECT_PLAN.md.
- **ABORT** — terminate workflow; summarise and close.
- **MERGE_TO_MAIN** — manual trigger to merge development to main branch.

---

## 15) Git Workflow Protocol

### Repository Creation
- **Repository**: `https://${GIT_SERVER}/${GIT_ORG}/<project_name>`
- **Default**: `GIT_SERVER=git.rain.network`, `GIT_ORG=oi-team/paul`
- **Branches**:
  - `main` - Production ready code
  - `development` - Integration branch
  - `development/phase-X-<feature>` - Feature branches per phase

### Commit Flow
- **First Commit**: To main branch, then switch to development
- **Feature Work**: On development/phase branches
- **Gate Merges**: Auto-merge to development after Agent-03/12 approval
- **Production Merge**: Manual to main via `MERGE_TO_MAIN` token

### SSH Configuration
- **Cross-platform**: Works on Linux VM, Windows WSL, Docker
- **Auto-push**: Enabled with stored credentials

---

## 16) Control Token Handling

### Agent Response Protocol
All agents must handle control tokens as follows:

**OK_TO_SPAWN Response (Agent-01 ONLY):**
- **IMMEDIATELY** update PROJECT_PLAN.md line 16: `MODE=PLAN_AND_EXECUTE`
- Begin spawning agents according to parallel execution groups
- Report: `[MODE UPDATED TO PLAN_AND_EXECUTE - SPAWNING GROUPS]`
- No additional user confirmation needed

**HALT Response:**
- Save current state to `.claude/state/agent-XX-instance-Y.json`
- Stop all operations immediately
- Report: `[AGENT-XX#i HALTED: state saved]`

**REPLAN Response:**
- Clear current task queue
- Wait for new instructions
- Report: `[AGENT-XX#i READY FOR REPLAN]`

**RESUME Response:**
- Load state from checkpoint
- Continue without duplication
- Report: `[AGENT-XX#i RESUMING: <task>]`

**UPDATE Response (Two Modes):**

**Mode 1 - Versioned Update (Major Changes):**
- User creates `.claude/spec/00-v1-update.md` (then v2, v3, etc.)
- User types: `UPDATE`
- Agent-01:
  1. Reads versioned update file
  2. Updates all spec files with new requirements
  3. Updates ENGINEERING_LOG.md with changes
  4. Updates outstanding tasks list
  5. Spawns agents for incremental work
- Report: `[UPDATE v<X> LOADED - SPECS UPDATED - SPAWNING]`

**Mode 2 - Inline Update (Quick Changes):**
- User types: `HALT` then changes then `UPDATE AND RESUME`
- Agent-01 processes inline without spec file
- Report: `[INLINE UPDATE - RESUMING]`

### State Preservation
- Auto-save every 5 minutes
- JSON format with task progress
- Stored in `.claude/state/` directory

---
