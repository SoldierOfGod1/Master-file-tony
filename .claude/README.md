# Master Project Template — Claude Code Multi-Agent Orchestration v9.0.0

## Core Philosophy

1. **Composability** — One agent = one responsibility. Combine freely.
2. **Explicitness** — Every decision traceable to a spec, ADR, or brief.
3. **Safety & Observability** — Structured logs, metrics, tracing on every service.
4. **Minimal Breakage Surface** — Version tools and APIs early.
5. **Documentation-First** — Specs before code, ADRs before refactors.

---

## Quick Start (For New Users)

### Step 1: Clone and Setup Your Project
```bash
# Clone this template
git clone https://git.rain.network/oi-team/paul/claude_project_template_master my-awesome-project
cd my-awesome-project

# Remove template git history and initialize as YOUR project
rm -rf .git
git init
```

### Step 2: Customize Repository Settings
Update `CLAUDE_CODE.md` section 15 with your git server:
```yaml
# Change from:
GIT_SERVER=git.rain.network
GIT_ORG=oi-team/paul
# To your server/org:
GIT_SERVER=github.com
GIT_ORG=yourusername
```

### Step 3: Configure MCP (Optional)
Edit `.mcp.json` at project root to register your project-specific MCP tool servers. See `.claude/rules/mcp-conventions.md` for naming conventions.

### Step 4: Start Building
```bash
# Launch Claude Code
claude .

# Begin!
> Read PROJECT_PLAN.md and act as Agent-01
> OK_TO_SPAWN
```

---

## The One Workflow (Always Parallel, Fully Automated)

### 1. Define Project (5 minutes)
```bash
# Write your project brief (only file you create!)
# Edit .claude/spec/00-project-brief.md
# Include: project description, features, tech preferences
```

### 2. Launch Parallel Execution (10 seconds)
```bash
# Start Claude Code
claude .

# In Claude, type:
> Read PROJECT_PLAN.md and act as Agent-01

# Review plan, then:
> OK_TO_SPAWN

# 20+ agents now working in parallel
```

### 3. Monitor & Control
```bash
# Control tokens (anytime):
> HALT                    # Pause all agents
> Add feature X. UPDATE AND RESUME  # Inline updates
> STATUS                  # Check progress
```

---

## Six Work Areas

| # | Area | Primary Agents | Output Location |
|---|------|---------------|-----------------|
| 1 | Architecture Decision | 01, 02, 03 | `docs/adr/`, `ENGINEERING_LOG.md` |
| 2 | Backend Service | 04, 09, 10 | `backend/`, `.claude/spec/03-apis/` |
| 3 | Frontend Service | 07, 08, 02 | `frontend/` |
| 4 | Database Schema | 05, 11 | `database/`, `schema/` |
| 5 | API Contract | 04, 09, 01 | `.claude/spec/03-apis/api-design.yaml` |
| 6 | Deployment Workflow | 06, 03, 12 | `docker/`, `kubernetes/`, CI/CD |

---

## What Actually Happens

When you type `OK_TO_SPAWN`, this parallel execution begins:

```
Phase 1 (6 agents):  Research, Database, Visuals       <- Start immediately
    |
Phase 2 (10 agents): Backend x4, Frontend x4, Integration x2  <- All parallel
    |
Phase 2.5:           MCP verification (/mcp list)
    |
Phase 3 (3 agents):  DevOps, AI/ML, DataScience       <- Specialized
    |
Phase 4 (12 agents): Security x4, Testing x8          <- Quality gates
    |
AUTO-DEPLOY: Ready for production!
```

**Peak parallelism: 20 agents working simultaneously**

## What You Get

```
my-project/
  CLAUDE.md              # Root entry point + Core Philosophy
  .mcp.json              # MCP server configuration
  .claude/
    PROJECT_PLAN.md      # Orchestration entry point
    CLAUDE_CODE.md       # Agent contract & protocols
    README.md            # This file
    ENGINEERING_LOG.md   # Living project log
    agents/              # 14 agent specifications
    hooks/               # Safety validation hooks
    rules/               # Safety, MCP, observability rules
    Skills/              # Reusable logic modules
    spec/                # Project specs & API design
  backend/               # Complete API with all business logic
  frontend/              # Frontend app (display only, no logic)
  database/              # Schema, migrations, seeds
  schema/                # Shared schemas (OpenAPI, JSON Schema)
  docs/adr/              # Architecture Decision Records
  docker/                # Containerized services
  kubernetes/            # Production deployment
  scripts/               # Utility scripts
```

## Built-in Safety

- **System Protection**: Cannot harm your computer (see SYSTEM_PROTECTION.md)
- **Project Isolation**: Each project in its own venv
- **Safety Hooks**: PreToolUse validation on every command
- **Core Safety Rules**: No destructive ops without confirmation
- **Git Integration**: Auto-commits with SSH key
- **State Recovery**: HALT/RESUME anytime

## Control Tokens (Use Anytime)

| Token | What it Does |
|-------|-------------|
| `OK_TO_SPAWN` | Start parallel execution |
| `HALT` | Pause everything |
| `UPDATE AND RESUME` | Make changes inline |
| `STATUS` | Show all agent progress |
| `ROLLBACK` | Undo to checkpoint |

## 14 Specialized Agents

1. **Agent-01**: Orchestrator (reads your brief, creates plan)
2. **Agent-02**: Product Research (3 instances)
3. **Agent-03**: Security Gate (4 instances)
4. **Agent-04**: Backend Services (6 instances)
5. **Agent-05**: Database Design (6 instances)
6. **Agent-06**: DevOps (3 instances)
7. **Agent-07**: Frontend (6 instances) — framework per project brief
8. **Agent-08**: Visualization (4 instances)
9. **Agent-09**: Integration (3 instances)
10. **Agent-10**: AI/ML Platform (5 instances)
11. **Agent-11**: Data Science (8 instances)
12. **Agent-12**: Testing (20 instances)
13. **Agent-13**: Cloud Migration (special-use only)
14. **Template**: Minimal agent template for custom agents

**Total capacity: 75 instances | Max parallel: 20**

## MCP Setup

MCP servers are configured in `.mcp.json` at project root. After changes, verify with `/mcp list`.

See `.claude/rules/mcp-conventions.md` for naming and schema requirements.

## Template Customization Checklist

When using this template for YOUR projects, update these locations:

| File | What to Change |
|------|---------------|
| `CLAUDE_CODE.md` section 15 | `GIT_SERVER` and `GIT_ORG` |
| `.claude/spec/00-project-brief.md` | Your project details |
| `.mcp.json` | Your MCP tool servers |

### Optional Customizations:
- **Agent limits**: Edit agent markdown files for different instance counts
- **Tech stack**: Update agent files and project brief for your preferred technologies
- **Company name**: Replace "rain" / "oi-team" references with your organization

---

**Version**: 9.0.0 | **Parallel by Default** | **Zero Friction** | **Full Automation**

## Template Repository

This template is maintained at: https://git.rain.network/oi-team/paul/claude_project_template_master

To get updates:
```bash
git pull https://git.rain.network/oi-team/paul/claude_project_template_master master
```
