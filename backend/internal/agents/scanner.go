// Package agents scans Claude Code agent definitions (global + project),
// their companion memory files, plus sibling hooks and rules directories.
// Everything on disk; no external network.
package agents

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Source tells the UI whether an agent came from the user's ~/.claude
// directory or the project's own .claude directory.
type Source string

const (
	SourceGlobal  Source = "global"
	SourceProject Source = "project"
	SourcePlugin  Source = "plugin"
)

// Agent is the JSON-facing view of one agent definition. Parsed on demand
// so changes on disk show up without a backend restart.
type Agent struct {
	ID          string `json:"id"`           // stable key: source + filename
	Name        string `json:"name"`         // derived from frontmatter `name` or filename
	FileName    string `json:"file_name"`    // e.g. "agent-04-universal-backend.md"
	Path        string `json:"path"`         // absolute path on disk
	Description string `json:"description"`  // first non-empty paragraph
	Category    string `json:"category"`     // computed from name + filename
	Source      Source `json:"source"`
	Model       string `json:"model,omitempty"`
	Version     string `json:"version,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	Overrides   bool   `json:"overrides"`    // true if a global agent with same filename exists
	HasMemory   bool   `json:"has_memory"`   // sibling .memory.md exists
	Plugin      string `json:"plugin,omitempty"` // marketplace/plugin name for SourcePlugin entries
}

// Hook is one file inside .claude/hooks/ — a script or documentation.
type Hook struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Kind       string `json:"kind"`        // "script" | "docs" | "config"
	Language   string `json:"language"`    // "bash" | "powershell" | "json" | "markdown" | ...
	SizeBytes  int64  `json:"size_bytes"`
	Executable bool   `json:"executable"`  // +x on unix, .ps1/.sh/.bat extensions on any OS
}

// Rule is a rules doc, possibly under a language subfolder.
type Rule struct {
	Name    string `json:"name"`     // human-friendly filename without extension
	Path    string `json:"path"`
	Group   string `json:"group"`    // top-level folder: "common" | "python" | "golang" | ...
	Source  Source `json:"source"`
}

// Scanner reads agents / hooks / rules from both the user's home dir and
// the provided project root. Construct once per request; it holds no state.
type Scanner struct {
	homeDir    string
	projectDir string
}

// New returns a ready scanner. Callers typically pass os.Getwd-derived
// path for projectDir; homeDir is derived from os.UserHomeDir internally.
func New(projectDir string) *Scanner {
	h, _ := os.UserHomeDir()
	return &Scanner{homeDir: h, projectDir: projectDir}
}

// ListAgents returns every agent from global, project, and plugin dirs.
// Project agents are flagged as OVERRIDE when they share a filename with a
// global agent (same behaviour as Claude Code's own resolution). Plugin
// agents are tagged with their host plugin name so the UI can group them.
func (s *Scanner) ListAgents() []Agent {
	global := readAgents(filepath.Join(s.homeDir, ".claude", "agents"), SourceGlobal)
	project := readAgents(filepath.Join(s.projectDir, ".claude", "agents"), SourceProject)
	plugins := readPluginAgents(filepath.Join(s.homeDir, ".claude", "plugins"))

	// Mark overrides: project agent with same filename as a global one.
	globalNames := make(map[string]struct{}, len(global))
	for _, a := range global {
		globalNames[a.FileName] = struct{}{}
	}
	for i := range project {
		if _, ok := globalNames[project[i].FileName]; ok {
			project[i].Overrides = true
		}
	}

	out := append(project, global...)
	out = append(out, plugins...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// readPluginAgents walks `~/.claude/plugins/` for every `agents/` directory
// and parses the .md files inside. Plugins install to a few different
// layouts (marketplaces, cache, repos, and nested skill-bundled agents),
// so we do a generic recursive walk rather than hard-code the shape.
//
// Each hit is tagged with its host plugin name — derived from the first
// path segment after `plugins/` — so the UI can group them.
func readPluginAgents(pluginsRoot string) []Agent {
	if _, err := os.Stat(pluginsRoot); err != nil {
		return nil
	}
	var out []Agent
	// Remember directories we've already read so the same agent doesn't
	// show up twice when the same marketplace lives under both /cache and
	// /repos. Key on the absolute path of each agents/ directory.
	seenDirs := make(map[string]struct{})

	// Dedupe by plugin+filename across buckets so agents that live in both
	// `marketplaces/` and `cache/` don't show up twice.
	seenAgent := make(map[string]struct{})

	_ = filepath.Walk(pluginsRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if !info.IsDir() {
			return nil
		}
		// Skip the cache bucket entirely — it's a build-time copy of whatever
		// sits under marketplaces/repos, so we'd double-count otherwise.
		rel, _ := filepath.Rel(pluginsRoot, path)
		relSlash := filepath.ToSlash(rel)
		if strings.HasPrefix(relSlash, "cache/") || relSlash == "cache" {
			return filepath.SkipDir
		}
		if filepath.Base(path) != "agents" {
			return nil
		}
		if _, dup := seenDirs[path]; dup {
			return filepath.SkipDir
		}
		seenDirs[path] = struct{}{}

		plugin := pluginNameFromPath(path, pluginsRoot)
		for _, a := range readAgents(path, SourcePlugin) {
			a.Plugin = plugin
			key := plugin + "::" + a.FileName
			if _, ok := seenAgent[key]; ok {
				continue
			}
			seenAgent[key] = struct{}{}
			out = append(out, a)
		}
		return filepath.SkipDir // don't recurse into an agents/ folder
	})
	return out
}

// pluginNameFromPath extracts a friendly plugin identifier from an agents/
// directory path. Layouts seen in the wild:
//   - plugins/marketplaces/<market>/plugins/<plugin>/.../agents
//   - plugins/marketplaces/<market>/agents            (single-plugin market)
//   - plugins/marketplaces/<market>/docs/<locale>/agents
//   - plugins/cache/<market>/<plugin>/<version>/.../agents
//   - plugins/repos/<owner>/<repo>/.../agents
func pluginNameFromPath(agentsDir, pluginsRoot string) string {
	rel, err := filepath.Rel(pluginsRoot, agentsDir)
	if err != nil {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 2 {
		return ""
	}
	bucket := parts[0]
	switch bucket {
	case "marketplaces":
		// <market>/plugins/<plugin>/.../agents → prefer the nested plugin.
		if len(parts) >= 4 && parts[2] == "plugins" {
			return parts[3]
		}
		// Everything else (docs, skills, .agents, flat agents/) belongs to
		// the marketplace itself, which for single-plugin markets IS the
		// plugin.
		return parts[1]
	case "cache":
		// <market>/<plugin>/<version>/.../agents
		if len(parts) >= 3 {
			return parts[2]
		}
		return parts[1]
	case "repos":
		// <owner>/<repo>/.../agents
		if len(parts) >= 3 {
			return parts[2]
		}
		return parts[1]
	}
	return parts[0]
}

// CreateAgentRequest is the input to Scanner.CreateAgent. Body can be
// empty — we'll synthesise a minimal frontmatter-only file in that
// case so the file is immediately valid.
type CreateAgentRequest struct {
	Name        string
	Description string
	Category    string
	Dest        Source // SourceGlobal or SourceProject
	Body        string // raw markdown (may be empty); frontmatter is always built for the caller
	Model       string
	Overwrite   bool
}

// CreateAgent writes a new agent .md under the user-owned root for the
// requested destination (global → ~/.claude/agents, project →
// <project>/.claude/agents). Slugifies the name; refuses duplicates
// unless Overwrite is true. Returns the absolute path.
func (s *Scanner) CreateAgent(req CreateAgentRequest) (string, error) {
	name := strings.TrimSpace(req.Name)
	desc := strings.TrimSpace(req.Description)
	if name == "" || desc == "" {
		return "", fmt.Errorf("name and description are required")
	}
	slug := slugifyAgentName(name)
	if slug == "" {
		return "", fmt.Errorf("name yielded empty slug")
	}

	var root string
	switch req.Dest {
	case SourceProject:
		root = filepath.Join(s.projectDir, ".claude", "agents")
	default:
		root = filepath.Join(s.homeDir, ".claude", "agents")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", root, err)
	}
	target := filepath.Join(root, slug+".md")
	if _, err := os.Stat(target); err == nil && !req.Overwrite {
		return "", fmt.Errorf("agent %s already exists — pick a different name", slug)
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + slug + "\n")
	b.WriteString("description: " + escapeYAMLInline(desc) + "\n")
	if req.Category != "" {
		b.WriteString("category: " + escapeYAMLInline(req.Category) + "\n")
	}
	if req.Model != "" {
		b.WriteString("model: " + escapeYAMLInline(req.Model) + "\n")
	}
	b.WriteString("---\n\n")
	if req.Body != "" {
		b.WriteString(req.Body)
		if !strings.HasSuffix(req.Body, "\n") {
			b.WriteString("\n")
		}
	} else {
		b.WriteString("# " + name + "\n\n" + desc + "\n")
	}

	// Reuse the sandboxed atomic write helper from hooks_rules.go.
	if err := s.WriteFile(target, []byte(b.String())); err != nil {
		return "", err
	}
	return target, nil
}

// slugifyAgentName produces a filesystem-safe slug (lowercase, hyphens).
func slugifyAgentName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var out strings.Builder
	prevHyphen := true
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out.WriteRune(r)
			prevHyphen = false
		case r == '-' || r == '_' || r == ' ':
			if !prevHyphen {
				out.WriteRune('-')
				prevHyphen = true
			}
		}
	}
	return strings.Trim(out.String(), "-")
}

// escapeYAMLInline is a conservative YAML single-line escape — wraps in
// double quotes if the string contains `:`, `"`, `'`, `#`, or starts
// with a character YAML treats specially.
func escapeYAMLInline(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return `""`
	}
	needsQuote := false
	if strings.ContainsAny(s, ":\"'#&*!|>%@`") {
		needsQuote = true
	}
	if strings.HasPrefix(s, "- ") || strings.HasPrefix(s, "? ") {
		needsQuote = true
	}
	if !needsQuote {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

// ReadMemory returns the contents of the agent's companion memory file, or
// "" when no file exists. Path disambiguates which agent (global vs project).
func (s *Scanner) ReadMemory(path string) (string, error) {
	memPath := memoryPath(path)
	data, err := os.ReadFile(memPath)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read memory %s: %w", memPath, err)
	}
	return string(data), nil
}

// AppendMemory atomically appends a new entry to the agent's memory file.
// Each entry gets a UTC timestamp and a trailing newline so the file stays
// parseable as simple newest-first-by-reading markdown.
func (s *Scanner) AppendMemory(path, note string) error {
	note = strings.TrimSpace(note)
	if note == "" {
		return fmt.Errorf("memory note is empty")
	}
	memPath := memoryPath(path)

	// Ensure the header exists on the first write so the file renders nicely.
	if _, err := os.Stat(memPath); os.IsNotExist(err) {
		header := "# Agent Memory\n\nAppend-only lessons. Newest entries at the bottom.\n\n"
		if err := os.WriteFile(memPath, []byte(header), 0o644); err != nil {
			return fmt.Errorf("init memory %s: %w", memPath, err)
		}
	}

	f, err := os.OpenFile(memPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open memory %s: %w", memPath, err)
	}
	defer f.Close()
	ts := nowISO()
	entry := fmt.Sprintf("\n- [%s] %s\n", ts, note)
	if _, err := f.WriteString(entry); err != nil {
		return fmt.Errorf("write memory %s: %w", memPath, err)
	}
	return nil
}

// memoryPath returns the sibling memory file for an agent .md. E.g.
// `.../agents/agent-04-universal-backend.md` → `.../agents/agent-04-universal-backend.memory.md`.
func memoryPath(agentPath string) string {
	dir, file := filepath.Split(agentPath)
	ext := filepath.Ext(file)
	stem := strings.TrimSuffix(file, ext)
	return filepath.Join(dir, stem+".memory.md")
}

func readAgents(dir string, src Source) []Agent {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Agent
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Skip templates and memory files; templates aren't agents themselves
		// and memory files are rendered per-agent via ReadMemory.
		if strings.HasSuffix(name, ".memory.md") || strings.HasPrefix(name, "AGENT_TEMPLATE") {
			continue
		}
		if filepath.Ext(name) != ".md" {
			continue
		}

		full := filepath.Join(dir, name)
		a := parseAgent(full, src)
		if a == nil {
			continue
		}
		// Does a sibling memory file exist? Cheap stat.
		if _, err := os.Stat(memoryPath(full)); err == nil {
			a.HasMemory = true
		}
		out = append(out, *a)
	}
	return out
}

