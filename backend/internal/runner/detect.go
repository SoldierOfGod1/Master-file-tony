package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Component describes one runnable unit inside a project directory.
// A single project can have 0–2 (backend + frontend) or more.
type Component struct {
	Role     string   `json:"role"`     // "backend" | "frontend"
	Label    string   `json:"label"`    // display name
	Dir      string   `json:"dir"`      // absolute path to run from
	Command  string   `json:"command"`  // executable (e.g. "go", "npm", "bun")
	Args     []string `json:"args"`     // argv after command
	Port     int      `json:"port"`     // 0 if unknown
	HealthURL string  `json:"health_url"`
}

// Detect walks the project root looking for a Go backend and/or a JS
// frontend. The first go.mod within the root or a direct `backend/`
// child becomes the backend component. The first package.json that
// declares a `dev` script (or `start`) within the root or a direct
// `frontend*/` child becomes the frontend component.
//
// Only top-level and one-level-deep directories are inspected — we
// don't recurse into node_modules or nested mono-repo packages, both
// to keep detection fast and to avoid picking up stray examples.
func Detect(projectRoot string) []Component {
	if projectRoot == "" {
		return nil
	}
	info, err := os.Stat(projectRoot)
	if err != nil || !info.IsDir() {
		return nil
	}

	var comps []Component

	// Backend: first go.mod in project root or ./backend*/
	if c := detectGo(projectRoot); c != nil {
		comps = append(comps, *c)
	}
	// Frontend: first package.json with a dev/start script
	if c := detectJS(projectRoot); c != nil {
		comps = append(comps, *c)
	}
	return comps
}

func detectGo(root string) *Component {
	candidates := []string{root}
	candidates = append(candidates, oneLevelMatches(root, "backend")...)
	candidates = append(candidates, oneLevelMatches(root, "server")...)
	candidates = append(candidates, oneLevelMatches(root, "api")...)

	for _, dir := range candidates {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
			continue
		}
		args := []string{"run", "./cmd/server"}
		if _, err := os.Stat(filepath.Join(dir, "cmd", "server", "main.go")); err != nil {
			// fall back to ./... if there's a main.go at the module root
			if _, err := os.Stat(filepath.Join(dir, "main.go")); err == nil {
				args = []string{"run", "."}
			}
		}
		return &Component{
			Role:     "backend",
			Label:    filepath.Base(dir) + " (go)",
			Dir:      dir,
			Command:  "go",
			Args:     args,
			Port:     guessGoPort(dir),
			HealthURL: "",
		}
	}
	return nil
}

func detectJS(root string) *Component {
	candidates := []string{root}
	candidates = append(candidates, oneLevelMatches(root, "frontend")...)
	candidates = append(candidates, oneLevelMatches(root, "web")...)
	candidates = append(candidates, oneLevelMatches(root, "app")...)
	candidates = append(candidates, oneLevelMatches(root, "ui")...)

	type pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	for _, dir := range candidates {
		data, err := os.ReadFile(filepath.Join(dir, "package.json"))
		if err != nil {
			continue
		}
		var p pkg
		if err := json.Unmarshal(data, &p); err != nil {
			continue
		}
		script := ""
		switch {
		case p.Scripts["dev"] != "":
			script = "dev"
		case p.Scripts["start"] != "":
			script = "start"
		default:
			continue
		}
		manager, args := npmRun(dir, script)
		return &Component{
			Role:     "frontend",
			Label:    filepath.Base(dir) + " (" + script + ")",
			Dir:      dir,
			Command:  manager,
			Args:     args,
			Port:     guessJSPort(p.Scripts[script]),
			HealthURL: "",
		}
	}
	return nil
}

// npmRun returns the command + args to invoke an npm script, preferring
// whichever lockfile the project carries (bun → bun, pnpm → pnpm,
// yarn → yarn, else npm).
func npmRun(dir, script string) (string, []string) {
	if _, err := os.Stat(filepath.Join(dir, "bun.lockb")); err == nil {
		return "bun", []string{"run", script}
	}
	if _, err := os.Stat(filepath.Join(dir, "pnpm-lock.yaml")); err == nil {
		return "pnpm", []string{"run", script}
	}
	if _, err := os.Stat(filepath.Join(dir, "yarn.lock")); err == nil {
		return "yarn", []string{script}
	}
	return "npm", []string{"run", script}
}

func oneLevelMatches(root, prefix string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []string
	lowPrefix := strings.ToLower(prefix)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if strings.HasPrefix(strings.ToLower(e.Name()), lowPrefix) {
			out = append(out, filepath.Join(root, e.Name()))
		}
	}
	return out
}

// guessGoPort peeks at config.toml (the convention used across this
// codebase) to extract a `port =` line. Returns 0 if unreadable.
func guessGoPort(dir string) int {
	data, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "port") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		v := strings.TrimSpace(parts[1])
		v = strings.TrimSuffix(v, "\r")
		if p, err := strconv.Atoi(v); err == nil && p > 0 && p < 65536 {
			return p
		}
	}
	return 0
}

// guessJSPort peeks at the dev script text (e.g. "vite --port 5173")
// and pulls out a --port flag when one is explicit. Returns 0 when
// absent — the frontend may pick an ephemeral port and we'll still
// run, just without a clickable "Open" button.
func guessJSPort(script string) int {
	tokens := strings.Fields(script)
	for i, t := range tokens {
		if t == "--port" && i+1 < len(tokens) {
			if p, err := strconv.Atoi(tokens[i+1]); err == nil && p > 0 && p < 65536 {
				return p
			}
		}
		if strings.HasPrefix(t, "--port=") {
			if p, err := strconv.Atoi(strings.TrimPrefix(t, "--port=")); err == nil && p > 0 && p < 65536 {
				return p
			}
		}
	}
	if strings.Contains(script, "vite") {
		return 5173
	}
	if strings.Contains(script, "next") {
		return 3000
	}
	return 0
}
