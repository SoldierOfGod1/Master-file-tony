# Project Specifications

## Simplified Structure

### 1. `00-project-brief.md` (User fills this)
The core document that drives everything. Keep it under 1 page.
Includes: project description, tech stack, MCP tools needed, observability requirements.

### 2. `01-technical-spec.md` (Agent-01 fills this)
Technical decisions based on the brief. Includes ADR references and observability plan.

### 3. `02-implementation-notes.md` (Agent-01 fills this)
Phases (starting with Phase 0: MCP Setup), risks, and dependencies.

### 4. `03-apis/api-design.yaml` (Agent-04 fills this)
OpenAPI 3.1.0 specification for the backend. Includes `/health` and `/metrics` endpoints.

### 5. `03-apis/README.md`
API contract conventions and rules.

## Optional Files
- `cloud-migration-brief.md` — Only when migrating between cloud providers
- `00-vX-update.template.md` — Template for versioned update briefs

## How It Works

1. **User** fills only `00-project-brief.md`
2. **Agent-01** reads the brief and generates:
   - Technical specification (with observability plan)
   - Implementation notes (starting with MCP setup)
   - Task breakdown in ENGINEERING_LOG.md
   - Spawn plan for other agents
3. **Agent-04** creates the API design (OpenAPI 3.1.0)
4. **All agents** work from these concise documents

## Tips for Project Brief
- Keep it under 30 lines
- Focus on WHAT not HOW
- List only MVP features (3-5 max)
- Specify timeline clearly
- State constraints upfront
- Include MCP tools if needed
- Specify observability preferences

This simplified structure reduces cognitive load and ensures agents focus on building, not reading.
