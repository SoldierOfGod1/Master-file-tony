# MCP Conventions

Rules for Model Context Protocol (MCP) tool registration and usage across all agents.

## Server Configuration

- All MCP servers are declared in `.mcp.json` at project root.
- Prefer **HTTP transport** (`streamable-http`) over stdio for production tools.
- Never put long MCP tool definitions in `CLAUDE.md` — reference `.mcp.json` instead.

## Tool Naming

- Use **verb-noun** pattern: `create-user`, `validate-schema`, `run-migration`.
- Prefix with domain when ambiguous: `auth-validate-token`, `db-run-query`.
- Kebab-case only. No camelCase or snake_case in tool names.

## Required Fields per Tool

Every MCP tool definition must include:

| Field | Required | Description |
|-------|----------|-------------|
| `purpose` | Yes | One-line description of what the tool does |
| `input_schema` | Yes | JSON Schema for inputs |
| `output_schema` | Yes | JSON Schema for outputs |
| `input_examples` | Yes (3+) | At least 3 example inputs |
| `error_codes` | Yes | Expected error responses |

## Example Tool Definition

```json
{
  "name": "db-run-query",
  "purpose": "Execute a read-only SQL query against the project database",
  "input_schema": {
    "type": "object",
    "properties": {
      "query": { "type": "string", "description": "SQL SELECT query" },
      "database": { "type": "string", "enum": ["primary", "analytics"] }
    },
    "required": ["query"]
  },
  "output_schema": {
    "type": "object",
    "properties": {
      "rows": { "type": "array" },
      "row_count": { "type": "integer" }
    }
  },
  "input_examples": [
    { "query": "SELECT COUNT(*) FROM users" },
    { "query": "SELECT id, name FROM products WHERE active = true", "database": "primary" },
    { "query": "SELECT date, revenue FROM daily_stats ORDER BY date DESC LIMIT 30", "database": "analytics" }
  ]
}
```

## Verification

After any MCP configuration change, run `/mcp list` to verify all servers are reachable and tools are registered correctly.

## Safety

- MCP tool calls that modify external state must follow core-safety.md rules.
- Log all MCP tool invocations to `.claude/log/mcp-calls.jsonl`.
- Rate-limit external MCP calls to prevent runaway costs.
