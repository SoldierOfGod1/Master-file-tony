package store

import (
	"encoding/json"
	"fmt"
)

// Connection is a single database connection the user has registered
// through the Settings UI. The id is a stable slug — used in URLs and
// as the key in the pool manager's map.
type Connection struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Driver    string `json:"driver"`   // "postgres" | "clickhouse"
	Host      string `json:"host"`
	Port      string `json:"port"`
	Database  string `json:"database"`
	User      string `json:"user"`
	Password  string `json:"password"`
	SSLMode   string `json:"ssl_mode"`
	IsPrimary bool   `json:"is_primary"`

	// Optional read-only replica. When populated, read-heavy queries
	// (customer lookup fan-outs, sales poller CTEs) route here
	// instead of the primary — protecting replication lag on the
	// primary. User same credentials + same database; only host/port
	// differ. Empty means "use primary for reads too" (today's
	// behaviour, for backward compat).
	ReadReplicaHost string `json:"read_replica_host,omitempty"`
	ReadReplicaPort string `json:"read_replica_port,omitempty"`
}

// Filled returns true if the connection has enough fields to open a pool.
// Per-driver requirements differ — Postgres needs an explicit database
// name, ClickHouse defaults to `default` when one isn't given (matches
// DBeaver), Grafana is a single-tenant HTTP API where only the URL +
// token matter. Treating the postgres requirement as universal made
// every ClickHouse and Grafana row light up amber even when fully
// configured.
func (c Connection) Filled() bool {
	switch c.Driver {
	case "clickhouse":
		// host + user + password are enough; port has a sane default
		// for the HTTP interface (8123/8443 by ssl_mode), database is
		// optional ("" → ClickHouse routes to `default`).
		return c.Host != "" && c.User != "" && c.Password != ""
	case "grafana":
		// Grafana service-account token sits in Password. Host is
		// the base URL. Everything else is unused.
		return c.Host != "" && c.Password != ""
	default:
		// Postgres + future drivers — keep the strict check.
		return c.Host != "" && c.Port != "" && c.User != "" && c.Password != "" && c.Database != ""
	}
}

// MaskedPassword returns the password masked for display. Kept here so all
// callers (settings page, list response, test endpoint) render it the same way.
func (c Connection) MaskedPassword() string {
	if c.Password == "" {
		return ""
	}
	if len(c.Password) > 4 {
		return "••••" + c.Password[len(c.Password)-4:]
	}
	return "••••"
}

// SettingDBConnections is the single app_settings key that stores all
// registered connections as one JSON array.
const SettingDBConnections = "db.connections"

// ListConnections reads every registered connection from app_settings.
func (s *Store) ListConnections() ([]Connection, error) {
	raw, err := s.GetSetting(SettingDBConnections)
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return []Connection{}, nil
	}
	var out []Connection
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("parse connections json: %w", err)
	}
	return out, nil
}

// SaveConnections replaces the whole connection list. Use List → mutate →
// Save. Enforces that at most one entry has IsPrimary=true.
func (s *Store) SaveConnections(conns []Connection) error {
	// Normalise: exactly one primary. If more than one claim primary we
	// keep the first; if none do, promote the first filled entry.
	primarySeen := false
	for i := range conns {
		if conns[i].IsPrimary {
			if primarySeen {
				conns[i].IsPrimary = false
			}
			primarySeen = true
		}
	}
	if !primarySeen {
		for i := range conns {
			if conns[i].Filled() {
				conns[i].IsPrimary = true
				break
			}
		}
	}
	buf, err := json.Marshal(conns)
	if err != nil {
		return err
	}
	return s.SetSetting(SettingDBConnections, string(buf))
}

// UpsertConnection adds or replaces a single connection by id.
func (s *Store) UpsertConnection(c Connection) error {
	conns, err := s.ListConnections()
	if err != nil {
		return err
	}
	found := false
	for i, existing := range conns {
		if existing.ID == c.ID {
			// Treat masked OR empty incoming password as "no change"
			// so re-saves don't wipe it. The frontend sends a patch
			// without the password key when the user leaves the
			// password field blank — JSON deserializes that into the
			// zero value (""). Without this guard, every Save from
			// the form would silently overwrite the stored password
			// with empty string. Symptom: ClickHouse / Postgres
			// returns "password is incorrect" because the saved
			// password got nuked by the previous Save click.
			if c.Password == "" || isPasswordMasked(c.Password) {
				c.Password = existing.Password
			}
			conns[i] = c
			found = true
			break
		}
	}
	if !found {
		conns = append(conns, c)
	}
	return s.SaveConnections(conns)
}

// DeleteConnection removes a connection by id.
func (s *Store) DeleteConnection(id string) error {
	conns, err := s.ListConnections()
	if err != nil {
		return err
	}
	out := make([]Connection, 0, len(conns))
	for _, c := range conns {
		if c.ID == id {
			continue
		}
		out = append(out, c)
	}
	return s.SaveConnections(out)
}

// GetConnection returns one connection by id, or an empty Connection and
// ok=false if it doesn't exist.
func (s *Store) GetConnection(id string) (Connection, bool, error) {
	conns, err := s.ListConnections()
	if err != nil {
		return Connection{}, false, err
	}
	for _, c := range conns {
		if c.ID == id {
			return c, true, nil
		}
	}
	return Connection{}, false, nil
}

