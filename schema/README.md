# Schema Directory

Shared schema definitions for the project. All agents reference schemas here as the single source of truth.

## Conventions

- **OpenAPI**: 3.1.0 for all API specs (kept in `.claude/spec/03-apis/`).
- **JSON Schema**: Draft 2020-12 for data models.
- **Naming**: `kebab-case` file names, e.g. `user-profile.schema.json`.
- **Never duplicate**: If a schema exists here, reference it — don't copy it into agent output.

## Versioning

```
schema/
  v1/
    user.schema.json
    order.schema.json
  v2/
    user.schema.json      # breaking change → new version dir
```

- Non-breaking additions go in the current version directory.
- Breaking changes require a new `vN/` directory and an ADR in `docs/adr/`.

## Validation

All schemas should be validated against their meta-schema before commit. Agents must generate schemas from source of truth (database models, API specs), never fabricate them.
