package agents

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ListHooks enumerates files inside .claude/hooks/. Each entry exposes its
// size + a best-guess "kind" (script / docs / config) and "language" so the
// UI can syntax-highlight the preview panel. Project hooks first (they're
// the authoritative ones for this repo); global hooks folded in after.
func (s *Scanner) ListHooks() []Hook {
	var out []Hook
	out = append(out, readHooks(filepath.Join(s.projectDir, ".claude", "hooks"))...)
	out = append(out, readHooks(filepath.Join(s.homeDir, ".claude", "hooks"))...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func readHooks(dir string) []Hook {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Hook
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		name := e.Name()
		full := filepath.Join(dir, name)

		kind, lang := classifyHook(name)
		out = append(out, Hook{
			Name:       name,
			Path:       full,
			Kind:       kind,
			Language:   lang,
			SizeBytes:  info.Size(),
			Executable: isExecutable(name),
		})
	}
	return out
}

// classifyHook maps a filename extension to (kind, language) hints for the UI.
func classifyHook(name string) (kind, lang string) {
	lower := strings.ToLower(name)
	switch filepath.Ext(lower) {
	case ".sh", ".bash":
		return "script", "bash"
	case ".ps1":
		return "script", "powershell"
	case ".bat", ".cmd":
		return "script", "batch"
	case ".py":
		return "script", "python"
	case ".js", ".mjs", ".cjs":
		return "script", "javascript"
	case ".ts":
		return "script", "typescript"
	case ".go":
		return "script", "go"
	case ".json":
		return "config", "json"
	case ".yaml", ".yml":
		return "config", "yaml"
	case ".toml":
		return "config", "toml"
	case ".md", ".markdown":
		return "docs", "markdown"
	case ".txt":
		return "docs", "text"
	default:
		return "other", "text"
	}
}

// isExecutable is a best-effort check. On Windows we rely on the file
// extension; on Unix we'd check mode bits, but we only need a hint for the
// UI so the extension check is enough cross-platform.
func isExecutable(name string) bool {
	lower := strings.ToLower(name)
	switch filepath.Ext(lower) {
	case ".sh", ".bash", ".ps1", ".bat", ".cmd", ".exe":
		return true
	}
	return false
}

// ListRules walks .claude/rules/ recursively (at most one level deep, which
// matches how rules are actually organised — common/ plus one per language).
// Project rules come first so overrides are visible at the top.
func (s *Scanner) ListRules() []Rule {
	var out []Rule
	out = append(out, readRules(filepath.Join(s.projectDir, ".claude", "rules"), SourceProject)...)
	out = append(out, readRules(filepath.Join(s.homeDir, ".claude", "rules"), SourceGlobal)...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func readRules(root string, src Source) []Rule {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []Rule
	for _, e := range entries {
		if e.IsDir() {
			sub := filepath.Join(root, e.Name())
			subEntries, err := os.ReadDir(sub)
			if err != nil {
				continue
			}
			for _, se := range subEntries {
				if se.IsDir() || !strings.HasSuffix(se.Name(), ".md") {
					continue
				}
				out = append(out, Rule{
					Name:   strings.TrimSuffix(se.Name(), ".md"),
					Path:   filepath.Join(sub, se.Name()),
					Group:  e.Name(),
					Source: src,
				})
			}
			continue
		}
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		out = append(out, Rule{
			Name:   strings.TrimSuffix(e.Name(), ".md"),
			Path:   filepath.Join(root, e.Name()),
			Group:  "root",
			Source: src,
		})
	}
	return out
}

// ReadFile returns raw bytes for any of the agent/hook/rule paths above.
// Used by the frontend preview panel. Caller is expected to have already
// sanity-checked that the path is under the agents/hooks/rules directories
// we care about — but we enforce that here too, just in case.
func (s *Scanner) ReadFile(path string) (string, error) {
	clean := filepath.Clean(path)
	if !s.isAllowedPath(clean) {
		return "", os.ErrPermission
	}
	data, err := os.ReadFile(clean)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// isAllowedPath ensures the requested file is inside one of the scanned
// directories. Prevents the ReadFile endpoint from being abused as a
// general file-read primitive.
func (s *Scanner) isAllowedPath(clean string) bool {
	roots := []string{
		filepath.Join(s.homeDir, ".claude", "agents"),
		filepath.Join(s.homeDir, ".claude", "hooks"),
		filepath.Join(s.homeDir, ".claude", "rules"),
		filepath.Join(s.projectDir, ".claude", "agents"),
		filepath.Join(s.projectDir, ".claude", "hooks"),
		filepath.Join(s.projectDir, ".claude", "rules"),
	}
	for _, root := range roots {
		cleanRoot := filepath.Clean(root)
		if strings.HasPrefix(clean, cleanRoot+string(filepath.Separator)) || clean == cleanRoot {
			return true
		}
	}
	return false
}
