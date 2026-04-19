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
	"time"

	"github.com/SoldierOfGod1/command-centre/internal/clickup"
	"github.com/SoldierOfGod1/command-centre/internal/event"
	"github.com/SoldierOfGod1/command-centre/internal/store"
)

// Engine bundles the state each sync pass needs. Callers hold one of these
// for the process lifetime and call Run(ctx) once.
type Engine struct {
	DB       *sql.DB
	Store    *store.Store
	Log      *slog.Logger
	Bus      *event.Bus
	Interval time.Duration
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
	return nil
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
