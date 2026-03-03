# Hooks Directory

Hooks are shell scripts that execute in response to Claude Code events. They enforce safety, quality, and observability standards.

## Hook Types

### PreToolUse
Runs **before** a tool is executed. Use to block dangerous operations.

- **File**: `safety-check.sh` (Linux/Mac), `safety-check.ps1` (Windows)
- **Purpose**: Validate commands against blocklist, check paths are within project root, flag destructive operations.
- **Exit code 1** = block the operation.

### PostToolUse
Runs **after** a tool completes. Use for auto-formatting and linting.

- **Purpose**: Auto-format generated code, run linters, update logs.
- **Examples**: `prettier --write`, `black .`, `eslint --fix`

### OnError
Runs when an agent encounters an error. Use to capture diagnostics.

- **Purpose**: Capture stack trace, write to `.claude/log/errors.jsonl`, notify orchestrator.
- **Log format**:
  ```json
  {
    "timestamp": "2026-03-03T12:00:00Z",
    "agent": "04",
    "instance": 2,
    "error": "ConnectionRefusedError",
    "stack": "...",
    "context": "user-service API endpoint"
  }
  ```

## Rules

1. **Never silently fail** — if a hook encounters an error, it must log the error and exit with a non-zero code. Silent failures hide bugs.
2. **Keep hooks fast** — hooks run on every tool call. Target < 100ms execution time.
3. **Idempotent** — hooks may run multiple times for the same operation. Ensure no side effects on re-run.

## Integration with Safety Scripts

The `safety-check.sh` / `safety-check.ps1` scripts implement the PreToolUse pattern. They:
- Check commands against dangerous patterns (rm -rf, DROP, format, etc.)
- Verify operations stay within `${PROJECT_ROOT}`
- Log all commands to `.claude/log/command-audit.log`
- Block protected path access

See `.claude/rules/core-safety.md` for the full safety rule set.
