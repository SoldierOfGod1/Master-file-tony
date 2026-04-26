// Package sync keeps the local `projects` table aligned with a ClickUp list.
// Outbound writes happen inline on project create/update (see server.API).
// This file owns the inbound poller goroutine and a shared helper used by
// both directions.
package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	gosync "sync"
	"time"

	"github.com/SoldierOfGod1/command-centre/internal/clickup"
	"github.com/SoldierOfGod1/command-centre/internal/event"
	"github.com/SoldierOfGod1/command-centre/internal/store"
)

// Alias so the field type reads naturally while avoiding a name clash
// with this package (the project package is called `sync`).
type syncMutex = gosync.Mutex

// Engine bundles the state each sync pass needs. Callers hold one of these
// for the process lifetime and call Run(ctx) once.
type Engine struct {
	DB       *sql.DB
	Store    *store.Store
	Log      *slog.Logger
	Bus      *event.Bus
	Interval time.Duration

	// Async sync state — the Sync now button fires this. Protected by
	// syncMu. StartAsyncPushAll drops new runs if one is in flight.
	syncMu    syncMutex
	syncState SyncState
}

// SyncState is the JSON shape the /projects/sync/status endpoint
// returns. Mirrors what the UI needs to render a progress bar.
type SyncState struct {
	InProgress   bool      `json:"in_progress"`
	StartedAt    time.Time `json:"started_at,omitempty"`
	FinishedAt   time.Time `json:"finished_at,omitempty"`
	Total        int       `json:"total"`
	Pushed       int       `json:"pushed"`
	Skipped      int       `json:"skipped"`
	CurrentID    string    `json:"current_id,omitempty"`
	LastError    string    `json:"last_error,omitempty"`
	LastDuration string    `json:"last_duration,omitempty"`
}

// New returns an Engine with default 60s polling. Interval can be overridden
// for tests.
func New(s *store.Store, log *slog.Logger, bus *event.Bus) *Engine {
	return &Engine{
		DB:       s.DB,
		Store:    s,
		Log:      log,
		Bus:      bus,
		Interval: 60 * time.Second,
	}
}

// Status returns a copy of the latest async sync state.
func (e *Engine) Status() SyncState {
	e.syncMu.Lock()
	defer e.syncMu.Unlock()
	return e.syncState
}

// StartAsyncPushAll kicks off PushAll in a background goroutine and
// tracks progress on engine.syncState so the UI can poll /status. Safe
// to call concurrently — a second call while one is running is a no-op.
func (e *Engine) StartAsyncPushAll() {
	e.syncMu.Lock()
	if e.syncState.InProgress {
		e.syncMu.Unlock()
		return
	}
	e.syncState = SyncState{InProgress: true, StartedAt: time.Now().UTC()}
	e.syncMu.Unlock()

	go func() {
		start := time.Now()
		// Use our per-project counter so the UI shows progress.
		pushed, skipped, err := e.pushAllTracked()
		e.syncMu.Lock()
		e.syncState.InProgress = false
		e.syncState.FinishedAt = time.Now().UTC()
		e.syncState.Pushed = pushed
		e.syncState.Skipped = skipped
		e.syncState.LastDuration = time.Since(start).Round(time.Millisecond).String()
		if err != nil {
			e.syncState.LastError = err.Error()
		} else {
			e.syncState.LastError = ""
		}
		e.syncMu.Unlock()
	}()
}

// pushAllTracked mirrors PushAll but updates engine.syncState per
// project so the status endpoint can show live progress.
func (e *Engine) pushAllTracked() (pushed int, skipped int, err error) {
	rows, err := e.DB.Query(`SELECT id FROM projects`)
	if err != nil {
		return 0, 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return pushed, skipped, err
		}
		ids = append(ids, id)
	}
	rows.Close()

	e.syncMu.Lock()
	e.syncState.Total = len(ids)
	e.syncMu.Unlock()

	for _, id := range ids {
		e.syncMu.Lock()
		e.syncState.CurrentID = id
		e.syncMu.Unlock()
		if perr := e.PushProject(id); perr != nil {
			e.Log.Warn("sync: push failed", "project_id", id, "error", perr)
			skipped++
		} else {
			pushed++
		}
		e.syncMu.Lock()
		e.syncState.Pushed = pushed
		e.syncState.Skipped = skipped
		e.syncMu.Unlock()
	}
	e.syncMu.Lock()
	e.syncState.CurrentID = ""
	e.syncMu.Unlock()
	return pushed, skipped, nil
}