// parseAgent reads an agent .md file and pulls out the interesting fields.
// Agents use one of two frontmatter styles:
//   1. Triple-dash YAML at the top (`---\nname: x\n---`).
//   2. A fenced `yaml` block under a "Metadata" section.
// We handle both.
func parseAgent(path string, src Source) *Agent {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	base := filepath.Base(path)
	a := &Agent{
		ID:       string(src) + ":" + base,
		FileName: base,
		Path:     path,
		Source:   src,
		Name:     friendlyNameFromFilename(base),
	}

	sc := bufio.NewScanner(file)
	sc.Buffer(make([]byte, 1024*64), 1024*512)

	inYAML := false
	inYAMLBlock := false
	firstH1 := ""
	description := ""

	for sc.Scan() {
		line := sc.Text()
		trim := strings.TrimSpace(line)

		// Top-of-file frontmatter: ---\nkey: val\n---
		if trim == "---" {
			if inYAML {
				inYAML = false
			} else if firstH1 == "" && description == "" {
				inYAML = true
			}
			continue
		}
		if inYAML {
			setYAMLField(a, trim)
			continue
		}

		// YAML fenced block inside the body (Metadata section)
		if strings.HasPrefix(trim, "```yaml") {
			inYAMLBlock = true
			continue
		}
		if inYAMLBlock {
			if trim == "```" {
				inYAMLBlock = false
				continue
			}
			setYAMLField(a, trim)
			continue
		}

		// First `# H1` is the fallback title.
		if strings.HasPrefix(trim, "# ") && firstH1 == "" {
			firstH1 = strings.TrimSpace(strings.TrimPrefix(trim, "# "))
			continue
		}

		// First non-empty non-heading paragraph is the description.
		if description == "" && trim != "" && !strings.HasPrefix(trim, "#") && !strings.HasPrefix(trim, "```") {
			description = trim
			// Grab another line if it extends the sentence.
			if sc.Scan() {
				next := strings.TrimSpace(sc.Text())
				if next != "" && !strings.HasPrefix(next, "#") {
					description += " " + next
				}
			}
		}
	}

	if a.Name == "" {
		a.Name = firstH1
	}
	if a.Name == "" {
		a.Name = friendlyNameFromFilename(base)
	}
	a.Description = trimLen(description, 280)
	a.Category = categoriseAgent(a.Name, base)
	return a
}

