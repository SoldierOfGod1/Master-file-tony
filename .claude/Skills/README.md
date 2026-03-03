# Skills Directory

Skills are reusable logic modules that agents can invoke for project-internal operations.

## Conventions

- **Naming**: kebab-case, domain-prefixed.
  - Examples: `rain-auth-validate`, `db-migrate-preview`, `api-generate-client`
- **Scope**: Project-internal reusable logic only. External integrations use MCP tools.
- **One skill = one responsibility**. Compose skills for complex workflows.

## File Structure

```
.claude/Skills/
  README.md                    # This file
  rain-auth-validate/
    skill.md                   # Skill definition (purpose, inputs, outputs)
    implementation.py          # Skill logic
    tests/
      test_skill.py            # Skill tests
  db-migrate-preview/
    skill.md
    implementation.sh
```

## Skill Definition (`skill.md`)

```markdown
# skill-name

## Purpose
One-line description.

## Inputs
| Param | Type | Required | Description |
|-------|------|----------|-------------|
| ... | ... | ... | ... |

## Outputs
| Field | Type | Description |
|-------|------|-------------|
| ... | ... | ... |

## Usage
\`\`\`
[How to invoke this skill from an agent]
\`\`\`

## Owner Agent
Agent-XX (primary), Agent-YY (backup)
```

## Registration

After creating a skill, register it in `.claude/ENGINEERING_LOG.md` under the Skills Registry section.