// Run starts the inbound polling loop. Blocks until ctx is cancelled.
// Call from a goroutine: `go engine.Run(ctx)`.
func (e *Engine) Run(ctx context.Context) {
	// Fire once immediately so the first pull happens at boot instead of
	// after the first tick.
	e.pullOnce()

	t := time.NewTicker(e.Interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			e.Log.Info("sync engine stopped")
			return
		case <-t.C:
			e.pullOnce()
		}
	}
}

// pullOnce fetches the configured ClickUp list and reconciles each mapped
// project. Never returns an error — it logs and moves on so a single bad
// poll doesn't kill the loop.
func (e *Engine) pullOnce() {
	cfg, client, ok := e.clientFromSettings()
	if !ok {
		return // not configured; no-op
	}

	tasks, err := client.ListTasks(cfg.ListID)
	if err != nil {
		e.Log.Warn("clickup pull failed", "error", err)
		return
	}

	// Index ClickUp tasks by ID for O(1) lookup when walking the projects table.
	byID := make(map[string]clickup.Task, len(tasks))
	for _, t := range tasks {
		byID[t.ID] = t
	}

	rows, err := e.DB.Query(`
		SELECT id, name, status, progress_pct, description,
		       clickup_task_id, external_updated_at
		FROM projects
		WHERE clickup_task_id != ''
	`)
	if err != nil {
		e.Log.Error("sync: list local projects", "error", err)
		return
	}
	defer rows.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	var pulled int

	for rows.Next() {
		var id, name, status, desc, taskID, externalTS string
		var progress int
		if err := rows.Scan(&id, &name, &status, &progress, &desc, &taskID, &externalTS); err != nil {
			e.Log.Warn("sync: scan row", "error", err)
			continue
		}
		task, ok := byID[taskID]
		if !ok {
			continue // mapped to a task we can't see — maybe user changed list
		}

		// If ClickUp's date_updated is not newer than the one we recorded
		// at last push, there's nothing to pull.
		if task.DateUpdated == "" || task.DateUpdated == externalTS {
			continue
		}

		remote := clickup.NormaliseStatus(task.Status)
		local := clickup.NormaliseStatus(status)
		if remote == local && task.Description == desc {
			// Field values match; just record the timestamp.
			_, _ = e.DB.Exec(
				`UPDATE projects SET external_updated_at=?, clickup_last_sync=? WHERE id=?`,
				task.DateUpdated, now, id,
			)
			continue
		}

		if _, err := e.DB.Exec(`
			UPDATE projects
			SET status = ?, description = ?, updated_at = ?,
			    external_updated_at = ?, clickup_last_sync = ?
			WHERE id = ?
		`, prettyStatus(remote), task.Description, now, task.DateUpdated, now, id); err != nil {
			e.Log.Error("sync: update local project", "id", id, "error", err)
			continue
		}
		pulled++
		e.Log.Info("sync: pulled clickup change", "project_id", id, "name", name,
			"status", remote)

		// Push a WebSocket event so the Projects page can refresh without
		// full reload.
		if e.Bus != nil {
			e.Bus.PublishJSON("projects.updated", map[string]string{
				"id":     id,
				"source": "clickup",
			})
		}
	}

	if pulled > 0 {
		e.Log.Info("clickup pull complete", "pulled", pulled, "total_tasks", len(tasks))
	}
}

// DeleteProject removes a project locally AND its mirrored ClickUp task
// (+ any subtasks under it — ClickUp cascades those when the parent
// task is deleted). Returns a sql.ErrNoRows-compatible error when the
// project doesn't exist so callers can 404 cleanly.
func (e *Engine) DeleteProject(projectID string) error {
	var taskID string
	err := e.DB.QueryRow(
		`SELECT COALESCE(clickup_task_id,'') FROM projects WHERE id = ?`, projectID,
	).Scan(&taskID)
	if err != nil {
		return fmt.Errorf("delete: load %s: %w", projectID, err)
	}
	// Remove the ClickUp task first. Best-effort — if ClickUp fails we
	// still drop the local row so the state can't diverge further.
	if taskID != "" {
		if _, client, ok := e.clientFromSettings(); ok {
			if derr := client.DeleteTask(taskID); derr != nil {
				e.Log.Warn("sync: delete clickup task",
					"project_id", projectID, "task_id", taskID, "error", derr)
			}
		}
	}
	// Local cleanup — project row + any component-subtask bridge rows.
	_, _ = e.DB.Exec(`DELETE FROM project_subtask_map WHERE project_id = ?`, projectID)
	_, _ = e.DB.Exec(`DELETE FROM pipelines WHERE project_id = ?`, projectID)
	res, err := e.DB.Exec(`DELETE FROM projects WHERE id = ?`, projectID)
	if err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("project %s not found", projectID)
	}
	e.Log.Info("sync: deleted project", "project_id", projectID, "task_id", taskID)
	return nil
}

