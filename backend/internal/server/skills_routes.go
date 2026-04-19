package server

import (
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
