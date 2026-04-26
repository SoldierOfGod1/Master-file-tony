package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/SoldierOfGod1/command-centre/internal/agents"
)

// RegisterAgentsRoutes wires the Agent Fleet endpoints: agents list, hooks
// list, rules list, raw file read (sandboxed), and per-agent memory R/W.
func RegisterAgentsRoutes(mux *http.ServeMux, api *API) {
	mux.HandleFunc("GET /api/v1/agent-fleet/agents", api.handleFleetAgents)
	mux.HandleFunc("GET /api/v1/agent-fleet/hooks", api.handleFleetHooks)
	mux.HandleFunc("GET /api/v1/agent-fleet/rules", api.handleFleetRules)
	mux.HandleFunc("GET /api/v1/agent-fleet/file", api.handleFleetFile)
	mux.HandleFunc("GET /api/v1/agent-fleet/memory", api.handleMemoryRead)
	mux.HandleFunc("POST /api/v1/agent-fleet/memory", api.handleMemoryAppend)
	mux.HandleFunc("POST /api/v1/agent-fleet/file", api.handleFleetFileWrite)
	mux.HandleFunc("POST /api/v1/agent-fleet/agents", api.handleFleetCreateAgent)
}

// newScanner returns an agents.Scanner rooted at whichever working directory
// the backend was started in. Walks up 5 levels to find a .claude directory
// so running `go run ./cmd/server` from backend/ still resolves to the
// project root where .claude lives.
func (a *API) newScanner() *agents.Scanner {
	root, _ := os.Getwd()
	return agents.New(resolveClaudeRoot(root))
}

// resolveClaudeRoot walks upward from startDir looking for a `.claude/agents`
// directory. We need the agents subdir specifically — the backend's own
// `.claude/logs/` would short-circuit a plain `.claude` existence check.
func resolveClaudeRoot(startDir string) string {
	dir := startDir
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(dir + "/.claude/agents"); err == nil {
			return dir
		}
		parent := parentDir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return startDir
}

// parentDir returns the parent of `p`. A tiny wrapper so the upward walk
// reads naturally.
func parentDir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[:i]
		}
	}
	return p
}

func (a *API) handleFleetAgents(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, a.newScanner().ListAgents())
}

func (a *API) handleFleetHooks(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, a.newScanner().ListHooks())
}

func (a *API) handleFleetRules(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, a.newScanner().ListRules())
}

// handleFleetFile returns the raw contents of a sandboxed file. The scanner
// enforces that `path` is inside one of the .claude/{agents,hooks,rules}
// directories so this endpoint can't be abused as a generic file-read.
func (a *API) handleFleetFile(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		jsonError(w, http.StatusBadRequest, "path required")
		return
	}
	content, err := a.newScanner().ReadFile(path)
	if err != nil {
		if os.IsPermission(err) {
			jsonError(w, http.StatusForbidden, "path outside sandbox")
			return
		}
		if os.IsNotExist(err) {
			jsonError(w, http.StatusNotFound, "not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]string{"path": path, "content": content})
}

func (a *API) handleMemoryRead(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		jsonError(w, http.StatusBadRequest, "path required (agent .md path)")
		return
	}
	content, err := a.newScanner().ReadMemory(path)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]string{"path": path, "content": content})
}

// handleMemoryAppend adds a new dated entry to the agent's memory file.
// Body: {"path": "...agent.md", "note": "what worked / what to avoid"}
func (a *API) handleMemoryAppend(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
		Note string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Path == "" || body.Note == "" {
		jsonError(w, http.StatusBadRequest, "path and note required")
		return
	}
	if err := a.newScanner().AppendMemory(body.Path, body.Note); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	content, _ := a.newScanner().ReadMemory(body.Path)
	jsonOK(w, map[string]string{"path": body.Path, "content": content})
}

// handleFleetFileWrite atomically overwrites an agent/hook/rule/skill
// markdown file, subject to the scanner's writable-path allow-list.
// Plugin-owned files return 403 — the UI should clone-to-global first.
type fleetWriteBody struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (a *API) handleFleetFileWrite(w http.ResponseWriter, r *http.Request) {
	var body fleetWriteBody
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
				"path is read-only (plugin-owned or outside the user .claude tree)")
			return
		}
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Best-effort re-read so the UI can refresh from the canonical bytes.
	updated, _ := sc.ReadFile(body.Path)
	jsonOK(w, map[string]string{"path": body.Path, "content": updated})
}

// handleFleetCreateAgent accepts a minimal agent spec and materialises
// the .md file under the chosen user root (global or project).
type fleetCreateAgentBody struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category,omitempty"`
	Source      string `json:"source"` // "global" | "project"
	Model       string `json:"model,omitempty"`
	Body        string `json:"body,omitempty"`
	Overwrite   bool   `json:"overwrite,omitempty"`
}

func (a *API) handleFleetCreateAgent(w http.ResponseWriter, r *http.Request) {
	var b fleetCreateAgentBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid json")
		return
	}
	dest := agents.SourceGlobal
	if strings.EqualFold(b.Source, "project") {
		dest = agents.SourceProject
	}
	sc := a.newScanner()
	path, err := sc.CreateAgent(agents.CreateAgentRequest{
		Name: b.Name, Description: b.Description,
		Category: b.Category, Dest: dest,
		Model: b.Model, Body: b.Body, Overwrite: b.Overwrite,
	})
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Return the freshly-parsed agent row so the UI can splice it in.
	for _, ag := range sc.ListAgents() {
		if ag.Path == path {
			jsonOK(w, ag)
			return
		}
	}
	jsonOK(w, map[string]string{"path": path})
}
