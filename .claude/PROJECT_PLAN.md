# PROJECT_PLAN.md — Multi-Agent Orchestration Entry Point

```orchestrator
ROLE=Agent-01
PROTOCOL=SPAWN,SYNC,MERGE,HALT
PARALLEL=true
TERMINAL_OUTPUT=minimal
STREAM_CODE_TO_TERMINAL=false
VERBOSE=false
LOG_FILE=logs/agents.jsonl
STATE_DIR=.claude/state
COMMITS_DIR=.claude/commits
# Include core governance + agents, and your spec pack under .claude/spec/
SPECS=CLAUDE_CODE.md,.claude/agents/*.md,.claude/spec/**/*.md,.claude/spec/**/*.yaml
GATES=Agent-03,Agent-12
MODE=PLAN_ONLY            # change to PLAN_AND_EXECUTE after you approve the plan
ROOT_CONSTRAINT=true      # Constrain all operations to project root
MAX_PARALLEL_SPAWNS=20    # Optimal for true parallelism (not 10!)
SPAWN_STRATEGY=dependency_aware  # Spawn as soon as dependencies met
```

## Parallel Execution Directive
Agent-01, you are the orchestrator. Execute the following PARALLEL workflow:

### Phase 1: Planning (Sequential)
1. Read `.claude/spec/00-project-brief.md`
2. Generate technical specs and implementation plan
3. Create spawn plan with parallel execution groups
4. **WAIT for user approval**

### CRITICAL: OK_TO_SPAWN Behavior
When user types `OK_TO_SPAWN`:
1. **IMMEDIATELY update MODE from PLAN_ONLY to PLAN_AND_EXECUTE in this file**
2. **Then begin parallel spawning**
3. **No additional user confirmation needed**

### Phase 2: Parallel Spawning (After OK_TO_SPAWN)
**AUTOMATICALLY SPAWN THESE GROUPS IN PARALLEL:**

**Group A (No Dependencies - Start Immediately):**
- Agent-02 (1-3 instances): Product research
- Agent-05 (1-2 instances): Database design
- Agent-08 (1-2 instances): Visualization prep

**Group B (After Group A completes):**
- Agent-04 (2-4 instances): Backend services IN PARALLEL
- Agent-07 (2-4 instances): Frontend components IN PARALLEL
- Agent-09 (1-2 instances): Integration planning IN PARALLEL

### Phase 2.5: MCP Verification
After spawning, verify MCP tools are available:
- Run `/mcp list` to confirm all registered servers respond
- Log MCP tool inventory to ENGINEERING_LOG.md
- If any MCP server fails, log warning and continue with fallback

**Group C (After Group B):**
- Agent-06 (1 instance): DevOps setup
- Agent-10 (1-2 instances): AI/ML if needed
- Agent-11 (1-2 instances): Data science if needed

**Group D (Gates - After Groups A-C):**
- Agent-03 (2-4 instances): Security review IN PARALLEL
- Agent-12 (4-8 instances): Testing IN PARALLEL

### Phase 3: Output Requirements
Write spawn plan to `ENGINEERING_LOG.md` with:
- **Parallel execution groups** clearly marked
- **Instance counts** per agent
- **Dependencies** between groups
- **Expected parallelism factor** (e.g., "10 agents running simultaneously")
- **Observability checkpoint**: confirm structured logging and health endpoints deployed

**Print summary showing PARALLEL EXECUTION plan.**
**Wait for `OK_TO_SPAWN` token.**

## OK_TO_SPAWN Protocol
When the user types `OK_TO_SPAWN`:
1. Agent-01 MUST update line 16 of this file: `MODE=PLAN_AND_EXECUTE`
2. Agent-01 then immediately begins spawning agents in parallel groups
3. No further user interaction needed - full automation begins

## Spec sources (Simplified)
- .claude/spec/00-project-brief.md  **<-- USER FILLS THIS (only required file)**
- .claude/spec/01-technical-spec.md  **<-- Agent-01 generates**
- .claude/spec/02-implementation-notes.md  **<-- Agent-01 generates**
- .claude/spec/03-apis/api-design.yaml  **<-- Agent-04 generates**

**IMPORTANT**: User only needs to fill 00-project-brief.md. Agent-01 generates the technical spec and implementation notes from the brief, then creates the spawn plan.

## Notes
- Keep terminal output minimal (status one-liners only). Long code/diffs go to files and are listed in **ARTIFACTS**.
- On completion, each agent writes a <=72-char line to `.claude/commits/AGENT-XX#i.msg` for auto-commit.
