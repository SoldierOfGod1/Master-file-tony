// Package platforms runs health checks against rain's operational
// services + databases. Started life as a reachability prober for the
// dashboard's platform tiles; now it's also the engine behind the
// /service monitoring tab — DNS, TLS, content validation, severity-
// tiered alerts, and an incident timeline all live in this package.
package platforms

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Criticality controls which tiles render on the compact Dashboard
// panel vs. the full /service page. "top" = both surfaces render
// them; "standard" = /service only.
type Criticality string

const (
	CriticalityTop      Criticality = "top"
	CriticalityStandard Criticality = "standard"
)

// Expectation captures the content-validation rules one Target
// wants enforced beyond "HTTP 2xx/3xx". All fields are optional;
// nil Expect means HTTP-only checks.
type Expectation struct {
	TitleContains string `json:"title_contains,omitempty"`
	BodyContains  string `json:"body_contains,omitempty"`
	MustBeHTTPS   bool   `json:"must_be_https,omitempty"`
}

// Target describes one service the monitor probes.
type Target struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Group       string       `json:"group"`        // BSS | Billing | Customer | Dev | Ops
	URL         string       `json:"url"`
	Method      string       `json:"method"`
	Description string       `json:"description,omitempty"`
	DocsURL     string       `json:"docs_url,omitempty"`
	Criticality Criticality  `json:"criticality"`  // "top" | "standard"
	Environment string       `json:"environment"`  // "sit" | "public" | "internal"
	Owner       string       `json:"owner,omitempty"`
	Expect      *Expectation `json:"expect,omitempty"`
}

// DNSCheck summarises a DNS lookup for the target's hostname.
type DNSCheck struct {
	Resolved  bool     `json:"resolved"`
	IPs       []string `json:"ips,omitempty"`
	LatencyMS int64    `json:"latency_ms"`
	Error     string   `json:"error,omitempty"`
}

