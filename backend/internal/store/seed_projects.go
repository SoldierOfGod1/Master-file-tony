package store

import (
	"encoding/json"
	"fmt"
	"time"
)

// ProjectComponent is one folder that makes up a logical project. Role is
// either "core", "frontend", "backend", or "infra" — used by the frontend
// to chip-colour each path.
type ProjectComponent struct {
	Role string `json:"role"`
	Path string `json:"path"`
}

// seedProject is the in-code definition of a default project. A slice of
// these is INSERTed by SeedProjectsIfEmpty on first startup.
type seedProject struct {
	ID          string
	Name        string
	Description string
	Components  []ProjectComponent
	SITURL      string // deployment URL on the SIT vibe environment
	ProdURL     string // production URL (unknown today for most)
}

const rainRoot = `C:\Users\BaptistaManuel\Downloads\Process Automation Specialist`

// defaultProjects is the 13-project grouping the user approved in the plan.
// Order matters — it's the initial board order. `ID` prefixes are kept short
// so they're stable identifiers (also used as ClickUp task references).
var defaultProjects = []seedProject{
	{ID: "proj-baptista-fin", Name: "Baptista Finance Dashboard", Description: "Personal finance dashboard",
		Components: []ProjectComponent{{Role: "core", Path: rainRoot + `\Baptista finance dashboard`}}},

	{ID: "proj-rapids", Name: "RAPIDS", Description: "RAPIDS platform — full stack",
		Components: []ProjectComponent{
			{Role: "core", Path: rainRoot + `\RAPIDS`},
			{Role: "backend", Path: rainRoot + `\rapids-backend-repo`},
			{Role: "frontend", Path: rainRoot + `\rapids-frontend-repo`},
		},
		SITURL: "https://rapids-sit.vibe.rain.co.za/"},

	{ID: "proj-rollout-tracker", Name: "Rollout Tracker", Description: "Network rollout tracker",
		Components: []ProjectComponent{{Role: "core", Path: rainRoot + `\ROLLOUT_TRACKER`}},
		SITURL:     "https://rollouttracker-sit.vibe.rain.co.za/"},

	{ID: "proj-rainlex", Name: "RainLex", Description: "RainLex app + deployment",
		Components: []ProjectComponent{
			{Role: "core", Path: rainRoot + `\rainLex`},
			{Role: "infra", Path: rainRoot + `\rainlex-deploy`},
		},
		SITURL: "https://rainlex-front-sit.vibe.rain.co.za/"},

	{ID: "proj-learning-dev", Name: "Learning Dev", Description: "Internal learning/dev sandbox",
		Components: []ProjectComponent{{Role: "core", Path: rainRoot + `\Learning_Dev`}}},

	{ID: "proj-rainway-hr", Name: "Rainway HR AI Agent", Description: "HR automation AI agent",
		Components: []ProjectComponent{{Role: "core", Path: rainRoot + `\rainway_HR_AI_Agent`}},
		SITURL:     "https://raina-myhr-frontend-sit.vibe.rain.co.za/chat"},

	{ID: "proj-bulk-risk-filter", Name: "Bulk Risk Filter", Description: "Bulk transaction risk screener",
		Components: []ProjectComponent{{Role: "core", Path: rainRoot + `\Bulk Risk Filter`}}},

	{ID: "proj-fin-categoriser", Name: "Financial Categorizer (Prod)", Description: "Production financial transaction categoriser",
		Components: []ProjectComponent{{Role: "core", Path: rainRoot + `\financial-categorizer-prod`}}},

	{ID: "proj-neo", Name: "Neo", Description: "Neo project",
		Components: []ProjectComponent{{Role: "core", Path: rainRoot + `\Neo`}}},

	{ID: "proj-leaseiq", Name: "LeaseIQ", Description: "LeaseIQ lease accounting tooling",
		Components: []ProjectComponent{{Role: "core", Path: rainRoot + `\LeaseIQ`}}},

	{ID: "proj-borrowing-cost", Name: "Borrowing Cost Files", Description: "Borrowing cost workstream",
		Components: []ProjectComponent{{Role: "core", Path: rainRoot + `\Borrowing cost files`}}},

	{ID: "proj-raincheck-qa", Name: "RainCheck QA Portal", Description: "RainCheck quality assurance portal",
		Components: []ProjectComponent{{Role: "core", Path: rainRoot + `\RainCheck QA Portal`}}},

	{ID: "proj-cpe-depreciation", Name: "CPE Depreciation", Description: "CPE depreciation modelling (monorepo + backend)",
		Components: []ProjectComponent{
			{Role: "core", Path: rainRoot + `\cpe_depreciation-master`},
			{Role: "backend", Path: rainRoot + `\cpe-depreciation-backend`},
		}},

	{ID: "proj-pdf-reader", Name: "PDF Reader", Description: "PDF reader utility",
		Components: []ProjectComponent{{Role: "core", Path: rainRoot + `\PDF reaader`}}},

	{ID: "proj-sparcv2", Name: "Sparcv2", Description: "Sparc v2 forms — SIT-deployed",
		Components: []ProjectComponent{{Role: "core", Path: rainRoot + `\sparcv2`}},
		SITURL:     "https://sparcv2-form-sit.vibe.rain.co.za/sparc-v2-forms.html"},
}