// PushProject mirrors a single project to ClickUp. Called inline from the
// HTTP handlers after a successful POST/PUT to /api/v1/projects.
func (e *Engine) PushProject(projectID string) error {
	cfg, client, ok := e.clientFromSettings()
	if !ok {
		return nil // not configured — silently skip
	}

	var (
		name, desc, status, taskID string
		priority                   string
	)
	err := e.DB.QueryRow(`
		SELECT name, description, status, priority, clickup_task_id
		FROM projects WHERE id = ?
	`, projectID).Scan(&name, &desc, &status, &priority, &taskID)
	if err != nil {
		return fmt.Errorf("push: load %s: %w", projectID, err)
	}

	pr := priorityToClickUp(priority)

	if taskID == "" {
		// First push for this project — create the ClickUp task.
		t, err := client.CreateTask(cfg.ListID, clickup.CreateTaskInput{
			Name:        name,
			Description: desc,
			Status:      clickup.NormaliseStatus(status),
			Priority:    pr,
		})
		if err != nil {
			return fmt.Errorf("push: create task for %s: %w", projectID, err)
		}
		// Get full task to capture date_updated.
		full, err := client.GetTask(t.ID)
		if err != nil {
			return fmt.Errorf("push: get created task: %w", err)
		}
		_, err = e.DB.Exec(`
			UPDATE projects
			SET clickup_task_id = ?, external_updated_at = ?,
			    clickup_last_sync = datetime('now')
			WHERE id = ?
		`, full.ID, full.DateUpdated, projectID)
		if err != nil {
			return fmt.Errorf("push: persist task id: %w", err)
		}
		e.Log.Info("sync: pushed new clickup task", "project_id", projectID,
			"task_id", full.ID)
		// Cascade: push every pipeline for this project as a subtask.
		if pe := e.pushPipelinesFor(projectID, full.ID, cfg, client); pe != nil {
			e.Log.Warn("sync: pipeline subtask push",
				"project_id", projectID, "parent_task_id", full.ID, "error", pe)
		}
		return nil
	}

	// Existing task — PUT the current state.
	t, err := client.UpdateTask(taskID, clickup.UpdateTaskInput{
		Name:        name,
		Description: desc,
		Status:      clickup.NormaliseStatus(status),
		Priority:    pr,
	})
	if err != nil {
		return fmt.Errorf("push: update %s: %w", taskID, err)
	}
	_, err = e.DB.Exec(`
		UPDATE projects
		SET external_updated_at = ?, clickup_last_sync = datetime('now')
		WHERE id = ?
	`, t.DateUpdated, projectID)
	if err != nil {
		return fmt.Errorf("push: persist timestamp: %w", err)
	}
	e.Log.Info("sync: pushed update", "project_id", projectID, "task_id", taskID)
	// Also push any pipelines whose subtask hasn't been created yet.
	if pe := e.pushPipelinesFor(projectID, taskID, cfg, client); pe != nil {
		e.Log.Warn("sync: pipeline subtask push",
			"project_id", projectID, "parent_task_id", taskID, "error", pe)
	}
	return nil
}

// pushPipelinesFor creates a ClickUp subtask for every pipeline row
// belonging to `projectID` that doesn't yet have a `clickup_task_id`,
// and (as a second pass) for every entry in the project's
// `components` JSON array (frontend / backend / core / etc.).
//
// Components are looked up in a side table `project_subtask_map` so
// re-runs don't recreate the same subtask — first push stores
// component_role → ClickUp task id, subsequent pushes update status only.
//
// Runs inline during a project push. Failures on individual subtasks
// are logged and skipped.
func (e *Engine) pushPipelinesFor(projectID, parentTaskID string,
	cfg ClickUpConfig, client *clickup.Client,
) error {
	// ── Pipelines (named build/test/deploy runs) ───────────────
	if err := e.pushPipelineRows(projectID, parentTaskID, cfg, client); err != nil {
		e.Log.Warn("sync: pipelines subpush", "project_id", projectID, "error", err)
	}
	// ── Project components (frontend / backend / core) ────────
	if err := e.pushProjectComponents(projectID, parentTaskID, cfg, client); err != nil {
		return fmt.Errorf("components subpush: %w", err)
	}
	return nil
}

