package skills

import (
	"context"
	"log/slog"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// HealthStatus summarises the last reachability probe for one MCP server.
// For http transports it's a real HTTP ping; for stdio/command transports
// we check the command exists on PATH (can't launch the full process here
// without side-effects).
type HealthStatus struct {
	Name      string    `json:"name"`
	Status    string    `json:"status"`     // "up" | "down" | "local" | "unknown"
	LatencyMS int64     `json:"latency_ms"`
	CheckedAt time.Time `json:"checked_at"`
	Error     string    `json:"error,omitempty"`
}

// HealthMonitor polls every known MCP server on a fixed interval and keeps
// the latest status in memory. Exposed via /api/v1/mcp/health.
type HealthMonitor struct {
	log      *slog.Logger
	interval time.Duration
	client   *http.Client

	mu      sync.RWMutex
	latest  map[string]HealthStatus
}

// NewHealthMonitor returns a monitor ticking at `interval`. Caller should
// start it with Run(ctx) in a goroutine.
func NewHealthMonitor(log *slog.Logger, interval time.Duration) *HealthMonitor {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	return &HealthMonitor{
		log:      log,
		interval: interval,
		client:   &http.Client{Timeout: 3 * time.Second},
		latest:   make(map[string]HealthStatus),
	}
}

// Run ticks forever (or until ctx is done), probing every MCP server in
// parallel per tick. Safe to run as a single background goroutine.
func (m *HealthMonitor) Run(ctx context.Context, projectDir string) {
	m.probeAll(ctx, projectDir) // immediate first pass so the UI isn't empty
	t := time.NewTicker(m.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.probeAll(ctx, projectDir)
		}
	}
}

// Snapshot returns the latest health map as a slice for the API.
func (m *HealthMonitor) Snapshot() []HealthStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]HealthStatus, 0, len(m.latest))
	for _, s := range m.latest {
		out = append(out, s)
	}
	return out
}

func (m *HealthMonitor) probeAll(ctx context.Context, projectDir string) {
	servers := New(projectDir).ListMCPServers()
	var wg sync.WaitGroup
	for _, srv := range servers {
		if !srv.Enabled {
			continue
		}
		wg.Add(1)
		go func(s MCPServer) {
			defer wg.Done()
			st := m.probe(ctx, s)
			m.mu.Lock()
			m.latest[s.Name] = st
			m.mu.Unlock()
		}(srv)
	}
	wg.Wait()
}

func (m *HealthMonitor) probe(ctx context.Context, s MCPServer) HealthStatus {
	now := time.Now().UTC()
	if s.URL != "" {
		start := time.Now()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
		if err != nil {
			return HealthStatus{Name: s.Name, Status: "down", CheckedAt: now, Error: err.Error()}
		}
		resp, err := m.client.Do(req)
		lat := time.Since(start).Milliseconds()
		if err != nil {
			return HealthStatus{Name: s.Name, Status: "down", LatencyMS: lat, CheckedAt: now, Error: err.Error()}
		}
		_ = resp.Body.Close()
		// Any HTTP response means the server is at least reachable. MCP
		// servers typically 401/405/404 on a plain GET — that still counts
		// as "up" for our purposes.
		return HealthStatus{Name: s.Name, Status: "up", LatencyMS: lat, CheckedAt: now}
	}
	if s.Command != "" {
		if _, err := exec.LookPath(commandHead(s.Command)); err != nil {
			return HealthStatus{Name: s.Name, Status: "down", CheckedAt: now, Error: "command not on PATH"}
		}
		return HealthStatus{Name: s.Name, Status: "local", CheckedAt: now}
	}
	return HealthStatus{Name: s.Name, Status: "unknown", CheckedAt: now}
}

// commandHead returns the first token of the command string — handles both
// "npx @foo/bar" and Windows-friendly "cmd /c npx …" invocations.
func commandHead(cmd string) string {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return ""
	}
	head := parts[0]
	if runtime.GOOS == "windows" && strings.EqualFold(head, "cmd") && len(parts) >= 3 {
		return parts[2]
	}
	return head
}
