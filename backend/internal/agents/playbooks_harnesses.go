package agents

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Playbook is one slash-command definition (`.claude/commands/<name>.md`).
// These act as saved prompts the operator can re-run via `/<name>` in the
// CLI; surfacing them in the Agent Fleet page lets the operator see what's
// available and edit the bodies the same way they edit agents.
type Playbook struct {
	Name        string `json:"name"`        // filename stem, e.g. "code-review"
	Path        string `json:"path"`
	Description string `json:"description"` // first non-empty paragraph or frontmatter `description`
	Source      Source `json:"source"`      // global | project | plugin
	Plugin      string `json:"plugin,omitempty"`
	SizeBytes   int64  `json:"size_bytes"`
}

// Harness is one configuration file that shapes how Claude Code (or this
// command-centre) runs: settings.json (permissions, env, hooks),
// settings.local.json (per-machine overrides), .mcp.json, CLAUDE.md.
// Surfacing them lets the operator audit and edit the harness without
// hunting through ~/.claude/.
type Harness struct {
	Name      string `json:"name"`        // human-friendly label
	Path      string `json:"path"`
	Kind      string `json:"kind"`        // "settings" | "settings.local" | "mcp" | "claude.md"
	Source    Source `json:"source"`      // global | project
	SizeBytes int64  `json:"size_bytes"`
}

// ListPlaybooks enumerates slash-command definitions from the standard
// roots. Order: project first (operator's local override wins), then
// global, then plugin-bundled. Same pattern as ListAgents.
func (s *Scanner) ListPlaybooks() []Playbook {
	var out []Playbook
	if s.projectDir != "" {
		out = append(out, readPlaybooks(filepath.Join(s.projectDir, ".claude", "commands"), SourceProject, "")...)
	}
	if s.homeDir != "" {
		out = append(out, readPlaybooks(filepath.Join(s.homeDir, ".claude", "commands"), SourceGlobal, "")...)
		out = append(out, readPluginPlaybooks(filepath.Join(s.homeDir, ".claude", "plugins"))...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			// project < global < plugin so project shows up first
			return sourceRank(out[i].Source) < sourceRank(out[j].Source)
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// ListHarnesses returns every harness-config file that exists. Missing
// files are silently skipped; the UI just shows the ones we have.
func (s *Scanner) ListHarnesses() []Harness {
	var out []Harness
	add := func(path, name, kind string, src Source) {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return
		}
		out = append(out, Harness{
			Name:      name,
			Path:      path,
			Kind:      kind,
			Source:    src,
			SizeBytes: info.Size(),
		})
	}
	if s.projectDir != "" {
		base := filepath.Join(s.projectDir, ".claude")
		add(filepath.Join(base, "settings.json"), "Project settings", "settings", SourceProject)
		add(filepath.Join(base, "settings.local.json"), "Project settings (local)", "settings.local", SourceProject)
		add(filepath.Join(s.projectDir, ".mcp.json"), "Project MCP servers", "mcp", SourceProject)
		add(filepath.Join(base, "CLAUDE.md"), "Project CLAUDE.md", "claude.md", SourceProject)
		add(filepath.Join(s.projectDir, "CLAUDE.md"), "Project root CLAUDE.md", "claude.md", SourceProject)
	}
	if s.homeDir != "" {
		base := filepath.Join(s.homeDir, ".claude")
		add(filepath.Join(base, "settings.json"), "Global settings", "settings", SourceGlobal)
		add(filepath.Join(base, "settings.local.json"), "Global settings (local)", "settings.local", SourceGlobal)
		add(filepath.Join(base, "mcp.json"), "Global MCP servers", "mcp", SourceGlobal)
		add(filepath.Join(s.homeDir, "CLAUDE.md"), "Home CLAUDE.md", "claude.md", SourceGlobal)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return sourceRank(out[i].Source) < sourceRank(out[j].Source)
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func sourceRank(s Source) int {
	switch s {
	case SourceProject:
		return 0
	case SourceGlobal:
		return 1
	case SourcePlugin:
		return 2
	}
	return 3
}

// readPlaybooks scans a single .claude/commands/ directory (one level
// deep — slash commands don't nest).
func readPlaybooks(dir string, src Source, plugin string) []Playbook {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Playbook
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if filepath.Ext(name) != ".md" {
			continue
		}
		full := filepath.Join(dir, name)
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Playbook{
			Name:        strings.TrimSuffix(name, ".md"),
			Path:        full,
			Description: firstParagraph(full),
			Source:      src,
			Plugin:      plugin,
			SizeBytes:   info.Size(),
		})
	}
	return out
}

// readPluginPlaybooks walks ~/.claude/plugins/** for `commands/`
// directories. Mirrors readPluginAgents — same layouts, same dedup rule.
func readPluginPlaybooks(pluginsRoot string) []Playbook {
	if _, err := os.Stat(pluginsRoot); err != nil {
		return nil
	}
	var out []Playbook
	seenDirs := make(map[string]struct{})
	seen := make(map[string]struct{})

	_ = filepath.Walk(pluginsRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || !info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(pluginsRoot, path)
		relSlash := filepath.ToSlash(rel)
		// Same cache-bucket skip as readPluginAgents — repos/marketplaces
		// already cover everything.
		if strings.HasPrefix(relSlash, "cache/") || relSlash == "cache" {
			return filepath.SkipDir
		}
		if filepath.Base(path) != "commands" {
			return nil
		}
		if _, dup := seenDirs[path]; dup {
			return filepath.SkipDir
		}
		seenDirs[path] = struct{}{}

		plugin := pluginNameFromPath(path, pluginsRoot)
		for _, p := range readPlaybooks(path, SourcePlugin, plugin) {
			key := plugin + "::" + p.Name
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, p)
		}
		return filepath.SkipDir
	})
	return out
}

// firstParagraph reads the first non-empty paragraph (or YAML
// frontmatter `description`) from a markdown file. Used as a one-line
// hint in the Playbooks tab so the operator doesn't have to open every
// file just to remember what `/code-review` does.
func firstParagraph(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*64), 1024*256)

	inFM := false
	frontmatterDesc := ""
	body := ""
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first {
			first = false
			if strings.TrimSpace(line) == "---" {
				inFM = true
				continue
			}
		}
		if inFM {
			if strings.TrimSpace(line) == "---" {
				inFM = false
				continue
			}
			if strings.HasPrefix(strings.TrimSpace(line), "description:") {
				frontmatterDesc = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "description:"))
				frontmatterDesc = strings.Trim(frontmatterDesc, `"'`)
			}
			continue
		}
		// Skip blank lines and # headings; capture the first real line.
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		body = t
		break
	}
	if frontmatterDesc != "" {
		return frontmatterDesc
	}
	if len(body) > 240 {
		return body[:237] + "..."
	}
	return body
}
