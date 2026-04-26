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
		conversation_id TEXT,
		input_tokens INTEGER NOT NULL DEFAULT 0,
		output_tokens INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`,
	// SQLite allows ADD COLUMN; these are idempotent-safe via the schema_version
	// guard in migrations.go. Apply once per fresh DB; they no-op on re-runs.
	`ALTER TABLE cost_records ADD COLUMN conversation_id TEXT`,
	`ALTER TABLE cost_records ADD COLUMN input_tokens INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE cost_records ADD COLUMN output_tokens INTEGER NOT NULL DEFAULT 0`,

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
		user_id TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`,
	// Phase B1 of the agent-orchestrator plan: per-conversation
	// user_id. Existing rows get '' which the dispatcher treats
	// as 'anonymous' (write tools refused). Every new conversation
	// from the UI / Discord populates this from the request.
	`ALTER TABLE conversations ADD COLUMN user_id TEXT NOT NULL DEFAULT ''`,

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
	// Mirror for pipelines — each pipeline becomes a subtask under its
	// parent project's ClickUp task. Idempotent via the migrator's
	// "duplicate column" guard.
	`ALTER TABLE pipelines ADD COLUMN clickup_task_id TEXT DEFAULT ''`,
	`ALTER TABLE pipelines ADD COLUMN clickup_last_sync TEXT DEFAULT ''`,
	// Environment deployment URLs — one SIT and one Prod per project.
	// Feeds the Projects tab's Current/SIT/Prod view toggle.
	`ALTER TABLE projects ADD COLUMN sit_url TEXT DEFAULT ''`,
	`ALTER TABLE projects ADD COLUMN prod_url TEXT DEFAULT ''`,
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

	// --- rain Service monitoring tab (platforms health history + alerts + incidents) ---
	// service_checks is a ring-buffer of every health check — drives the
	// 24h/7d/30d uptime rollups and the service-detail latency chart.
	// One row per target per tick; stores state + latency + http_code.
	`CREATE TABLE IF NOT EXISTS service_checks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		service_id TEXT NOT NULL,
		state TEXT NOT NULL,
		http_code INTEGER NOT NULL DEFAULT 0,
		latency_ms INTEGER NOT NULL DEFAULT 0,
		error TEXT NOT NULL DEFAULT '',
		checked_at TEXT NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_service_checks_service_time ON service_checks(service_id, checked_at DESC)`,
	// service_alerts: every alert emitted by the rules engine.
	// Deduplicated at write time by (service_id, kind, open); `acked`
	// + `resolved_at` track the manual + auto lifecycle.
	`CREATE TABLE IF NOT EXISTS service_alerts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		service_id TEXT NOT NULL,
		kind TEXT NOT NULL,
		severity TEXT NOT NULL,
		message TEXT NOT NULL,
		cause TEXT NOT NULL DEFAULT '',
		next_step TEXT NOT NULL DEFAULT '',
		state TEXT NOT NULL DEFAULT 'open',
		created_at TEXT NOT NULL,
		resolved_at TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE INDEX IF NOT EXISTS idx_service_alerts_state ON service_alerts(state, created_at DESC)`,
	// service_incidents: auto-created on severity >= critical.
	// State: open → investigating → mitigated → resolved.
	`CREATE TABLE IF NOT EXISTS service_incidents (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		service_id TEXT NOT NULL,
		kind TEXT NOT NULL,
		severity TEXT NOT NULL,
		title TEXT NOT NULL,
		summary TEXT NOT NULL DEFAULT '',
		state TEXT NOT NULL DEFAULT 'open',
		opened_at TEXT NOT NULL,
		mitigated_at TEXT NOT NULL DEFAULT '',
		resolved_at TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE INDEX IF NOT EXISTS idx_service_incidents_state ON service_incidents(state, opened_at DESC)`,
	// service_incident_events: ordered timeline per incident (auto +
	// manual actions like ack / resolve).
	`CREATE TABLE IF NOT EXISTS service_incident_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		incident_id INTEGER NOT NULL,
		kind TEXT NOT NULL,
		message TEXT NOT NULL,
		at TEXT NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_service_incident_events_incident ON service_incident_events(incident_id, at ASC)`,

	// --- Customer 360 v2 decisioning layer (NBA recommendations + outcomes) ---
	// customer_recommendations: every rec shown to an agent. Written
	// by rankRecommendations on each lookup; updated on agent action.
	// `kind` is the stable catalogue key used for cooldown lookup.
	// `reason_codes` is pipe-separated on write to avoid a child table
	// for a field the UI only ever reads as a list.
	`CREATE TABLE IF NOT EXISTS customer_recommendations (
		id TEXT PRIMARY KEY,
		customer_id TEXT NOT NULL,
		type TEXT NOT NULL,
		kind TEXT NOT NULL,
		title TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		channel TEXT NOT NULL,
		priority_rank INTEGER NOT NULL,
		expected_value REAL NOT NULL DEFAULT 0,
		cost_estimate REAL NOT NULL DEFAULT 0,
		reason_codes TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'presented',
		created_at TEXT NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_customer_recs_customer ON customer_recommendations(customer_id, created_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_customer_recs_status ON customer_recommendations(customer_id, kind, status, created_at DESC)`,

	// customer_recommendation_actions: raw audit of every accept/
	// dismiss/snooze + the channel the agent executed through + any
	// outcome note. This is the training data seed for a future
	// uplift model.
	`CREATE TABLE IF NOT EXISTS customer_recommendation_actions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		recommendation_id TEXT NOT NULL,
		customer_id TEXT NOT NULL,
		action TEXT NOT NULL,
		channel TEXT NOT NULL DEFAULT '',
		agent_id TEXT NOT NULL DEFAULT '',
		note TEXT NOT NULL DEFAULT '',
		at TEXT NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_rec_actions_rec ON customer_recommendation_actions(recommendation_id)`,
	`CREATE INDEX IF NOT EXISTS idx_rec_actions_customer ON customer_recommendation_actions(customer_id, at DESC)`,

	// Manual IMSI override per customer. When our 3-pivot IMSI
	// resolution (billing-account → msisdn → subscriber) can't find
	// a customer's SIMs, the operator enters their IMSIs here and
	// the Usage + CDR panels use them directly. `imsis` is a simple
	// pipe-separated list so we avoid a child table for a rarely-
	// edited small payload.
	`CREATE TABLE IF NOT EXISTS customer_imsi_overrides (
		customer_id TEXT PRIMARY KEY,
		imsis TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`,

	// POPIA audit trail for every IMSI lookup. Phase 3 of
	// docs/axiom/sim-diagnostics-plan.md. One row per resolveIMSIs
	// call. `individual_id` is the FK back to the customer (no
	// msisdn_hash — hashing adds attack surface without information
	// per eng-review 3C). `winning_phase` records which cascade
	// phase resolved (override / p1_product / p1_5_service /
	// p2_view_account / p3_view_user / exhausted) so we can see
	// when phase 1 starts drifting behind later phases.
	// Retention target: 18 months — cleanup is a downstream
	// concern; this migration just creates the table.
	`CREATE TABLE IF NOT EXISTS imsi_lookup_audit (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		at TEXT NOT NULL DEFAULT (datetime('now')),
		individual_id TEXT NOT NULL,
		source TEXT NOT NULL,
		winning_phase TEXT NOT NULL,
		imsi_count INTEGER NOT NULL DEFAULT 0,
		response_code INTEGER NOT NULL DEFAULT 200,
		reason TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE INDEX IF NOT EXISTS idx_imsi_audit_individual_at ON imsi_lookup_audit(individual_id, at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_imsi_audit_at ON imsi_lookup_audit(at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_imsi_audit_phase_at ON imsi_lookup_audit(winning_phase, at DESC)`,
}
