# CLAUDE.md — Project Root Entry Point (v9.0)

> **Default org**: rain (rain.co.za) — change in `.claude/spec/00-project-brief.md` for other orgs.

---

## Core Philosophy

1. **Composability** — Small, focused agents and tools that combine freely. One agent = one responsibility.
2. **Explicitness** — No magic. Every decision is traceable to a spec, ADR, or project brief.
3. **Safety & Observability** — Structured logs, metrics, tracing on every service. Pre-flight checks before destructive ops.
4. **Minimal Breakage Surface** — Version tools and APIs early. Never break a running contract without an ADR.
5. **Documentation-First** — If it isn't written down, it doesn't exist. Specs before code, ADRs before refactors.

---

## Quick Start

1. Fill in `.claude/spec/00-project-brief.md` (the only file you must write).
2. Run `claude .` and tell Agent-01 to read `PROJECT_PLAN.md`.
3. Review the plan, then type `OK_TO_SPAWN`.

Full orchestration docs: [`.claude/README.md`](.claude/README.md)

---

## Key Pointers

| Document | Purpose |
|----------|---------|
| `.claude/PROJECT_PLAN.md` | Orchestration entry point & spawn config |
| `.claude/CLAUDE_CODE.md` | Agent contract, protocols, rules |
| `.claude/README.md` | Setup guide & template overview |
| `.claude/spec/00-project-brief.md` | Your project definition (fill this) |
| `.claude/ENGINEERING_LOG.md` | Living log of decisions, work, releases |
| `.mcp.json` | MCP server configuration |
| `docs/adr/` | Architecture Decision Records |
| `schema/` | Shared schemas (OpenAPI, JSON Schema) |

---

## MCP Configuration

MCP servers are declared in `.mcp.json` at project root. See `.claude/rules/mcp-conventions.md` for naming and schema requirements.

---

## Anti-Patterns (Quick Reference)

- Do NOT put long MCP tool definitions in CLAUDE.md — use `.mcp.json`.
- Do NOT vibe-code — always trace work back to a spec or ADR.
- Do NOT skip observability — every service needs structured logs + health check.
- Do NOT fabricate schemas — generate from source of truth or validate against `schema/`.
- Do NOT exceed 8 tool calls in a single agent turn without `/compact`.

---

## Six Work Areas

| # | Area | Primary Agents | Output |
|---|------|---------------|--------|
| 1 | Architecture Decision | 01, 02, 03 | `docs/adr/`, `ENGINEERING_LOG.md` |
| 2 | Backend Service | 04, 09, 10 | `backend/`, `.claude/spec/03-apis/` |
| 3 | Frontend Service | 07, 08, 02 | `frontend/` |
| 4 | Database Schema | 05, 11 | `database/`, `schema/` |
| 5 | API Contract | 04, 09, 01 | `.claude/spec/03-apis/api-design.yaml` |
| 6 | Deployment Workflow | 06, 03, 12 | `docker/`, `kubernetes/`, CI/CD |
