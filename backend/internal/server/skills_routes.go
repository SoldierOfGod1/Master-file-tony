package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"

	"github.com/SoldierOfGod1/command-centre/internal/skills"
)

// RegisterSkillsRoutes wires /api/v1/skills, /api/v1/mcp, and the MCP
// health snapshot endpoint onto the mux.
func RegisterSkillsRoutes(mux *http.ServeMux, api *API) {
	mux.HandleFunc("GET /api/v1/skills", api.handleListSkills)
	mux.HandleFunc("GET /api/v1/mcp", api.handleListMCP)
	mux.HandleFunc("GET /api/v1/mcp/health", api.handleMCPHealth)
	// Read + write a skill file (SKILL.md). Reuses the agents scanner's
	// sandbox — skills live under the same ~/.claude/skills or
	// <project>/.claude/skills roots that the scanner knows.
	mux.HandleFunc("GET /api/v1/skills/file", api.handleSkillFileRead)
	mux.HandleFunc("POST /api/v1/skills/file", api.handleSkillFileWrite)
}

func (a *API) handleListSkills(w http.ResponseWriter, r *http.Request) {
	projectDir, _ := os.Getwd()
	sc := skills.New(projectDir)
	jsonOK(w, sc.ListSkills())
}

func (a *API) handleListMCP(w http.ResponseWriter, r *http.Request) {
	projectDir, _ := os.Getwd()
	sc := skills.New(projectDir)
	jsonOK(w, sc.ListMCPServers())
}

// handleMCPHealth returns the latest cached status for every known MCP
// server. The monitor refreshes in the background every 60s; the API is
// non-blocking so the UI badge can poll cheaply.
func (a *API) handleMCPHealth(w http.ResponseWriter, r *http.Request) {
	if a.MCPHealth == nil {
		jsonOK(w, []skills.HealthStatus{})
		return
	}
	jsonOK(w, a.MCPHealth.Snapshot())
}

// handleSkillFileRead returns the raw contents of a sandboxed skill
// file. Uses the agents scanner's shared allow-list (which now
// includes ~/.claude/skills + <project>/.claude/skills).
func (a *API) handleSkillFileRead(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		jsonError(w, http.StatusBadRequest, "path query param required")
		return
	}
	sc := a.newScanner()
	body, err := sc.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			jsonError(w, http.StatusForbidden, "path outside sandbox")
			return
		}
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	jsonOK(w, map[string]string{"path": path, "content": body})
}

// handleSkillFileWrite atomically overwrites a user-owned skill file.
// Plugin-owned paths return 403.
type skillWriteBody struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (a *API) handleSkillFileWrite(w http.ResponseWriter, r *http.Request) {
	var body skillWriteBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Path == "" {
		jsonError(w, http.StatusBadRequest, "path required")
		return
	}
	sc := a.newScanner()
	if err := sc.WriteFile(body.Path, []byte(body.Content)); err != nil {
		if errors.Is(err, os.ErrPermission) {
			jsonError(w, http.StatusForbidden,
				"skill is read-only (plugin-owned or outside the user .claude tree)")
			return
		}
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// New name/description in frontmatter would otherwise be hidden
	// behind the 60s skills cache — drop it so the next list call
	// re-scans.
	skills.InvalidateCache()
	updated, _ := sc.ReadFile(body.Path)
	jsonOK(w, map[string]string{"path": body.Path, "content": updated})
}