// pushPipelineRows — pipelines table subtask push. Typically the
// pipelines table is empty, but when a CI-integration fills it, each
// row becomes a subtask under the project.
func (e *Engine) pushPipelineRows(projectID, parentTaskID string,
	cfg ClickUpConfig, client *clickup.Client,
) error {
	rows, err := e.DB.Query(`
		SELECT id, name, COALESCE(type,'build'), COALESCE(status,'idle'),
		       COALESCE(branch,''), COALESCE(clickup_task_id,'')
		  FROM pipelines
		 WHERE project_id = ?
		 ORDER BY created_at ASC`, projectID)
	if err != nil {
		return fmt.Errorf("pipelines load: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, name, typ, status, branch, taskID string
		if err := rows.Scan(&id, &name, &typ, &status, &branch, &taskID); err != nil {
			continue
		}
		title := name
		if branch != "" {
			title = title + " · " + branch
		}
		desc := "Type: " + typ
		if branch != "" {
			desc += "\nBranch: " + branch
		}
		if taskID == "" {
			t, err := client.CreateTask(cfg.ListID, clickup.CreateTaskInput{
				Name:        title,
				Description: desc,
				Status:      clickup.NormaliseStatus(status),
				Parent:      parentTaskID,
			})
			if err != nil {
				e.Log.Warn("sync: create pipeline subtask", "pipeline_id", id, "error", err)
				continue
			}
			_, _ = e.DB.Exec(`UPDATE pipelines
				SET clickup_task_id = ?, clickup_last_sync = datetime('now')
				WHERE id = ?`, t.ID, id)
			e.Log.Info("sync: pushed pipeline subtask",
				"pipeline_id", id, "parent_task_id", parentTaskID, "subtask_id", t.ID)
			continue
		}
		if err := client.UpdateTaskStatus(taskID, clickup.NormaliseStatus(status)); err != nil {
			e.Log.Warn("sync: update pipeline subtask",
				"pipeline_id", id, "task_id", taskID, "error", err)
			continue
		}
		_, _ = e.DB.Exec(`UPDATE pipelines SET clickup_last_sync = datetime('now') WHERE id = ?`, id)
	}
	return nil
}

// pushProjectComponents creates one subtask per component (core /
// backend / frontend / etc.) under the project's ClickUp parent task.
// Reuses the lightweight `project_subtask_map` bridge table to
// remember (projectID, role) → subtask_id and avoid duplicates.
func (e *Engine) pushProjectComponents(projectID, parentTaskID string,
	cfg ClickUpConfig, client *clickup.Client,
) error {
	// Make sure the bridge exists. Plain SQLite-compatible.
	_, _ = e.DB.Exec(`
		CREATE TABLE IF NOT EXISTS project_subtask_map (
			project_id TEXT NOT NULL,
			role       TEXT NOT NULL,
			clickup_task_id TEXT NOT NULL,
			last_sync TEXT NOT NULL DEFAULT (datetime('now')),
			PRIMARY KEY (project_id, role)
		)`)

	// Read the components JSON and project status from the project row.
	var componentsJSON, projStatus string
	if err := e.DB.QueryRow(
		`SELECT COALESCE(components,'[]'), COALESCE(status,'in_progress')
		   FROM projects WHERE id = ?`, projectID,
	).Scan(&componentsJSON, &projStatus); err != nil {
		return fmt.Errorf("load components: %w", err)
	}
	// Parse — each entry is {path, role}.
	type component struct {
		Path string `json:"path"`
		Role string `json:"role"`
	}
	var comps []component
	if err := json.Unmarshal([]byte(componentsJSON), &comps); err != nil || len(comps) == 0 {
		return nil // no components — nothing to do
	}

	status := clickup.NormaliseStatus(projStatus)
	for _, c := range comps {
		if c.Role == "" {
			c.Role = "core"
		}
		role := c.Role
		title := titleForComponent(role)
		desc := "Component: " + role
		if c.Path != "" {
			desc += "\nLocal path: " + c.Path
		}

		// Look up the existing subtask (if any).
		var subTaskID string
		_ = e.DB.QueryRow(
			`SELECT clickup_task_id FROM project_subtask_map WHERE project_id = ? AND role = ?`,
			projectID, role).Scan(&subTaskID)

		if subTaskID == "" {
			t, err := client.CreateTask(cfg.ListID, clickup.CreateTaskInput{
				Name:        title,
				Description: desc,
				Status:      status,
				Parent:      parentTaskID,
			})
			if err != nil {
				e.Log.Warn("sync: create component subtask",
					"project_id", projectID, "role", role, "error", err)
				continue
			}
			_, _ = e.DB.Exec(
				`INSERT INTO project_subtask_map(project_id, role, clickup_task_id)
				 VALUES(?,?,?)`,
				projectID, role, t.ID)
			e.Log.Info("sync: pushed component subtask",
				"project_id", projectID, "role", role,
				"parent_task_id", parentTaskID, "subtask_id", t.ID)
			continue
		}
		// Existing subtask — nudge its status to match the parent.
		if err := client.UpdateTaskStatus(subTaskID, status); err != nil {
			e.Log.Warn("sync: update component subtask",
				"project_id", projectID, "role", role,
				"task_id", subTaskID, "error", err)
			continue
		}
		_, _ = e.DB.Exec(
			`UPDATE project_subtask_map SET last_sync = datetime('now')
			 WHERE project_id = ? AND role = ?`, projectID, role)
	}
	return nil
}

// titleForComponent renders a human-readable subtask title for a
// project component role.
func titleForComponent(role string) string {
	switch role {
	case "frontend":
		return "Frontend"
	case "backend":
		return "Backend"
	case "core":
		return "Core / Main"
	case "database", "db":
		return "Database"
	case "infra", "devops":
		return "Infrastructure / DevOps"
	}
	// Title-case fallback.
	if role == "" {
		return "Core"
	}
	return strings.ToUpper(role[:1]) + role[1:]
}

// PushAll is called by the "Sync now" button and by seed-clickup. Creates
// ClickUp tasks for every unmapped project, then pushes all others.
// Returns counts for the API response.
func (e *Engine) PushAll() (pushed int, skipped int, err error) {
	rows, err := e.DB.Query(`SELECT id FROM projects`)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return pushed, skipped, err
		}
		ids = append(ids, id)
	}

	for _, id := range ids {
		if err := e.PushProject(id); err != nil {
			e.Log.Warn("sync: push failed", "project_id", id, "error", err)
			skipped++
			continue
		}
		pushed++
	}
	return pushed, skipped, nil
}

