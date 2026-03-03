# ENGINEERING_LOG — Project Log (v9.0)

> Single source of truth for plan, work, tests, approvals, and release evidence.

## 0) Project & Ownership
- **Project:** <name>
- **Repo:** <url>
- **Owner (Eng/PM):** <names>
- **Dates:** <start -> target>

## 1) Discovery & Decisions
- Key decisions (short, dated bullets)
- Links to specs/diagrams

## 2) Architecture & Boundaries
- Services, APIs/events
- Data (schemas, migrations, partitions)
- Security (authn/z, CSP, CORS)

## 3) Risks & Mitigations
- R1: <risk> -> <mitigation>
- R2: <risk> -> <mitigation>

## 4) Implementation Plan (For Approval)
- Scope & success metrics
- Task DAG (critical path)
- Team & roles

## 5) Spawn Plan
- Inline `[SPAWN ...]` lines or a link to `spawn.plan.yaml` (Mode B)

## 6) Work Log
| Time | Agent#Inst | Summary | Artifacts | Commit |
|------|------------|---------|-----------|--------|
| | | | | |

## 7) Testing Evidence (Gates)
- **Agent-03 (Quality/Security):** reviews, SAST, headers/CSP evidence
- **Agent-12 (E2E/Perf):** Playwright reports, K6 summaries

## 8) Approvals (Required)
- [ ] Agent-03
- [ ] Agent-12
- [ ] Product/Owner sign-off

## 9) Release Notes
- Version, changes, migrations

## 10) Rollback Plan
- How to revert safely

## 11) Post-release Review
- What worked / what we improve next

## 12) Observability
- **Structured Logging**: Confirm JSON log format deployed per `.claude/rules/observability.md`
- **Metrics**: Confirm latency, error rate, throughput metrics exposed
- **Tracing**: Confirm OpenTelemetry trace propagation across services
- **Health Checks**: Confirm `/health` endpoint on every service

## 13) Architecture Decisions (ADRs)
| # | Title | Status | Date | Link |
|---|-------|--------|------|------|
| | | | | See `docs/adr/` |

## 14) MCP Tools Registry
| Tool Name | Server | Purpose | Registered By |
|-----------|--------|---------|---------------|
| | | | |

## 15) Skills Registry
| Skill Name | Owner Agent | Purpose | Path |
|------------|------------|---------|------|
| | | | See `.claude/Skills/` |
