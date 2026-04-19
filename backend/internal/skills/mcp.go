package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// ListMCPServers reads project `.mcp.json` and the user-level `~/.claude/mcp.json`
// (if present) and returns a flattened list. Missing files are silently skipped.
func (s *scanner) ListMCPServers() []MCPServer {
	var out []MCPServer

	// The backend often runs with cwd=backend/, so walk upward looking for a
	// `.mcp.json` — up to 5 levels above the supplied project dir.
	if s.projectDir != "" {
		dir := s.projectDir
		for i := 0; i < 6; i++ {
			candidate := filepath.Join(dir, ".mcp.json")
			if _, err := os.Stat(candidate); err == nil {
				out = append(out, readMCPFile(candidate, SourceProject)...)
				break
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	if s.homeDir != "" {
		out = append(out, readMCPFile(filepath.Join(s.homeDir, ".claude", "mcp.json"), SourceGlobal)...)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func readMCPFile(path string, src Source) []MCPServer {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc struct {
		Servers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil
	}
	out := make([]MCPServer, 0, len(doc.Servers))
	for name, spec := range doc.Servers {
		srv := MCPServer{Name: name, Source: src, Enabled: true}
		if v, ok := spec["transport"].(string); ok {
			srv.Transport = v
		} else if _, ok := spec["command"].(string); ok {
			srv.Transport = "stdio"
		}
		if v, ok := spec["url"].(string); ok {
			srv.URL = v
		}
		if v, ok := spec["command"].(string); ok {
			srv.Command = v
		}
		if v, ok := spec["enabled"].(bool); ok {
			srv.Enabled = v
		}
		if v, ok := spec["_group"].(string); ok {
			srv.Group = v
		}
		if v, ok := spec["_comment"].(string); ok {
			srv.Comment = v
		}
		out = append(out, srv)
	}
	return out
}