// ClickUpConfig is the subset of settings the sync engine needs. Reads from
// app_settings every time so the user can rotate token/list/workspace via
// the Settings page without restarting the backend.
type ClickUpConfig struct {
	Token       string
	WorkspaceID string
	ListID      string
}

func (e *Engine) clientFromSettings() (ClickUpConfig, *clickup.Client, bool) {
	all, err := e.Store.GetAllSettings()
	if err != nil {
		e.Log.Warn("sync: read settings", "error", err)
		return ClickUpConfig{}, nil, false
	}
	cfg := ClickUpConfig{
		Token:       all[store.SettingClickUpToken],
		WorkspaceID: all[store.SettingClickUpWorkspaceID],
		ListID:      all[store.SettingClickUpListID],
	}
	if cfg.Token == "" || cfg.ListID == "" {
		return cfg, nil, false
	}
	return cfg, clickup.New(cfg.Token), true
}

// priorityToClickUp maps the dashboard's textual priority onto ClickUp's
// 1–4 scale. Unknown values become 3 (normal).
func priorityToClickUp(p string) int {
	switch strings.ToLower(p) {
	case "urgent", "critical", "p1":
		return 1
	case "high", "p2":
		return 2
	case "normal", "medium", "p3":
		return 3
	case "low", "p4":
		return 4
	default:
		return 3
	}
}

// prettyStatus upper-cases the first letter of each word so the UI gets
// "In Progress" rather than the lowercase canonical form we use on the wire.
func prettyStatus(s string) string {
	parts := strings.Fields(s)
	for i, p := range parts {
		if len(p) == 0 {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
	}
	return strings.Join(parts, " ")
}

// Unused: kept to silence "imported and not used" while scaffolding.
var _ = json.Marshal
