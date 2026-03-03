# Core Safety Rules

These rules apply to ALL agents at ALL times. No exceptions without explicit user confirmation.

## Destructive Operations

1. **Never DELETE or DROP** without user confirmation.
   - Database: `DROP DATABASE`, `DROP TABLE`, `TRUNCATE TABLE` require explicit approval.
   - Filesystem: `rm -rf`, bulk deletes require listing affected paths first.
   - Git: `--force` push, branch deletion require confirmation.

2. **List changes + risk level** before any external tool call.
   ```
   RISK: MEDIUM — will modify 3 database tables
   CHANGES:
   - ALTER TABLE users ADD COLUMN ...
   - CREATE INDEX idx_users_email ...
   - UPDATE users SET status = 'active' WHERE ...
   ACTION: Proceed? (yes/no)
   ```

3. **Prefer dry-run first** for all migration, deployment, and infrastructure operations.
   - Database migrations: run with `--dry-run` or `--preview` before applying.
   - Terraform: `plan` before `apply`.
   - Kubernetes: `--dry-run=client` before `kubectl apply`.

4. **Flag cost estimates** before executing cloud resource provisioning or API calls with usage-based pricing.

5. **Never fabricate schemas** — generate from source of truth (database models, API definitions) or validate against `schema/` directory.

## Secrets & Credentials

- Never output secrets, tokens, or passwords in logs, terminal, or agent messages.
- Redact PII unless explicitly approved for the specific operation.
- Never hardcode credentials — use environment variables or secret managers.

## Scope Constraints

- All operations must stay within `${PROJECT_ROOT}`.
- No spawning by any agent except Agent-01.
- No direct execution of generated code without test coverage.
