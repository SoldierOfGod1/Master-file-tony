// Package runner owns the lifecycle of local dev servers launched
// from the Projects page. One Manager instance is wired into the API
// and holds an in-memory map of project_id → running Group. A Group
// bundles up to two Processes (backend + frontend) so the UI can
// "start project" with a single click and get both halves up.
//
// The package is intentionally self-contained: no DB access, no
// HTTP handling. The HTTP layer in the server package calls the
// Manager and serialises results.
package runner

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/SoldierOfGod1/command-centre/internal/event"
)

// State is the lifecycle state of a single Process.
type State string

const (
	StateStopped  State = "stopped"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateCrashed  State = "crashed"
	StateStopping State = "stopping"
)

// Process is one running dev server (backend or frontend).
type Process struct {
	Component Component  `json:"component"`
	State     State      `json:"state"`
	PID       int        `json:"pid"`
	StartedAt time.Time  `json:"started_at,omitempty"`
	ExitedAt  time.Time  `json:"exited_at,omitempty"`
	ExitCode  int        `json:"exit_code"`
	Error     string     `json:"error,omitempty"`

	cmd      *exec.Cmd
	cancel   context.CancelFunc
	logBuf   *ringBuffer
	killOnce sync.Once
}

// Group is the bundle of processes launched for one project id.
type Group struct {
	ProjectID string     `json:"project_id"`
	Processes []*Process `json:"processes"`
}

// Manager owns all running groups.
type Manager struct {
	mu     sync.RWMutex
	groups map[string]*Group
	log    *slog.Logger
	bus    *event.Bus
}

// NewManager wires the dependencies. The bus lets the WS hub rebroadcast
// runner events (log lines, state changes) to any connected frontend.
func NewManager(log *slog.Logger, bus *event.Bus) *Manager {
	return &Manager{
		groups: make(map[string]*Group),
		log:    log,
		bus:    bus,
	}
}

// Start launches all detected components for the given project. It
// returns an error if the project is already running, the path is
// empty, or no runnable components were detected.
func (m *Manager) Start(projectID, localPath string) (*Group, error) {
	if projectID == "" || localPath == "" {
		return nil, errors.New("project id and local path are required")
	}

	m.mu.Lock()
	if g := m.groups[projectID]; g != nil && groupLive(g) {
		m.mu.Unlock()
		return g, errors.New("project is already running — stop it first")
	}
	m.mu.Unlock()

	comps := Detect(localPath)
	if len(comps) == 0 {
		return nil, fmt.Errorf("no runnable components found under %s (expected go.mod and/or package.json)", localPath)
	}

	g := &Group{ProjectID: projectID}
	for _, c := range comps {
		p := &Process{
			Component: c,
			State:     StateStarting,
			logBuf:    newRingBuffer(500),
		}
		if c.Port > 0 && portInUse(c.Port) {
			p.State = StateCrashed
			p.Error = fmt.Sprintf("port %d already in use", c.Port)
			g.Processes = append(g.Processes, p)
			m.publish(projectID, p, "port-conflict", "")
			continue
		}
		g.Processes = append(g.Processes, p)
		if err := m.spawn(projectID, p); err != nil {
			p.State = StateCrashed
			p.Error = err.Error()
			m.publish(projectID, p, "spawn-failed", err.Error())
			continue
		}
	}

	m.mu.Lock()
	m.groups[projectID] = g
	m.mu.Unlock()
	return g, nil
}

// Stop terminates every process in the group. Returns an error only
// if the group isn't tracked. Kills that fail on individual processes
// are logged but don't abort the loop.
func (m *Manager) Stop(projectID string) error {
	m.mu.Lock()
	g := m.groups[projectID]
	m.mu.Unlock()
	if g == nil {
		return errors.New("project is not running")
	}
	for _, p := range g.Processes {
		p.State = StateStopping
		m.publish(projectID, p, "stopping", "")
		m.killProcess(p)
	}
	return nil
}

// StopAll kills every tracked group. Called from main.go on shutdown
// so Command Centre never orphans a dev server.
func (m *Manager) StopAll() {
	m.mu.RLock()
	ids := make([]string, 0, len(m.groups))
	for id := range m.groups {
		ids = append(ids, id)
	}
	m.mu.RUnlock()
	for _, id := range ids {
		_ = m.Stop(id)
	}
}

// Status returns a snapshot of one project's runner state.
func (m *Manager) Status(projectID string) (*Group, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	g, ok := m.groups[projectID]
	return g, ok
}

// All returns every tracked group — used by the frontend to paint
// LEDs on the Projects page in a single fetch.
func (m *Manager) All() []*Group {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Group, 0, len(m.groups))
	for _, g := range m.groups {
		out = append(out, g)
	}
	return out
}

