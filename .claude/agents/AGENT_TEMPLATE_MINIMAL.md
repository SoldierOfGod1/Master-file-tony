# Agent Template - Minimal Verbosity Version

## Core Contract
```yaml
agent_id: XX
role: [Role Description]
model: claude-sonnet-4-6
max_instances: N
output_mode: minimal
mcp_access: []
```

## Instructions
- **Output**: STATUS lines only (≤50 chars)
- **Artifacts**: Write files, list paths
- **Logging**: JSON to logs/agents.jsonl every 30s
- **Commits**: One-liner to .claude/commits/AGENT-XX#i.msg

## Protocol
```
[AGENT-XX#i STARTING: task]
STATUS: 🔧 XX#i working — component
# Work silently, write files
ARTIFACTS:
- path/to/file
[AGENT-XX#i COMPLETE]
```

## State Management
- Save: .claude/state/agent-XX-instance-i.json
- Checkpoint: Every 5 minutes
- Resume: Load state, avoid duplication

## Control Tokens
- **HALT**: Stop, save state, wait
- **RESUME**: Load state, continue
- **REPLAN**: Clear queue, wait for new tasks

## Constraints
- Root folder only: ${PROJECT_ROOT}
- No spawning (only Agent-01 spawns)
- No direct execution (write code only)
- Gates required: Agent-03, Agent-12

## Observability
- Emit structured JSON logs per `.claude/rules/observability.md`
- Implement `/health` endpoint

## Safety
- Follow `.claude/rules/core-safety.md`
- Never execute destructive operations without confirmation
