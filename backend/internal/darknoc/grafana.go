package darknoc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/SoldierOfGod1/command-centre/internal/store"
)

// GrafanaProxy talks to a single Grafana instance via its HTTP API
// using a service-account token (Bearer auth). Used by Cybertron's
// chat tool to answer "what's the latest 5G CRITICAL count" without
// the operator opening the dashboard.
//
// All credentials come from the existing connections registry, row
// id `grafana-prod` (driver=grafana). Host = base URL (with scheme),
// Password = the token. User/Database/Port are unused — Grafana's
// API is single-tenant.
type GrafanaProxy struct {
	store *store.Store
	log   *slog.Logger
	conn  string
	dash  string // dashboard UID — defaults to --cEk8A4k
	hc    *http.Client
}

// GrafanaPanelData is the shape Cybertron returns for a panel query:
// the dashboard + panel context, then a small selection of the panel's
// data series. Kept deliberately narrow so we don't echo the full
// 100KB+ Grafana response into the chat history.
type GrafanaPanelData struct {
	Dashboard string         `json:"dashboard"`
	PanelID   int            `json:"panel_id"`
	Title     string         `json:"title"`
	Series    []GrafanaSeries `json:"series"`
	Note      string         `json:"note,omitempty"`
}

type GrafanaSeries struct {
	Name   string  `json:"name"`
	Latest float64 `json:"latest"`
	Avg    float64 `json:"avg,omitempty"`
}

func NewGrafanaProxy(s *store.Store, log *slog.Logger) *GrafanaProxy {
	return &GrafanaProxy{
		store: s,
		log:   log.With("component", "darknoc.grafana"),
		conn:  "grafana-prod",
		dash:  "--cEk8A4k",
		hc:    &http.Client{Timeout: 8 * time.Second},
	}
}

// SetConnection / SetDashboard are escape hatches for tests + SIT
// installs that name the row differently or want to point at a
// different default dashboard.
func (g *GrafanaProxy) SetConnection(id string) { g.conn = strings.TrimSpace(id) }
func (g *GrafanaProxy) SetDashboard(uid string) { g.dash = strings.TrimSpace(uid) }

// DashboardUID exposes the configured default for the frontend (the
// /api/v1/darknoc/overview response carries this so the page knows
// which iframe URL to embed).
func (g *GrafanaProxy) DashboardUID() string { return g.dash }

// connection looks up the operator-configured Grafana row. Returns
// ok=false when no row exists — soft state.
func (g *GrafanaProxy) connection() (store.Connection, bool, error) {
	conns, err := g.store.ListConnections()
	if err != nil {
		return store.Connection{}, false, err
	}
	for _, c := range conns {
		if c.ID == g.conn && strings.EqualFold(c.Driver, "grafana") {
			return c, true, nil
		}
	}
	// Fallback to any grafana row — saves the operator from renaming
	// their existing connection just to match our default ID.
	var fallback *store.Connection
	for i, c := range conns {
		if !strings.EqualFold(c.Driver, "grafana") {
			continue
		}
		if strings.Contains(c.ID, "prod") || strings.Contains(c.ID, "primary") {
			return conns[i], true, nil
		}
		if fallback == nil {
			fallback = &conns[i]
		}
	}
	if fallback != nil {
		return *fallback, true, nil
	}
	return store.Connection{}, false, nil
}

// PanelData fetches a single panel's recent data via the Grafana
// dashboard model + datasource proxy. v1 only supports the default
// dashboard; per-dashboard introspection is in TODOS.md.
//
// Implementation note: Grafana's `/api/dashboards/uid/{uid}` returns
// the dashboard JSON model including each panel's `targets` (the
// queries). For v1 we don't run the queries ourselves — we just
// return the panel title + the static `currentValue` Grafana caches
// in the dashboard model. That's enough for the chat tool to say
// "panel X is showing N right now" without us reimplementing
// Prometheus/InfluxDB query execution.
func (g *GrafanaProxy) PanelData(ctx context.Context, panelID int) (GrafanaPanelData, error) {
	out := GrafanaPanelData{
		Dashboard: g.dash,
		PanelID:   panelID,
	}
	conn, ok, err := g.connection()
	if err != nil || !ok {
		out.Note = "no Grafana connection configured (Settings → Connections → driver: grafana)"
		return out, nil
	}
	if conn.Password == "" {
		out.Note = "Grafana token missing on connection"
		return out, nil
	}
	base := strings.TrimRight(conn.Host, "/")
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "https://" + base
	}
	endpoint := fmt.Sprintf("%s/api/dashboards/uid/%s", base, g.dash)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("Authorization", "Bearer "+conn.Password)
	req.Header.Set("Accept", "application/json")

	resp, err := g.hc.Do(req)
	if err != nil {
		out.Note = "grafana http: " + err.Error()
		return out, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if resp.StatusCode != http.StatusOK {
		out.Note = fmt.Sprintf("grafana %d: %s", resp.StatusCode, truncate(string(body), 160))
		return out, nil
	}

	var doc struct {
		Dashboard struct {
			Title  string `json:"title"`
			Panels []struct {
				ID    int    `json:"id"`
				Title string `json:"title"`
			} `json:"panels"`
		} `json:"dashboard"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		out.Note = "grafana parse: " + err.Error()
		return out, nil
	}
	for _, p := range doc.Dashboard.Panels {
		if p.ID == panelID {
			out.Title = p.Title
			break
		}
	}
	if out.Title == "" {
		out.Note = fmt.Sprintf("panel %d not found in %q", panelID, doc.Dashboard.Title)
	}
	return out, nil
}

// TestConnection probes a specific connection row (typically the
// one the operator just typed into the Settings form, not yet saved).
// Hits Grafana's /api/user — succeeds when the token is valid and
// the API is reachable.
func (g *GrafanaProxy) TestConnection(ctx context.Context, c store.Connection) error {
	if c.Host == "" {
		return errors.New("host required (e.g. https://grafana.rain.network)")
	}
	if c.Password == "" {
		return errors.New("token required (paste the service-account token into the password field)")
	}
	base := strings.TrimRight(c.Host, "/")
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "https://" + base
	}
	probeCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, base+"/api/user", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Password)
	resp, err := g.hc.Do(req)
	if err != nil {
		return fmt.Errorf("grafana http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return errors.New("grafana token rejected (401) — rotate at /admin/serviceaccounts")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("grafana health %d", resp.StatusCode)
	}
	return nil
}

// Health probes whether the Grafana token is valid + the API is
// reachable. Returns an error explaining the failure mode; nil on
// success. Safe to call from the frontend banner.
func (g *GrafanaProxy) Health(ctx context.Context) error {
	conn, ok, err := g.connection()
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("no grafana connection configured")
	}
	if conn.Password == "" {
		return errors.New("grafana token empty")
	}
	base := strings.TrimRight(conn.Host, "/")
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "https://" + base
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/user", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+conn.Password)
	resp, err := g.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return errors.New("grafana token rejected")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("grafana health %d", resp.StatusCode)
	}
	return nil
}
