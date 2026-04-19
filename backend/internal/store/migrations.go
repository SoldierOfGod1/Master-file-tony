package store

var migrations = []string{
	`CREATE TABLE IF NOT EXISTS agents (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		model TEXT NOT NULL,
		max_instances INTEGER,
		status TEXT NOT NULL DEFAULT 'idle',
		task TEXT NOT NULL DEFAULT 'Standby',
		role TEXT NOT NULL,
		updated_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`,

	`CREATE TABLE IF NOT EXISTS tasks (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		agent_id TEXT NOT NULL,
		priority TEXT NOT NULL DEFAULT 'p3',
		col TEXT NOT NULL DEFAULT 'inbox',
		description TEXT DEFAULT '',
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`,

	`CREATE TABLE IF NOT EXISTS feed_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		time TEXT NOT NULL,
		type TEXT NOT NULL,
		agent_id TEXT NOT NULL,
		message TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`,

	`CREATE TABLE IF NOT EXISTS tools (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		icon TEXT NOT NULL,
		description TEXT NOT NULL,
		detail TEXT NOT NULL,
		agents TEXT NOT NULL DEFAULT '[]',
		systems TEXT NOT NULL DEFAULT '[]',
		status TEXT NOT NULL DEFAULT 'planned',
		updated_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`,

	`CREATE TABLE IF NOT EXISTS log_entries (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp TEXT NOT NULL,
		level TEXT NOT NULL,
		agent_id TEXT NOT NULL,
		message TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`,

	`CREATE TABLE IF NOT EXISTS cost_records (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		date TEXT NOT NULL,
		model_name TEXT NOT NULL,
		amount_zar REAL NOT NULL DEFAULT 0,
		tokens_used INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`,

	`CREATE TABLE IF NOT EXISTS security_state (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		trust_score INTEGER NOT NULL DEFAULT 85,
		critical_count INTEGER NOT NULL DEFAULT 0,
		warning_count INTEGER NOT NULL DEFAULT 0,
		info_count INTEGER NOT NULL DEFAULT 2,
		rules_active INTEGER NOT NULL DEFAULT 12,
		last_scan TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`,

	`CREATE TABLE IF NOT EXISTS approvals (
		id TEXT PRIMARY KEY,
		type TEXT NOT NULL,
		title TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		requester TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		priority TEXT NOT NULL DEFAULT 'normal',
		payload TEXT DEFAULT '{}',
		reviewer TEXT DEFAULT '',
		review_comment TEXT DEFAULT '',
		expires_at TEXT DEFAULT '',
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`,

	`CREATE TABLE IF NOT EXISTS projects (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'planning',
		priority TEXT NOT NULL DEFAULT 'normal',
		owner TEXT NOT NULL DEFAULT '',
		repository_url TEXT DEFAULT '',
		tags TEXT DEFAULT '[]',
		progress_pct INTEGER NOT NULL DEFAULT 0,
		started_at TEXT DEFAULT '',
		target_date TEXT DEFAULT '',
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`,

	`CREATE TABLE IF NOT EXISTS project_agents (
		project_id TEXT NOT NULL,
		agent_id TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'contributor',
		PRIMARY KEY (project_id, agent_id)
	)`,

	`CREATE TABLE IF NOT EXISTS pipelines (
		id TEXT PRIMARY KEY,
		project_id TEXT DEFAULT '',
		name TEXT NOT NULL,
		type TEXT NOT NULL DEFAULT 'build',
		status TEXT NOT NULL DEFAULT 'idle',
		trigger_type TEXT NOT NULL DEFAULT 'manual',
		branch TEXT DEFAULT '',
		commit_sha TEXT DEFAULT '',
		started_at TEXT DEFAULT '',
		finished_at TEXT DEFAULT '',
		duration_ms INTEGER DEFAULT 0,
		stages TEXT NOT NULL DEFAULT '[]',
		artifacts TEXT DEFAULT '[]',
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`,

	`CREATE TABLE IF NOT EXISTS documents (
		id TEXT PRIMARY KEY,
		project_id TEXT DEFAULT '',
		title TEXT NOT NULL,
		type TEXT NOT NULL DEFAULT 'guide',
		content TEXT NOT NULL DEFAULT '',
		path TEXT DEFAULT '',
		version INTEGER NOT NULL DEFAULT 1,
		author TEXT NOT NULL DEFAULT '',
		tags TEXT DEFAULT '[]',
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`,

	`CREATE TABLE IF NOT EXISTS agent_office_states (
		agent_id TEXT PRIMARY KEY,
		desk_x INTEGER NOT NULL DEFAULT 0,
		desk_y INTEGER NOT NULL DEFAULT 0,
		zone TEXT NOT NULL DEFAULT 'lobby',
		activity TEXT NOT NULL DEFAULT 'idle',
		mood TEXT NOT NULL DEFAULT 'neutral',
		current_file TEXT DEFAULT '',
		collaboration TEXT DEFAULT '[]',
		last_action TEXT DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`,

	`CREATE TABLE IF NOT EXISTS audit_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp TEXT NOT NULL DEFAULT (datetime('now')),
		actor TEXT NOT NULL,
		action TEXT NOT NULL,
		resource_type TEXT NOT NULL,
		resource_id TEXT NOT NULL,
		detail TEXT DEFAULT '{}',
		created_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`,

	`CREATE TABLE IF NOT EXISTS conversations (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL DEFAULT 'New Conversation',
		project_dir TEXT NOT NULL,
		source TEXT NOT NULL DEFAULT 'ui',
		status TEXT NOT NULL DEFAULT 'active',
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`,

	`CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		conversation_id TEXT NOT NULL REFERENCES conversations(id),
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		source TEXT NOT NULL DEFAULT 'ui',
		metadata TEXT DEFAULT '{}',
		created_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`,

	`CREATE INDEX IF NOT EXISTS idx_messages_conversation ON messages(conversation_id)`,

	`CREATE TABLE IF NOT EXISTS chat_config (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		discord_token TEXT DEFAULT '',
		discord_user_id TEXT DEFAULT '',
		pin_hash TEXT DEFAULT '',
		default_project_dir TEXT DEFAULT '',
		pin_timeout_minutes INTEGER NOT NULL DEFAULT 15,
		updated_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`,

	`INSERT OR IGNORE INTO chat_config (id) VALUES (1)`,

	// --- Projects <-> ClickUp sync (v2) ---
	// sqlite has no IF NOT EXISTS on ALTER TABLE ADD COLUMN; the migrate()
	// runner in store.go ignores "duplicate column" errors so re-runs are safe.
	`ALTER TABLE projects ADD COLUMN local_path TEXT DEFAULT ''`,
	`ALTER TABLE projects ADD COLUMN components TEXT DEFAULT '[]'`,
	`ALTER TABLE projects ADD COLUMN has_frontend INTEGER DEFAULT 0`,
	`ALTER TABLE projects ADD COLUMN has_backend INTEGER DEFAULT 0`,
	`ALTER TABLE projects ADD COLUMN clickup_task_id TEXT DEFAULT ''`,
	`ALTER TABLE projects ADD COLUMN clickup_last_sync TEXT DEFAULT ''`,
	`ALTER TABLE projects ADD COLUMN external_updated_at TEXT DEFAULT ''`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_clickup ON projects(clickup_task_id) WHERE clickup_task_id != ''`,

	// --- App settings (ClickUp workspace/list/token swap from Settings UI) ---
	`CREATE TABLE IF NOT EXISTS app_settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`,

	// --- Wipe mock/demo data from earlier builds ---
	// These rows were all inserted by the old SeedIfEmpty(). Every id below
	// is a hard-coded seed id — no user-created row can clash with them.
	// Rows with non-seed ids (real activity written by backend services)
	// are left untouched. Running this migration multiple times is a no-op
	// once the ids are gone.
	`DELETE FROM agents WHERE id IN ('01','02','03','04','05','06','07','08','09','10','11','12','13','U1','U2','U3','U4')`,
	`DELETE FROM tasks WHERE id IN ('t1','t2','t3','t4','t5','t6')`,
	`DELETE FROM tools WHERE id IN ('askrain','billing','network','churn','rica','payment','fraud','coverage','orders')`,
	`DELETE FROM approvals WHERE id IN ('apr-1','apr-2')`,
	`DELETE FROM pipelines WHERE id IN ('pipe-1')`,
	`DELETE FROM documents WHERE id IN ('doc-1')`,
	`DELETE FROM feed_events`,     // only seeded writes here historically
	`DELETE FROM log_entries`,     // ditto
	`DELETE FROM agent_office_states`, // ditto
	// Reset the singleton security_state to honest zeros. Before this it
	// defaulted to trust_score=85 / info_count=2 / rules_active=12 — all fake.
	`UPDATE security_state SET trust_score=0, critical_count=0, warning_count=0, info_count=0, rules_active=0, last_scan='', updated_at=datetime('now') WHERE id=1`,
	`INSERT OR IGNORE INTO security_state (id, trust_score, critical_count, warning_count, info_count, rules_active, last_scan, updated_at) VALUES (1, 0, 0, 0, 0, 0, '', datetime('now'))`,
}