// PrimaryConnection returns the entry flagged IsPrimary — the default one
// the Customer 360 tab uses when the user hasn't picked explicitly.
func (s *Store) PrimaryConnection() (Connection, bool, error) {
	conns, err := s.ListConnections()
	if err != nil {
		return Connection{}, false, err
	}
	for _, c := range conns {
		if c.IsPrimary {
			return c, true, nil
		}
	}
	// Fallback: first filled one.
	for _, c := range conns {
		if c.Filled() {
			return c, true, nil
		}
	}
	return Connection{}, false, nil
}

// SeedDefaultConnections writes the user's 4 Postgres + 1 ClickHouse
// connections on first boot. Called once from main.go after migrations.
// Honours an existing list: if any connection is already registered we
// don't overwrite — the user may have typed passwords.
//
// Separately, this also migrates the legacy axiom.* settings into a single
// connection entry so passwords the user already saved on the older UI
// flow still work after the refactor.
func (s *Store) SeedDefaultConnections() error {
	conns, err := s.ListConnections()
	if err != nil {
		return err
	}

	// One-shot migration from the previous single-Axiom model.
	if legacy, ok := buildLegacyAxiomConnection(s); ok {
		// Only insert if the user hasn't already set this id up.
		if !containsID(conns, legacy.ID) {
			conns = append(conns, legacy)
		}
	}

	for _, defaults := range defaultConnections() {
		if containsID(conns, defaults.ID) {
			continue
		}
		conns = append(conns, defaults)
	}
	return s.SaveConnections(conns)
}

// defaultConnections returns the four Postgres connections (plus one
// ClickHouse placeholder) the user pasted in. Passwords are blank — the
// user fills them in via the Settings UI.
func defaultConnections() []Connection {
	return []Connection{
		{
			ID:        "axiom-sit-bss",
			Label:     "Axiom BSS · SIT",
			Driver:    "postgres",
			Host:      "bss-psql-sit-01.rain.network",
			Port:      "5432",
			Database:  "postgresdb",
			User:      "baptista",
			SSLMode:   "disable",
			IsPrimary: true,
		},
		{
			ID:       "axiom-prod",
			Label:    "Axiom · PROD",
			Driver:   "postgres",
			Host:     "axiom-prod-pg-cluster.rain.co.za",
			Port:     "5433",
			Database: "postgresdb",
			User:     "baptista",
			SSLMode:  "require",
		},
		{
			ID:       "axiom-sit-cluster",
			Label:    "Axiom SIT · Cluster",
			Driver:   "postgres",
			Host:     "sit-pg-cluster.rain.co.za",
			Port:     "5433",
			Database: "postgresdb",
			User:     "baptista",
			SSLMode:  "disable",
		},
		{
			ID:       "pg-10-193",
			Label:    "Postgres · 10.193.10.32",
			Driver:   "postgres",
			Host:     "10.193.10.32",
			Port:     "5432",
			Database: "postgresdb",
			User:     "baptista",
			SSLMode:  "disable",
		},
		{
			ID:       "clickhouse-main",
			Label:    "ClickHouse · HouseOfClicks",
			Driver:   "clickhouse",
			Host:     "houseofclicks.rain.co.za",
			Port:     "8123", // ClickHouse HTTP interface
			Database: "default",
			User:     "service.neo",
			SSLMode:  "disable",
		},
		{
			// Huawei GaussDB DWS — wire-compatible with Postgres so
			// the existing dbhealth probe works against it as-is.
			// Mirrors Axiom's posture: P1 priority via the
			// gaussdb-prefixed ID below, monitored on the same
			// 90s/180s cadence with the same alert rules.
			ID:       "gaussdb-prod",
			Label:    "GaussDB DWS · PROD",
			Driver:   "postgres",
			Host:     "10.20.48.183",
			Port:     "8000",
			Database: "gaussdb",
			User:     "antonio",
			SSLMode:  "disable",
		},
	}
}

// buildLegacyAxiomConnection re-packages the pre-refactor axiom.* settings
// into a single Connection so the user doesn't lose a password they already
// saved. Returns ok=false if no legacy host was configured.
func buildLegacyAxiomConnection(s *Store) (Connection, bool) {
	host, _ := s.GetSetting(SettingAxiomHost)
	if host == "" {
		return Connection{}, false
	}
	port, _ := s.GetSetting(SettingAxiomPort)
	if port == "" {
		port = "5432"
	}
	db, _ := s.GetSetting(SettingAxiomDatabase)
	user, _ := s.GetSetting(SettingAxiomUser)
	password, _ := s.GetSetting(SettingAxiomPassword)
	ssl, _ := s.GetSetting(SettingAxiomSSLMode)
	if ssl == "" {
		ssl = "disable"
	}
	return Connection{
		ID:        "axiom-sit-bss",
		Label:     "Axiom BSS · SIT",
		Driver:    "postgres",
		Host:      host,
		Port:      port,
		Database:  db,
		User:      user,
		Password:  password,
		SSLMode:   ssl,
		IsPrimary: true,
	}, true
}

func containsID(conns []Connection, id string) bool {
	for _, c := range conns {
		if c.ID == id {
			return true
		}
	}
	return false
}

// isPasswordMasked detects the "••••XXXX" placeholder so we don't overwrite
// real stored passwords when the frontend re-submits the form without the
// user retyping them.
func isPasswordMasked(v string) bool {
	return len(v) >= 2 && (v[0:3] == "\u2022\u2022\u2022" || (len(v) >= 6 && v[0:6] == "••••"))
}
