# Architecture Decision Records (ADRs)

This directory contains Architecture Decision Records for the project.

## What is an ADR?

An ADR captures a significant architectural decision along with its context and consequences. ADRs are immutable once accepted — if a decision is reversed, write a new ADR that supersedes the old one.

## Process

1. Copy `000-template.md` to `NNN-short-title.md` (next sequential number).
2. Fill in all sections.
3. Set status to **Proposed**.
4. Get review from Agent-01 (architecture) and Agent-03 (security).
5. Update status to **Accepted** or **Rejected**.

## File Naming

```
001-use-postgresql-for-primary-db.md
002-adopt-event-driven-architecture.md
003-select-auth-provider.md
```

## Index

| # | Title | Status | Date |
|---|-------|--------|------|
| 000 | Template | N/A | — |

Add new ADRs to this index as they are created.