// TLSCheck summarises the target's certificate. DaysToExpiry is a
// signed int so "already expired" shows as negative.
type TLSCheck struct {
	Valid        bool      `json:"valid"`
	Issuer       string    `json:"issuer,omitempty"`
	Subject      string    `json:"subject,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	DaysToExpiry int       `json:"days_to_expiry"`
	Error        string    `json:"error,omitempty"`
}

// ContentCheck holds the results of the optional content validation.
// Skipped if Target.Expect is nil.
type ContentCheck struct {
	Checked bool   `json:"checked"`
	TitleOK bool   `json:"title_ok"`
	BodyOK  bool   `json:"body_ok"`
	Error   string `json:"error,omitempty"`
}

// Status is a point-in-time snapshot of one Target's health.
type Status struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	Group         string       `json:"group"`
	URL           string       `json:"url"`
	DocsURL       string       `json:"docs_url,omitempty"`
	Criticality   Criticality  `json:"criticality"`
	Environment   string       `json:"environment"`
	Owner         string       `json:"owner,omitempty"`
	State         string       `json:"state"` // up | degraded | down | unknown
	HTTPCode      int          `json:"http_code"`
	LatencyMS     int64        `json:"latency_ms"`
	CheckedAt     time.Time    `json:"checked_at"`
	Error         string       `json:"error,omitempty"`
	DNS           DNSCheck     `json:"dns"`
	TLS           TLSCheck     `json:"tls"`
	Content       ContentCheck `json:"content"`
	FailureStreak int          `json:"failure_streak"`
	LastSuccess   time.Time    `json:"last_success,omitempty"`
	LastFailure   time.Time    `json:"last_failure,omitempty"`
	Uptime24h     float64      `json:"uptime_24h"`
	Uptime7d      float64      `json:"uptime_7d"`
	Uptime30d     float64      `json:"uptime_30d"`
}

// DefaultTargets is the full rain Service catalogue. The six "top"
// entries render on the Dashboard PlatformMonitorPanel; the
// remaining nine are /service-only.
var DefaultTargets = []Target{
	// ---- Top 6 (Dashboard + /service) ----
	{
		ID: "rain-public", Group: "Customer", Name: "rain",
		URL: "https://www.rain.co.za/", Method: http.MethodGet,
		Description: "Public rain.co.za marketing + self-service entry.",
		Criticality: CriticalityTop, Environment: "public",
		Owner:  "web-platform",
		Expect: &Expectation{MustBeHTTPS: true},
	},
	{
		ID: "rain-sit", Group: "Customer", Name: "rain SIT",
		URL: "https://ww2.sit.rain.co.za/", Method: http.MethodGet,
		Description: "SIT copy of rain self-service.",
		Criticality: CriticalityTop, Environment: "sit",
		Owner:  "web-platform",
		Expect: &Expectation{MustBeHTTPS: true},
	},
	{
		ID: "logistics", Group: "Ops", Name: "logistics",
		URL: "https://logistics-portal.bss.rain.co.za/dashboard", Method: http.MethodGet,
		Description: "Logistics portal dashboard (prod).",
		Criticality: CriticalityTop, Environment: "internal",
		Owner:  "logistics",
		Expect: &Expectation{MustBeHTTPS: true},
	},
	{
		ID: "rainmaker-web", Group: "Ops", Name: "rainmaker-web",
		URL: "https://rainmaker-web.athena.rain.co.za/login", Method: http.MethodGet,
		Description: "rainmaker-web login gate (Athena).",
		Criticality: CriticalityTop, Environment: "internal",
		Owner:  "assisted-sales",
		Expect: &Expectation{MustBeHTTPS: true, BodyContains: "login"},
	},
	{
		ID: "rica-v2", Group: "BSS", Name: "RICA",
		URL: "https://rica-v2-portal.bss.rain.co.za/login", Method: http.MethodGet,
		Description: "RICA v2 portal login.",
		Criticality: CriticalityTop, Environment: "internal",
		Owner:  "risk-and-rica",
		Expect: &Expectation{MustBeHTTPS: true, BodyContains: "login"},
	},
	{
		ID: "raingo", Group: "Customer", Name: "RainGo",
		URL: "https://www.raingo.co.za/", Method: http.MethodGet,
		Description: "RainGo customer portal.",
		Criticality: CriticalityTop, Environment: "public",
		Owner:  "raingo",
		Expect: &Expectation{MustBeHTTPS: true},
	},

	// ---- Standard 9 (/service only) ----
	{
		ID: "rapids-sit", Group: "Dev", Name: "RAPIDS",
		URL: "https://rapids-sit.vibe.rain.co.za/", Method: http.MethodGet,
		Description: "RAPIDS SIT dashboard.",
		Criticality: CriticalityStandard, Environment: "sit",
		Owner:  "data-platform",
	},
	{
		ID: "rollout-tracker", Group: "Ops", Name: "Rollout Tracker",
		URL: "https://rollouttracker-sit.vibe.rain.co.za/", Method: http.MethodGet,
		Description: "Feature rollout tracker (SIT).",
		Criticality: CriticalityStandard, Environment: "sit",
		Owner:  "platform",
	},
	{
		ID: "sparcv2-form", Group: "Ops", Name: "Sparcv2",
		URL: "https://sparcv2-form-sit.vibe.rain.co.za/sparc-v2-forms.html", Method: http.MethodGet,
		Description: "Sparc v2 form gateway (SIT).",
		Criticality: CriticalityStandard, Environment: "sit",
		Owner:  "risk-and-rica",
	},
	{
		ID: "station", Group: "Customer", Name: "Station (the101)",
		URL: "https://www.the101.info/", Method: http.MethodGet,
		Description: "theStation customer lookup UI.",
		Criticality: CriticalityStandard, Environment: "internal",
		Owner:  "ops",
	},
	{
		ID: "assisted-sales", Group: "Customer", Name: "Assisted Sales",
		URL: "https://assisted-sales.athena.rain.co.za/", Method: http.MethodGet,
		Description: "Assisted sales toolkit (Athena).",
		Criticality: CriticalityStandard, Environment: "internal",
		Owner:  "assisted-sales",
	},
	{
		ID: "risk-review-sit", Group: "BSS", Name: "New Risk Portal",
		URL: "https://risk-review-portal.sit.rain.co.za/", Method: http.MethodGet,
		Description: "New risk-review portal (SIT).",
		Criticality: CriticalityStandard, Environment: "sit",
		Owner:  "risk-and-rica",
	},
	{
		ID: "risk-portal", Group: "BSS", Name: "Risk Portal",
		URL: "https://axiom-support.bss.rain.co.za/risk-management/fraud-entry", Method: http.MethodGet,
		Description: "Legacy risk management portal (Axiom).",
		Criticality: CriticalityStandard, Environment: "internal",
		Owner:  "risk-and-rica",
	},
	{
		ID: "sebenza-sit", Group: "Ops", Name: "Sebenza",
		URL: "https://sebenza-sit.cns.rain.co.za/login", Method: http.MethodGet,
		Description: "Sebenza HR/people ops (SIT).",
		Criticality: CriticalityStandard, Environment: "sit",
		Owner:  "hr-ops",
		Expect: &Expectation{BodyContains: "login"},
	},
	{
		ID: "sparc", Group: "Ops", Name: "Sparc",
		URL: "https://sparc.rain.network/", Method: http.MethodGet,
		Description: "Sparc network-ops tooling.",
		Criticality: CriticalityStandard, Environment: "internal",
		Owner:  "network-ops",
	},
}

// Monitor polls every Target on a fixed interval and exposes the
// latest Status via Snapshot. Start with Run(ctx); cancel ctx to
// stop. Top-criticality targets poll at every tick; standard ones
// every third tick (so top-6 run every 60 s while standard run
// every 180 s on a 60 s base interval).
type Monitor struct {
	log      *slog.Logger
	interval time.Duration
	client   *http.Client
	targets  []Target

	mu           sync.RWMutex
	latest       map[string]Status
	streak       map[string]int
	lastOK       map[string]time.Time
	lastFail     map[string]time.Time
	tlsCache     map[string]TLSCheck
	tlsCheckedAt map[string]time.Time

	// History + alerting are set from main.go after construction,
	// via SetHistory / SetAlertSink. Keeping them optional means a
	// bare Monitor still works (tests, minimal deployments).
	history   HistoryWriter
	alertSink AlertSink
	tickCount int
}

// HistoryWriter accepts every tick's Status and persists it for
// uptime/latency rollups. Implemented in history.go (SQLite) and
// no-op by default so Monitor is usable stand-alone.
type HistoryWriter interface {
	Record(ctx context.Context, st Status)
	Rollup(ctx context.Context, id string) (u24h, u7d, u30d float64)
}

// AlertSink receives any alerts produced by the rules engine.
type AlertSink interface {
	Emit(ctx context.Context, a Alert, prev *Status, cur Status)
}

// NewMonitor builds a Monitor. Zero/nil targets uses DefaultTargets.
func NewMonitor(log *slog.Logger, interval time.Duration, targets []Target) *Monitor {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	if len(targets) == 0 {
		targets = DefaultTargets
	}
	return &Monitor{
		log:          log,
		interval:     interval,
		client:       &http.Client{Timeout: 8 * time.Second},
		targets:      targets,
		latest:       map[string]Status{},
		streak:       map[string]int{},
		lastOK:       map[string]time.Time{},
		lastFail:     map[string]time.Time{},
		tlsCache:     map[string]TLSCheck{},
		tlsCheckedAt: map[string]time.Time{},
	}
}

// SetHistory attaches a persistence layer. Safe to call once at
// startup before Run.
func (m *Monitor) SetHistory(h HistoryWriter) { m.history = h }

// SetAlertSink attaches an alert pipeline.
func (m *Monitor) SetAlertSink(s AlertSink) { m.alertSink = s }

// Run executes the probe loop. Top-criticality targets are probed
// every tick; standard every third tick (so the default 60 s
// interval gives top=60 s, standard=180 s).
func (m *Monitor) Run(ctx context.Context) {
	m.probeAll(ctx, true)
	t := time.NewTicker(m.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.tickCount++
			includeStandard := m.tickCount%3 == 0
			m.probeAll(ctx, includeStandard)
		}
	}
}

// Targets returns the static catalogue.
func (m *Monitor) Targets() []Target {
	out := make([]Target, len(m.targets))
	copy(out, m.targets)
	return out
}

// Snapshot is the slice the API returns.
func (m *Monitor) Snapshot() []Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Status, 0, len(m.targets))
	for _, t := range m.targets {
		if st, ok := m.latest[t.ID]; ok {
			out = append(out, st)
			continue
		}
		out = append(out, Status{
			ID: t.ID, Name: t.Name, Group: t.Group, URL: t.URL,
			DocsURL: t.DocsURL, State: "unknown",
			Criticality: t.Criticality, Environment: t.Environment,
			Owner: t.Owner,
		})
	}
	return out
}

func (m *Monitor) probeAll(ctx context.Context, includeStandard bool) {
	var wg sync.WaitGroup
	for _, t := range m.targets {
		if !includeStandard && t.Criticality != CriticalityTop {
			continue
		}
		wg.Add(1)
		go func(t Target) {
			defer wg.Done()
			prev, prevOK := m.latestStatus(t.ID)
			st := m.probe(ctx, t)
			m.commit(ctx, t, st, prev, prevOK)
		}(t)
	}
	wg.Wait()
}

func (m *Monitor) latestStatus(id string) (Status, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.latest[id]
	return s, ok
}

func (m *Monitor) commit(ctx context.Context, t Target, st Status, prev Status, prevOK bool) {
	m.mu.Lock()
	if st.State == "up" {
		m.streak[t.ID] = 0
		m.lastOK[t.ID] = st.CheckedAt
	} else {
		m.streak[t.ID]++
		m.lastFail[t.ID] = st.CheckedAt
	}
	st.FailureStreak = m.streak[t.ID]
	st.LastSuccess = m.lastOK[t.ID]
	st.LastFailure = m.lastFail[t.ID]
	m.latest[t.ID] = st
	m.mu.Unlock()

	// Record + rollup — both best-effort, both optional.
	if m.history != nil {
		m.history.Record(ctx, st)
		u24h, u7d, u30d := m.history.Rollup(ctx, t.ID)
		m.mu.Lock()
		updated := m.latest[t.ID]
		updated.Uptime24h = u24h
		updated.Uptime7d = u7d
		updated.Uptime30d = u30d
		m.latest[t.ID] = updated
		m.mu.Unlock()
	}
	if m.alertSink != nil {
		for _, a := range evaluateAlerts(t, &prev, st, prevOK) {
			m.alertSink.Emit(ctx, a, &prev, st)
		}
	}
}
