package server

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// RegisterQualityRoutes wires /api/v1/quality onto the mux. The single
// endpoint runs 3 gates in parallel (go vet, tsc, secret scan) and returns
// a consolidated result so the Dashboard panel can render 3 LEDs from one
// round-trip.
func RegisterQualityRoutes(mux *http.ServeMux, api *API) {
	mux.HandleFunc("POST /api/v1/quality", api.handleRunQuality)
	mux.HandleFunc("GET /api/v1/quality", api.handleRunQuality)
}

type gateResult struct {
	OK       bool   `json:"ok"`
	Output   string `json:"output"`
	Duration int64  `json:"duration_ms"`
	Skipped  bool   `json:"skipped,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type secretHit struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Rule string `json:"rule"`
	Text string `json:"text"`
}

type qualityResponse struct {
	Go         gateResult  `json:"go_vet"`
	TypeScript gateResult  `json:"typescript"`
	Secrets    gateResult  `json:"secrets"`
	Hits       []secretHit `json:"hits,omitempty"`
	RanAt      time.Time   `json:"ran_at"`
}

// handleRunQuality spawns the three gates concurrently with per-gate
// timeouts so a hung subprocess can't wedge the whole endpoint. Short
// responses — we truncate gate output to ~4kB each.
func (a *API) handleRunQuality(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	projectRoot := resolveProjectRoot()

	goCh := make(chan gateResult, 1)
	tsCh := make(chan gateResult, 1)
	secCh := make(chan struct {
		gate gateResult
		hits []secretHit
	}, 1)

	go func() { goCh <- runGoVet(ctx, filepath.Join(projectRoot, "backend")) }()
	go func() { tsCh <- runTSCheck(ctx, filepath.Join(projectRoot, "frontend-react")) }()
	go func() {
		g, h := runSecretScan(ctx, projectRoot)
		secCh <- struct {
			gate gateResult
			hits []secretHit
		}{g, h}
	}()

	sec := <-secCh
	resp := qualityResponse{
		Go:         <-goCh,
		TypeScript: <-tsCh,
		Secrets:    sec.gate,
		Hits:       sec.hits,
		RanAt:      time.Now().UTC(),
	}
	jsonOK(w, resp)
}

// resolveProjectRoot walks up from cwd looking for a sibling pair of
// `backend/` and `frontend-react/` so the gate commands run in the right
// place even when the server was started from ./backend.
func resolveProjectRoot() string {
	dir, _ := os.Getwd()
	for i := 0; i < 6; i++ {
		if stat1, err := os.Stat(filepath.Join(dir, "backend")); err == nil && stat1.IsDir() {
			if stat2, err := os.Stat(filepath.Join(dir, "frontend-react")); err == nil && stat2.IsDir() {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	cwd, _ := os.Getwd()
	return cwd
}

func runGoVet(ctx context.Context, dir string) gateResult {
	if _, err := os.Stat(dir); err != nil {
		return gateResult{OK: true, Skipped: true, Reason: "backend dir not found"}
	}
	return runGate(ctx, dir, "go", "vet", "./...")
}

func runTSCheck(ctx context.Context, dir string) gateResult {
	if _, err := os.Stat(filepath.Join(dir, "package.json")); err != nil {
		return gateResult{OK: true, Skipped: true, Reason: "frontend-react package.json not found"}
	}
	// tsc is the fastest reliable gate — skip eslint to keep p95 < 30s.
	return runGate(ctx, dir, "npx", "--no-install", "tsc", "--noEmit")
}

func runGate(ctx context.Context, dir, bin string, args ...string) gateResult {
	start := time.Now()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	dur := time.Since(start).Milliseconds()
	result := gateResult{
		OK:       err == nil,
		Output:   truncate(string(out), 4096),
		Duration: dur,
	}
	if err != nil && ctx.Err() == context.DeadlineExceeded {
		result.Reason = "timed out"
	}
	return result
}

// Secret-scan patterns. Tuned for low false-positives on this repo — keys
// that typically leak are AWS, OpenAI, Anthropic, GitHub, and generic
// `password = "..."` assignments. Each rule has an optional allow()
// predicate that re-checks the captured value to drop obvious placeholders
// and config-key literals.
type secretRule struct {
	name  string
	re    *regexp.Regexp
	allow func(string) bool // returns true when the match is a real hit
}

var secretRules = []secretRule{
	{name: "aws-access-key", re: regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{name: "openai-key", re: regexp.MustCompile(`sk-[A-Za-z0-9]{32,}`)},
	{name: "anthropic-key", re: regexp.MustCompile(`sk-ant-[A-Za-z0-9\-_]{40,}`)},
	{name: "github-token", re: regexp.MustCompile(`gh[psuro]_[A-Za-z0-9]{30,}`)},
	{
		name:  "generic-password",
		re:    regexp.MustCompile(`(?i)(password|passwd)\s*[:=]\s*["']([^"']{8,})["']`),
		allow: isLikelyRealPassword,
	},
}

// placeholderValues trips the generic-password rule down to only real hits.
// These strings appear as example passwords in our vendored docs and in
// third-party examples; none of them are actual credentials.
var placeholderValues = []string{
	"password", "passw0rd", "password123", "changeme", "example",
	"placeholder", "secure", "securep@ss", "yourpassword", "<password",
	"redacted", "xxxx", "p@ssword", "admin123", "123456",
}

// isLikelyRealPassword filters out:
//   - Dotted config keys like "axiom.password" (these are app_settings keys)
//   - Env-var references like "${FOO}"
//   - Common example values
func isLikelyRealPassword(value string) bool {
	v := strings.ToLower(value)
	// Env-var interpolation — not a baked secret.
	if strings.HasPrefix(v, "${") || strings.HasPrefix(v, "$") {
		return false
	}
	// Dotted lowercase config keys — e.g. "axiom.password".
	if strings.Contains(v, ".") && !strings.ContainsAny(v, " /\\@#!") {
		parts := strings.Split(v, ".")
		if len(parts) == 2 && len(parts[0]) > 0 && len(parts[1]) > 0 {
			return false
		}
	}
	for _, bad := range placeholderValues {
		if strings.Contains(v, bad) {
			return false
		}
	}
	return true
}

var scanSkipDirs = map[string]bool{
	"node_modules": true, ".git": true, "dist": true, "build": true,
	".next": true, "coverage": true, ".claude": true, ".vscode": true,
	"vendor": true, "target": true, "bin": true, "obj": true,
	// Vendored third-party docs and example trees — loud and noisy for
	// secret scanning. Example credentials in their own docs aren't our
	// leak risk.
	"everything-claude-code": true, "rain-mcp": true, ".worktrees": true,
	"tests": true, "testdata": true, "examples": true, "docs": true,
}

// runSecretScan walks the project root looking for hits. Only scans files
// < 256kB; skips binaries and common vendor dirs. The gate "fails" when
// any hit outside a test/fixture file is found.
func runSecretScan(ctx context.Context, root string) (gateResult, []secretHit) {
	start := time.Now()
	var hits []secretHit
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if scanSkipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Size() > 256*1024 {
			return nil
		}
		if !isTextExt(path) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for _, rule := range secretRules {
			for _, match := range rule.re.FindAllSubmatchIndex(data, -1) {
				// match indexes: 0,1 full match; 2*n,2*n+1 group n.
				full := string(data[match[0]:match[1]])
				// For generic-password, group 2 captures the value.
				if rule.allow != nil && len(match) >= 6 {
					val := string(data[match[4]:match[5]])
					if !rule.allow(val) {
						continue
					}
				}
				line := 1 + strings.Count(string(data[:match[0]]), "\n")
				hits = append(hits, secretHit{
					File: trimRoot(path, root),
					Line: line,
					Rule: rule.name,
					Text: trimMatch(full),
				})
			}
		}
		return nil
	})
	dur := time.Since(start).Milliseconds()
	gate := gateResult{
		OK:       err == nil && len(hits) == 0,
		Duration: dur,
	}
	if err != nil {
		gate.Output = err.Error()
	} else if len(hits) > 0 {
		gate.Output = formatHitSummary(hits)
	} else {
		gate.Output = "clean"
	}
	return gate, hits
}

func isTextExt(path string) bool {
	// Intentionally excludes .md — markdown is usually docs/examples and
	// generates huge false-positive volume. We still scan config formats
	// (toml/yaml/env/json) where real secrets tend to land.
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".json", ".toml",
		".yaml", ".yml", ".env", ".sh", ".py", ".sql":
		return true
	}
	return false
}

func trimRoot(path, root string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

func trimMatch(s string) string {
	if len(s) > 60 {
		return s[:8] + "…" + s[len(s)-8:]
	}
	return s
}

func formatHitSummary(hits []secretHit) string {
	var b strings.Builder
	for i, h := range hits {
		if i >= 10 {
			b.WriteString("… (truncated)\n")
			break
		}
		b.WriteString(h.File)
		b.WriteString(":")
		b.WriteString(intStr(h.Line))
		b.WriteString(" · ")
		b.WriteString(h.Rule)
		b.WriteString("\n")
	}
	return b.String()
}

func intStr(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n… (truncated)"
}
