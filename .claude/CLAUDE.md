# CLAUDE.md — Project Root Entry Point (v9.0)

> **Default org**: rain (rain.co.za) — change in `.claude/spec/00-project-brief.md` for other orgs.

---

## Operating Standard — Boil the Ocean (read first)

**The marginal cost of completeness is near zero with AI. Do the whole thing.
Do it right. Do it with tests. Do it with documentation.** The bar is
*"holy shit, that's done"*, not *"good enough"*.

This applies to every task, every session, every spawned subagent. Agent-01
(Soldier of God) propagates this to every spawn it makes; direct sessions
inherit it from this file.

**Never:**
- Offer to "table this for later" when the permanent fix is within reach.
- Leave a dangling thread when tying it off takes five more minutes.
- Present a workaround when the real fix exists.
- Hand back a plan when the user asked for the finished product.

**Always:**
- Search before building (existing patterns, std lib, prior code).
- Test before shipping (unit + integration + smoke probe end-to-end).
- Ship the complete thing — backend + frontend + docs + tests + restart + verify.
- The answer is the working, tested, documented product — not a plan.

**Acceptable to defer only when:** the blocker is genuinely external (the user's
VPN, a missing token, a third-party outage, an explicit user-input gate). Name
the blocker, state the unblock action, do not dress deferral up as iteration.

Time, fatigue, complexity are not excuses. See full text at
`~/.claude/projects/<project>/memory/feedback_completeness_standard.md`.

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

---

## Skills: rain-first, gstack for product pipeline

This repo has **two skill layers**. Pick by domain, not by habit.

### rain/Axiom work — ALWAYS use rain-specific tooling first

Anything touching rain BSS, Axiom, Snowflake middleware, customer/billing/payment data:

- **Axiom Explorer** at `/axiom` (backend `/api/v1/axiom/*`) is the source of truth. Cross-DB joins walk per-domain databases (`party`, `account`, `customer`, `product`, `service`, `resource`, `snowflake`, …).
- Local catalogues: `docs/axiom/axiom-prod-catalogue.json`, `docs/axiom/axiom-prod-columns.json`, `docs/axiom/axiom-prod-summary.md`. Regenerate with `python scripts/crawl-axiom.py`.
- **Writes route through Snowflake middleware** — NEVER UPDATE/INSERT/DELETE directly on Axiom tables. Every `/api/v1/axiom/*` endpoint is read-only with PII redaction baked in.
- Memory: the `reference_axiom_prod.md` memory records confirmed table paths, cross-DB join keys, and the 83-row endpoint→Axiom map in `backend/internal/axiom/correlate.go`.

### Everything else — gstack pipeline

`gstack` (Garry Tan, MIT, `~/.claude/skills/gstack/`) provides the product/process pipeline. Skills chain via design docs: each skill writes an artefact the next reads.

**Pipeline**: `Think → Plan → Build → Review → Test → Ship → Reflect`

| Phase | Use |
|---|---|
| Think | `/office-hours` (6-question YC interrogation → design doc) |
| Plan | `/autoplan` or `/plan-ceo-review` → `/plan-eng-review` → `/plan-design-review` → `/plan-devex-review` |
| Design | `/design-consultation` (greenfield), `/design-shotgun` (variants), `/design-html` (production HTML) |
| Build | Direct work, honouring frozen scope via `/freeze` + `/guard` + `/careful` |
| Review | `/review` (staff engineer), `/cso` (OWASP+STRIDE, 8/10 confidence gate, exploit scenarios) |
| Debug | `/investigate` (Iron Law: no fixes without root cause, 3-failed-fix stop) |
| Test | `/qa` (real browser + atomic fix commits + auto-regression tests), `/qa-only` (report only), `/benchmark` (Web Vitals baseline) |
| Ship | `/ship` (sync, test, push, PR), `/land-and-deploy` (merge → CI → deploy → verify), `/canary` (post-deploy monitoring loop) |
| Reflect | `/retro` (weekly), `/document-release` (release notes), `/learn` |

**Precedence**: when a gstack skill and a rain-specific tool could both apply to rain/BSS work, the rain tool wins. When no rain tool exists (anything product, process, design, deploy, retro), use gstack.

### Upgrading gstack

`~/.claude/skills/gstack/bin/gstack-update-check` runs on skill invocation (throttled, failure-safe). Manual upgrade: `/gstack-upgrade`.
