# System Protection Configuration
# CRITICAL: This file protects your system when using --dangerously-allow-all-tools

## Allowed Operations (Safe)
These operations are permitted within the project root:
- Creating/editing/deleting files within project directory
- Running npm/pip/go/maven/gradle install commands
- Starting development servers on designated ports
- Git operations within project repository
- Running tests and linters
- Building and packaging applications
- Reading documentation and configuration files

## Blocked Operations (Dangerous)
These operations are ALWAYS blocked to protect your system:

### SQL Destructive Operations
- `DROP DATABASE` — requires explicit user confirmation
- `DROP TABLE` — requires explicit user confirmation
- `TRUNCATE TABLE` — requires explicit user confirmation
- `DELETE FROM` without `WHERE` clause — always blocked
- Prefer `--dry-run` / `--preview` for all migrations before applying

### MCP Safety Rules
- MCP tool calls that modify external state require listing changes + risk level first
- Flag **cost estimates** before executing cloud provisioning or usage-based API calls
- Prefer **dry-run** mode when available for MCP tools that mutate state
- Log all MCP tool invocations to `.claude/log/mcp-calls.jsonl`

### Filesystem Protection
- `rm -rf /` or any variant targeting system root
- `rm -rf ~` or operations on home directory outside project
- `rm -rf ..` or parent directory operations
- `del /f /s /q C:\` or Windows system drive operations
- `format` or `diskpart` commands
- Any operation on `/etc`, `/usr`, `/bin`, `/sbin`, `/lib`
- Any operation on `C:\Windows`, `C:\Program Files`
- Modifications to system PATH variable
- Changes to system registry (Windows)

### Process and Service Protection
- `kill -9 1` or attempts to kill system processes
- `systemctl stop` on critical services
- `service stop` on critical services
- Killing explorer.exe or system processes
- Modifying startup scripts outside project

### Network Protection
- Firewall modifications
- Opening ports below 1024 (requires root/admin)
- Modifying /etc/hosts or C:\Windows\System32\drivers\etc\hosts
- VPN or network adapter changes
- Proxy server modifications

### Permission and Security
- `chmod 777` on system directories
- `chown` operations outside project
- Creating/modifying sudoers file
- Installing system-wide packages without confirmation
- Modifying security policies
- Disabling antivirus or security software

### Package Management Protection
- `sudo apt-get remove` of system packages
- `sudo yum remove` of system packages
- Uninstalling critical system libraries
- Global npm installs without explicit approval
- System Python modifications

## Environment Variable Protection
Protected variables that cannot be modified:
- PATH (can only append project-specific paths)
- HOME / USERPROFILE
- SYSTEMROOT
- PROGRAMFILES
- WINDIR
- System library paths

## Command Filters
Commands that require explicit user confirmation:
- Any command with `sudo` or admin elevation
- System-wide package installations
- Service modifications
- Cron job modifications
- Scheduled task modifications
- Shell profile modifications (~/.bashrc, ~/.zshrc, etc.)

## Safe Installation Whitelist
These package managers are allowed within project:
- `npm install` (local node_modules only)
- `pip install` (ONLY within activated venv)
- `./venv/bin/pip install` (explicit venv pip)
- `venv\Scripts\pip install` (Windows venv pip)
- `python -m pip install` (when venv activated)
- `pip install -r requirements.txt` (in venv)
- `pip install -r requirements-dev.txt` (in venv)
- `pip install -r requirements-ml.txt` (in venv)
- `go get` / `go mod download` (local go.mod)
- `cargo build` / `cargo add` (local Cargo.toml)
- `composer install` (local composer.json)
- `bundle install` (local Gemfile)
- `yarn add` (local package.json)
- `pnpm install` (local package.json)
- `poetry add` (local pyproject.toml)
- `mvn install` (local pom.xml)
- `gradle build` (local build.gradle)

## Directory Constraints
All operations must be within:
```
${PROJECT_ROOT}/
├── venv/              # Python virtual environment (auto-created)
├── node_modules/      # Node dependencies (auto-created)
├── backend/
├── frontend/
├── database/
├── tests/
├── scripts/
├── docs/
├── .claude/
├── logs/
├── docker/
├── kubernetes/
└── migration/         # Optional - only for cloud migration
```

## Virtual Environment Protection
- **Python venv**: Always created at `./venv` in project root
- **Activation Required**: All Python operations must use activated venv
- **No Global Python**: System Python packages are blocked
- **Package Isolation**: Each project has independent dependencies
- **Node modules**: Similar isolation for JavaScript packages

## Port Allocation Rules
- Frontend: 3000-3099 (project-specific)
- Backend: 8000-8099 (project-specific)
- Database: 5432, 3306, 27017 (standard)
- Redis: 6379
- Elasticsearch: 9200
- Never bind to 0.0.0.0 in production

## Git Safety
- Never force push to main/master
- Never delete .git directory
- Never modify .git/config with credentials
- Always use SSH keys or tokens (never hardcode)
- Preserve existing git history

## File Size Limits
- Single file write: Max 10MB
- Bulk operation: Max 100MB total
- Log files: Auto-rotate at 50MB
- Prevent infinite loops in file generation

## Execution Timeouts
- Single command: 2 minutes default
- Long-running: 10 minutes max
- Overnight runs: 24 hours with explicit flag
- Infinite loops detected and terminated

## Validation Hooks
Before executing sensitive operations:
1. Check if path is within PROJECT_ROOT
2. Validate command against blocklist
3. Check file size before write
4. Verify port availability
5. Confirm package source is trusted

## Emergency Stop
If Claude attempts any blocked operation:
1. Command is immediately rejected
2. User is notified with reason
3. Alternative safe approach suggested
4. Incident logged for review

## User Override
Users can explicitly allow specific operations by:
1. Confirming with "ALLOW_ONCE: <specific command>"
2. Adding to project-specific whitelist
3. Modifying .claude/config.yaml rules

## Implementation Priority
1. **CRITICAL**: Filesystem protection (prevent data loss)
2. **HIGH**: Process/service protection (prevent system instability)
3. **MEDIUM**: Network protection (prevent security issues)
4. **LOW**: Convenience restrictions (can be overridden)

## Monitoring
All potentially dangerous operations are:
- Logged to .claude/logs/security.log
- Require explicit confirmation
- Rate-limited to prevent abuse
- Audited for patterns

## Recovery
If something goes wrong:
- State saved before operations
- Rollback capability for file changes
- Git commits for version control
- Backup of critical configs

---
REMEMBER: These protections are for YOUR safety. They prevent accidental system damage while allowing productive development work.