func setYAMLField(a *Agent, line string) {
	if strings.HasPrefix(line, "#") || line == "" {
		return
	}
	idx := strings.Index(line, ":")
	if idx < 0 {
		return
	}
	key := strings.TrimSpace(line[:idx])
	val := strings.TrimSpace(line[idx+1:])
	val = strings.Trim(val, `"'`)
	switch key {
	case "name":
		if val != "" {
			a.Name = val
		}
	case "model":
		a.Model = val
	case "version":
		a.Version = val
	case "thinking":
		a.Thinking = val
	case "description":
		// Frontmatter description wins over body paragraph.
		a.Description = val
	}
}

// friendlyNameFromFilename turns "agent-04-universal-backend.md" into
// "Agent-04 Universal Backend".
func friendlyNameFromFilename(base string) string {
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	parts := strings.Split(stem, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

// Category rules: first match wins. Keep the list short so the UI groups
// don't sprawl; language-specific reviewers / build-resolvers are all one
// "Language Tools" bucket rather than 10 separate groups.
var categoryRules = []struct {
	pattern *regexp.Regexp
	label   string
}{
	{regexp.MustCompile(`(?i)agent-?01|orchestrator|architect`), "Orchestration"},
	{regexp.MustCompile(`(?i)agent-?03|security|quality|review|audit`), "Quality & Security"},
	{regexp.MustCompile(`(?i)agent-?(04|05|09)|backend|data-platform|database|fullstack`), "Backend & Data"},
	{regexp.MustCompile(`(?i)agent-?(07|08)|frontend|visualization|flutter|ui`), "Frontend & UI"},
	{regexp.MustCompile(`(?i)agent-?(10|11)|ai|ml|rag|embed`), "AI & ML"},
	{regexp.MustCompile(`(?i)agent-?06|devops|cloud|infra|kubernetes|docker|migration|harness`), "DevOps & Infra"},
	{regexp.MustCompile(`(?i)agent-?12|test|e2e|qa|playwright`), "Testing"},
	{regexp.MustCompile(`(?i)agent-?02|product|research|docs|copy`), "Research & Docs"},
	{regexp.MustCompile(`(?i)chief|loop|manager|planner|refactor|build-error-resolver|cpp-build|go-build|java-build|kotlin-build|rust-build|pytorch-build`), "Utilities & Tools"},
	{regexp.MustCompile(`(?i)cpp|go|java|kotlin|python|rust|flutter|typescript|swift|ruby|php|perl|csharp`), "Language Reviewers"},
}

func categoriseAgent(name, filename string) string {
	hay := strings.ToLower(name + " " + filename)
	for _, r := range categoryRules {
		if r.pattern.MatchString(hay) {
			return r.label
		}
	}
	return "Other"
}

func trimLen(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func nowISO() string {
	return time.Now().UTC().Format("2006-01-02 15:04:05Z")
}