// Logs returns the tail of a specific component's ring buffer. The
// index matches the component's position in the Group.Processes slice.
func (m *Manager) Logs(projectID string, idx, tail int) []LogLine {
	m.mu.RLock()
	g := m.groups[projectID]
	m.mu.RUnlock()
	if g == nil || idx < 0 || idx >= len(g.Processes) {
		return nil
	}
	return g.Processes[idx].logBuf.tail(tail)
}

// spawn starts one Process. Must be called with m unlocked.
func (m *Manager) spawn(projectID string, p *Process) error {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, p.Component.Command, p.Component.Args...)
	cmd.Dir = p.Component.Dir
	applyProcessAttrs(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return err
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return err
	}
	p.cmd = cmd
	p.cancel = cancel
	p.PID = cmd.Process.Pid
	p.StartedAt = time.Now()
	m.publish(projectID, p, "started", "")

	go m.streamLogs(projectID, p, "stdout", stdout)
	go m.streamLogs(projectID, p, "stderr", stderr)

	go func() {
		err := cmd.Wait()
		p.ExitedAt = time.Now()
		if exitErr, ok := err.(*exec.ExitError); ok {
			p.ExitCode = exitErr.ExitCode()
		} else if err == nil {
			p.ExitCode = 0
		}
		if p.State == StateStopping {
			p.State = StateStopped
		} else {
			p.State = StateCrashed
			if err != nil {
				p.Error = err.Error()
			}
		}
		m.publish(projectID, p, "exited", p.Error)
	}()

	if p.Component.Port > 0 {
		go m.probeHealth(projectID, p)
	} else {
		// No port to probe — optimistically flip to running after a
		// short delay so the UI reflects activity.
		go func() {
			time.Sleep(1500 * time.Millisecond)
			if p.State == StateStarting {
				p.State = StateRunning
				m.publish(projectID, p, "running", "")
			}
		}()
	}
	return nil
}

// probeHealth polls the component's port for up to 20 seconds and
// flips the State to running as soon as we get a TCP connect. If the
// process exits before the timeout, the exit handler will have
// already moved it to crashed — in which case we bail.
func (m *Manager) probeHealth(projectID string, p *Process) {
	deadline := time.Now().Add(20 * time.Second)
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(p.Component.Port))
	for time.Now().Before(deadline) {
		if p.State == StateCrashed || p.State == StateStopped || p.State == StateStopping {
			return
		}
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			p.State = StateRunning
			p.Component.HealthURL = "http://127.0.0.1:" + strconv.Itoa(p.Component.Port)
			// Try a single HTTP GET to enrich the state, but don't
			// fail health if the server refuses GET /.
			hc := &http.Client{Timeout: 500 * time.Millisecond}
			resp, _ := hc.Get(p.Component.HealthURL)
			if resp != nil {
				resp.Body.Close()
			}
			m.publish(projectID, p, "running", "")
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	if p.State == StateStarting {
		p.State = StateCrashed
		p.Error = "health probe timed out"
		m.publish(projectID, p, "health-timeout", p.Error)
	}
}

// streamLogs copies one pipe into the ring buffer and the event bus.
func (m *Manager) streamLogs(projectID string, p *Process, stream string, r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		entry := LogLine{
			Time:   time.Now(),
			Stream: stream,
			Role:   p.Component.Role,
			Line:   line,
		}
		p.logBuf.add(entry)
		if m.bus != nil {
			m.bus.PublishJSON("runner.log", map[string]any{
				"project_id": projectID,
				"role":       p.Component.Role,
				"stream":     stream,
				"line":       line,
				"at":         entry.Time.Format(time.RFC3339Nano),
			})
		}
	}
}

func (m *Manager) killProcess(p *Process) {
	p.killOnce.Do(func() {
		if p.cmd == nil || p.cmd.Process == nil {
			return
		}
		// Windows + posix process-tree kill lives in platform files.
		if err := killTree(p.cmd); err != nil {
			m.log.Warn("kill tree failed", "pid", p.PID, "error", err)
			if p.cancel != nil {
				p.cancel()
			}
		}
	})
}

func (m *Manager) publish(projectID string, p *Process, kind, msg string) {
	if m.bus == nil {
		return
	}
	m.bus.PublishJSON("runner.state", map[string]any{
		"project_id": projectID,
		"role":       p.Component.Role,
		"state":      string(p.State),
		"event":      kind,
		"message":    msg,
		"pid":        p.PID,
		"port":       p.Component.Port,
		"at":         time.Now().Format(time.RFC3339Nano),
	})
}

// groupLive returns true if at least one process is not yet terminal.
func groupLive(g *Group) bool {
	for _, p := range g.Processes {
		if p.State == StateStarting || p.State == StateRunning {
			return true
		}
	}
	return false
}

// portInUse checks whether 127.0.0.1:port is already bound. Used
// to refuse a start when something else (maybe an older orphaned
// dev server) owns the port we'd collide with.
func portInUse(port int) bool {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return true
	}
	_ = ln.Close()
	return false
}