// SeedProjectsIfEmpty inserts the default 13 projects if the projects table
// has no rows. Safe to call on every boot — becomes a no-op once seeded.
// Callers typically invoke this from main.go after SeedIfEmpty() for the
// other tables.
//
// Also: on every boot it *does* backfill `sit_url`/`prod_url` on existing
// rows where those columns are empty and a default is known — that way
// shipping new default URLs (like the 5 SIT endpoints we just added)
// doesn't require a database wipe. User-edited URLs are never overwritten.
func (s *Store) SeedProjectsIfEmpty() error {
	// Back-fill known SIT/Prod URLs on existing rows *without* touching
	// anything the user edited. This runs every boot and is idempotent.
	defer s.backfillEnvironmentURLs()

	// "Real" = has a local_path. Legacy placeholder rows from the original
	// seed (proj-1/2/3) have empty local_path and get replaced on first run.
	var realCount int
	if err := s.DB.QueryRow(
		`SELECT COUNT(*) FROM projects WHERE local_path != ''`,
	).Scan(&realCount); err != nil {
		return fmt.Errorf("count real projects: %w", err)
	}
	if realCount > 0 {
		return nil
	}

	// Clear legacy placeholders. User-created projects always have a
	// local_path so this never touches real data.
	if _, err := s.DB.Exec(
		`DELETE FROM projects WHERE local_path IS NULL OR local_path = ''`,
	); err != nil {
		return fmt.Errorf("clear legacy projects: %w", err)
	}

	s.Log.Info("seeding projects", "count", len(defaultProjects))
	now := time.Now().UTC().Format(time.RFC3339)

	for _, p := range defaultProjects {
		components, err := json.Marshal(p.Components)
		if err != nil {
			return fmt.Errorf("marshal components for %s: %w", p.ID, err)
		}

		hasFrontend := 0
		hasBackend := 0
		primaryPath := ""
		for _, c := range p.Components {
			if primaryPath == "" {
				primaryPath = c.Path
			}
			switch c.Role {
			case "frontend":
				hasFrontend = 1
			case "backend":
				hasBackend = 1
			}
		}

		if _, err := s.DB.Exec(`
			INSERT INTO projects (
				id, name, description, status, priority, owner,
				local_path, components, has_frontend, has_backend,
				progress_pct, sit_url, prod_url, created_at, updated_at
			) VALUES (?, ?, ?, 'To Do', 'normal', 'baptista', ?, ?, ?, ?, 0, ?, ?, ?, ?)
		`,
			p.ID, p.Name, p.Description, primaryPath, string(components),
			hasFrontend, hasBackend, p.SITURL, p.ProdURL, now, now,
		); err != nil {
			return fmt.Errorf("insert project %s: %w", p.ID, err)
		}
	}
	return nil
}

// backfillEnvironmentURLs updates sit_url/prod_url on projects that
// already exist in the DB (so upgrades pick up newly-known defaults)
// without clobbering user-edited values. "Empty" = NULL or ''.
func (s *Store) backfillEnvironmentURLs() {
	for _, p := range defaultProjects {
		if p.SITURL == "" && p.ProdURL == "" {
			continue
		}
		if p.SITURL != "" {
			_, _ = s.DB.Exec(
				`UPDATE projects SET sit_url = ?
				  WHERE id = ? AND (sit_url IS NULL OR sit_url = '')`,
				p.SITURL, p.ID)
		}
		if p.ProdURL != "" {
			_, _ = s.DB.Exec(
				`UPDATE projects SET prod_url = ?
				  WHERE id = ? AND (prod_url IS NULL OR prod_url = '')`,
				p.ProdURL, p.ID)
		}
	}
	// Also ensure the new Sparcv2 project row exists, even on a
	// pre-existing DB that was seeded before it was added to the list.
	for _, p := range defaultProjects {
		if p.ID != "proj-sparcv2" {
			continue
		}
		var exists int
		_ = s.DB.QueryRow(`SELECT COUNT(*) FROM projects WHERE id = ?`, p.ID).Scan(&exists)
		if exists > 0 {
			continue
		}
		components, _ := json.Marshal(p.Components)
		primaryPath := ""
		if len(p.Components) > 0 {
			primaryPath = p.Components[0].Path
		}
		now := time.Now().UTC().Format(time.RFC3339)
		_, _ = s.DB.Exec(`
			INSERT INTO projects (
				id, name, description, status, priority, owner,
				local_path, components, has_frontend, has_backend,
				progress_pct, sit_url, prod_url, created_at, updated_at
			) VALUES (?, ?, ?, 'To Do', 'normal', 'baptista', ?, ?, 0, 0, 0, ?, ?, ?, ?)
		`, p.ID, p.Name, p.Description, primaryPath, string(components),
			p.SITURL, p.ProdURL, now, now)
	}
}
