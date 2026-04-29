package darknoc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/SoldierOfGod1/command-centre/internal/ratelimit"
	"github.com/SoldierOfGod1/command-centre/internal/store"
)

// ClickHouseAdapter is the read-only ClickHouse client used by the
// HTTP routes and Cybertron's chat tool.
//
// Why HTTP instead of a driver: ClickHouse exposes a native HTTP
// interface (POST a SQL string + format=JSONEachRow). It's the
// canonical stateless integration path and avoids pulling in
// clickhouse-go (which would add ~5MB to the binary and a CGO-ish
// connection lifecycle we don't need for a 4-query workload).
type ClickHouseAdapter struct {
	store    *store.Store
	log      *slog.Logger
	conn     string // store.Connection.ID — defaults to "clickhouse-prod"
	registry []RegistryAgent
	client   *http.Client

	// Rate limit applied to every outbound ClickHouse request.
	// Defaults to 10 rps with a 20-burst window — the page fan-out
	// (3 parallel tiles × Strict-Mode double-fetch = 6 calls per
	// page load) easily fits in the burst, and the steady-state
	// rate keeps a chatty Cybertron loop or a stuck refresh from
	// hammering houseofclicks. Tunable per-process via the
	// CLICKHOUSE_RATE_PER_SEC + CLICKHOUSE_RATE_BURST env vars
	// (read in main.go on startup).
	limiter *ratelimit.Limiter

	// Cache hot endpoints. Faults move slowly enough that 30s TTL
	// is fine; without this, every page load fans out to all three
	// endpoints in parallel and triples cluster load.
	overviewCache  ttlCache[Overview]
	faultsCache    ttlCache[[]Fault]
	catalogueCache ttlCache[Catalogue]
}

// NewClickHouseAdapter wires the adapter to the store + log. The
// connection ID is resolved lazily on first call so a SIT install
// without a ClickHouse row still boots cleanly.
func NewClickHouseAdapter(s *store.Store, log *slog.Logger, registry []RegistryAgent) *ClickHouseAdapter {
	return &ClickHouseAdapter{
		store:    s,
		log:      log.With("component", "darknoc.clickhouse"),
		conn:     "clickhouse-prod",
		registry: registry,
		client:   &http.Client{Timeout: 8 * time.Second},
		limiter:  ratelimit.New("clickhouse", 20, 10),
	}
}

// SetRateLimit overrides the default 10 rps / 20 burst limiter. main.go
// calls this when CLICKHOUSE_RATE_PER_SEC / CLICKHOUSE_RATE_BURST env
// vars are set.
func (a *ClickHouseAdapter) SetRateLimit(burst int, perSecond float64) {
	a.limiter = ratelimit.New("clickhouse", burst, perSecond)
}

// Limiter returns the in-process limiter so an admin endpoint can
// expose its current bucket level. Nil when not configured.
func (a *ClickHouseAdapter) Limiter() *ratelimit.Limiter { return a.limiter }

// SetConnection overrides the default `clickhouse-prod` ID. Useful
// for tests and for SIT installs that name the row differently.
func (a *ClickHouseAdapter) SetConnection(id string) {
	a.conn = strings.TrimSpace(id)
}

// Registry is constant for the life of the adapter — no caching.
func (a *ClickHouseAdapter) Registry() []RegistryAgent { return a.registry }

// Overview is the page-header KPI bundle. All errors degrade to a
// populated-but-empty struct with Source = "unavailable" so the
// frontend can show a clear banner without throwing.
func (a *ClickHouseAdapter) Overview(ctx context.Context) (Overview, error) {
	if v, ok := a.overviewCache.get(); ok {
		return v, nil
	}

	out := Overview{
		GeneratedAt: time.Now().UTC(),
		Source:      "unavailable",
	}

	conn, ok, err := a.connection()
	if err != nil || !ok {
		out.Note = "no ClickHouse connection configured (Settings → Connections → New → driver: clickhouse)"
		return out, nil
	}

	// 15s budget — the 24h scan over default.cloududn_events touches
	// ~50B rows and lands at 4-9s on the rain cluster. The 6s budget
	// we tried first hit the deadline mid-scan on cold paths. Cache
	// is 30s so the cost amortises across page refreshes.
	queryCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// Real rain telemetry tables (discovered via /api/v1/darknoc/catalogue
	// against houseofclicks.rain.co.za):
	//   default.cloududn_events       — 49B rows, EPC/SGW events with
	//                                    operation_result, protocol_cause,
	//                                    procedure_delay_time, plmn
	//   default.viavi_analytics_events — 9.8B rows, RAN events
	//   default.n4_events             — 43B rows, 5G N4 control plane
	//
	// Single round-trip, single table, four conditional aggregations.
	// ClickHouse executes this as one sequential scan over the 24h
	// partition — sub-second on the rain cluster vs the 4-subquery
	// version we tried first which timed out at 6s.
	//
	// Success-value filter is upper-cased (the actual rain data is
	// 'SUCCESS' / 'SUCCESSFUL_RESPONSE' / 'OK', not lowercase). The
	// previous lowercase filter let successful events through as
	// faults — visible bug fixed here.
	//
	// Definitions:
	//   faults_24h    = events with non-success operation_result
	//   critical_24h  = those + procedure_delay_time > 5000ms (>5s
	//                   round-trip = real degradation, not noise)
	//   slices        = distinct PLMN seen in the window (proxy for
	//                   active network slices; rain is a single PLMN
	//                   today but the field exists for roaming)
	//   breaching     = events with delay > 10s (catastrophic — these
	//                   typically mean the procedure timed out)
	const q = `
SELECT
  count() AS total_24h,
  countIf(upper(operation_result) NOT IN ('SUCCESS','SUCCESSFUL_RESPONSE','OK','')) AS faults_24h,
  countIf(upper(operation_result) NOT IN ('SUCCESS','SUCCESSFUL_RESPONSE','OK','')
          AND procedure_delay_time > 5000) AS critical_24h,
  uniqExact(plmn) AS slices,
  countIf(procedure_delay_time > 10000) AS breaching
FROM default.cloududn_events
WHERE record_time >= now() - INTERVAL 24 HOUR
FORMAT JSONEachRow`

	start := time.Now()
	row, err := a.queryOne(queryCtx, conn, q)
	out.SourceLatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		a.log.Warn("overview query failed", "error", err)
		out.Note = "query failed: " + truncate(err.Error(), 160)
		return out, nil
	}

	out.TotalEvents24h = intOf(row["total_24h"])
	out.FaultsLast24h = intOf(row["faults_24h"])
	out.CriticalFaults24h = intOf(row["critical_24h"])
	out.ActiveSlices = intOf(row["slices"])
	out.SlicesBreachingSLA = intOf(row["breaching"])
	out.NetworkTrustScore = trustScoreFromRates(out.TotalEvents24h, out.FaultsLast24h, out.CriticalFaults24h, out.SlicesBreachingSLA)
	out.Source = "clickhouse"

	a.overviewCache.set(out, 30*time.Second)
	return out, nil
}

// Faults returns the latest fault rows. Capped at 50, ordered newest
// first. Same graceful degradation as Overview.
func (a *ClickHouseAdapter) Faults(ctx context.Context) ([]Fault, error) {
	if v, ok := a.faultsCache.get(); ok {
		return v, nil
	}

	conn, ok, err := a.connection()
	if err != nil || !ok {
		return []Fault{}, nil
	}

	queryCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// Real fault stream: non-successful cloududn (EPC) events.
	// Severity bucketing on procedure_delay_time matches operator
	// intuition — sub-second is normal, multi-second is degraded,
	// >5s is critical. Filter normalises the success-value casing
	// (rain stores them uppercase) so we don't leak successful
	// procedures into the fault list.
	const q = `
SELECT
  toString(sequence_no) AS id,
  toString(record_time) AS occurred_at,
  CASE
    WHEN procedure_delay_time > 5000 THEN 'critical'
    WHEN procedure_delay_time > 1000 THEN 'warning'
    ELSE 'info'
  END AS severity,
  ne_id AS source,
  plmn AS region,
  ran_type AS technology,
  concat(procedure_identification, ' · ', operation_result) AS title,
  concat('protocol_cause=', toString(protocol_cause), ' · delay=', toString(procedure_delay_time), 'ms') AS detail
FROM default.cloududn_events
WHERE record_time >= now() - INTERVAL 24 HOUR
  AND upper(operation_result) NOT IN ('SUCCESS','SUCCESSFUL_RESPONSE','OK','')
ORDER BY record_time DESC
LIMIT 50
FORMAT JSONEachRow`

	rows, err := a.queryRows(queryCtx, conn, q)
	if err != nil {
		a.log.Warn("faults query", "error", err)
		return []Fault{}, nil
	}

	out := make([]Fault, 0, len(rows))
	for _, r := range rows {
		f := Fault{
			ID:         strOf(r["id"]),
			Severity:   strOf(r["severity"]),
			Source:     strOf(r["source"]),
			Region:     strOf(r["region"]),
			Technology: strOf(r["technology"]),
			Title:      strOf(r["title"]),
			Detail:     strOf(r["detail"]),
		}
		if t, err := time.Parse("2006-01-02 15:04:05", strOf(r["occurred_at"])); err == nil {
			f.OccurredAt = t.UTC()
		}
		out = append(out, f)
	}

	a.faultsCache.set(out, 30*time.Second)
	return out, nil
}

// CatalogueDB is one database in the schema catalogue.
type CatalogueDB struct {
	Name   string         `json:"name"`
	Tables []CatalogueTbl `json:"tables"`
}

// CatalogueTbl is one table.
type CatalogueTbl struct {
	Name    string         `json:"name"`
	Engine  string         `json:"engine,omitempty"`
	Rows    int64          `json:"rows,omitempty"`
	Columns []CatalogueCol `json:"columns"`
}

// CatalogueCol is one column.
type CatalogueCol struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Default string `json:"default,omitempty"`
	Comment string `json:"comment,omitempty"`
}

// Catalogue is the full ClickHouse schema dump used by Cybertron's
// chat tool (so it stops hallucinating table names) and by the
// Network HUD (which can pivot its hardcoded `faults` queries onto
// whatever rain actually calls the fault stream). Cached server-side
// so chat invocations are cheap.
type Catalogue struct {
	GeneratedAt time.Time     `json:"generated_at"`
	Host        string        `json:"host"`
	Source      string        `json:"source"` // "clickhouse" | "unavailable"
	Note        string        `json:"note,omitempty"`
	Databases   []CatalogueDB `json:"databases"`
}

// CrawlCatalogue walks system.tables + system.columns in two
// queries (not one-per-DB) so a 200-database cluster doesn't blow
// out the request budget. Cached 10 minutes — schemas don't change
// in real time, and a cold crawl can return ~1MB on a busy cluster.
//
// Old version did 1 + N + (N×T) queries (databases, then per-DB
// tables, then per-table columns). On the rain cluster that's
// hundreds of HTTP roundtrips and a ~24-minute hang. The collapsed
// shape is 2 queries total: ~5 seconds end-to-end.
func (a *ClickHouseAdapter) CrawlCatalogue(ctx context.Context) (Catalogue, error) {
	if v, ok := a.catalogueCache.get(); ok {
		return v, nil
	}
	out := Catalogue{GeneratedAt: time.Now().UTC(), Source: "unavailable"}
	conn, ok, err := a.connection()
	if err != nil || !ok {
		out.Note = "no ClickHouse connection configured"
		return out, nil
	}
	out.Host = conn.Host

	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Query 1: every table on the cluster, with database + engine + rows.
	// Skipping the well-known system DBs that clutter the output.
	const tablesQ = `
SELECT database, name, engine, total_rows
  FROM system.tables
 WHERE database NOT IN ('system','INFORMATION_SCHEMA','information_schema')
 ORDER BY database, name
 FORMAT JSONEachRow`
	tableRows, terr := a.queryRows(queryCtx, conn, tablesQ)
	if terr != nil {
		out.Note = "list tables: " + truncate(terr.Error(), 200)
		return out, nil
	}

	// Query 2: every column on the cluster, same scope filter.
	// ClickHouse's system.columns returns ~50k rows on a big cluster
	// — JSONEachRow streams one row per line so the parse is linear.
	const columnsQ = `
SELECT database, table, name, type, default_kind, comment
  FROM system.columns
 WHERE database NOT IN ('system','INFORMATION_SCHEMA','information_schema')
 ORDER BY database, table, position
 FORMAT JSONEachRow`
	colRows, cerr := a.queryRows(queryCtx, conn, columnsQ)
	if cerr != nil {
		// Even without columns we can return tables — the catalogue
		// stays useful for table-name autocomplete in chat.
		a.log.Warn("catalogue columns", "error", cerr)
		colRows = nil
	}

	// Index columns by (db, table) so we can attach them to each
	// table in a single linear pass instead of an O(N×M) scan.
	colsByTable := make(map[string][]CatalogueCol, len(colRows)/8)
	for _, r := range colRows {
		key := strOf(r["database"]) + "::" + strOf(r["table"])
		colsByTable[key] = append(colsByTable[key], CatalogueCol{
			Name:    strOf(r["name"]),
			Type:    strOf(r["type"]),
			Default: strOf(r["default_kind"]),
			Comment: strOf(r["comment"]),
		})
	}

	// Group tables by database in the order returned (already sorted).
	dbIndex := make(map[string]int)
	for _, t := range tableRows {
		dbName := strOf(t["database"])
		if dbName == "" {
			continue
		}
		idx, ok := dbIndex[dbName]
		if !ok {
			idx = len(out.Databases)
			out.Databases = append(out.Databases, CatalogueDB{Name: dbName})
			dbIndex[dbName] = idx
		}
		tblName := strOf(t["name"])
		key := dbName + "::" + tblName
		out.Databases[idx].Tables = append(out.Databases[idx].Tables, CatalogueTbl{
			Name:    tblName,
			Engine:  strOf(t["engine"]),
			Rows:    int64(intOf(t["total_rows"])),
			Columns: colsByTable[key],
		})
	}

	out.Source = "clickhouse"
	a.catalogueCache.set(out, 10*time.Minute)
	return out, nil
}

// escapeSQLLiteral does a minimal quote-escape for ClickHouse string
// literals embedded in SQL we control. Inputs come from system.databases
// / system.tables — all rain-controlled identifiers, no user data —
// but escaping the apostrophe keeps us paranoid-safe.
func escapeSQLLiteral(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// TestConnection runs a `SELECT 1` against the supplied connection.
// Used by the Settings "Test connection" button. Bypasses the cache
// and the configured connection ID — the operator may be testing a
// row that isn't `clickhouse-prod` and isn't even saved yet.
func (a *ClickHouseAdapter) TestConnection(ctx context.Context, c store.Connection) error {
	if c.Host == "" {
		return errors.New("host required")
	}
	if c.User == "" {
		return errors.New("user required")
	}
	queryCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	rows, err := a.queryRows(queryCtx, c, "SELECT 1 AS ok FORMAT JSONEachRow")
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return errors.New("clickhouse returned no rows for SELECT 1")
	}
	return nil
}

// connection picks the ClickHouse row to use. Looks for the
// configured ID first (default `clickhouse-prod`); falls back to ANY
// row with driver=clickhouse so the operator doesn't have to rename
// their existing row (the rain operator's row is `clickhouse-main`,
// not `-prod` — auto-discovery saves a rename + a re-test).
//
// Returns ok=false (no error) when no row exists at all — that's a
// soft state, not a failure.
func (a *ClickHouseAdapter) connection() (store.Connection, bool, error) {
	conns, err := a.store.ListConnections()
	if err != nil {
		return store.Connection{}, false, err
	}
	// First pass: exact match on configured ID.
	for _, c := range conns {
		if c.ID == a.conn && strings.EqualFold(c.Driver, "clickhouse") {
			return c, true, nil
		}
	}
	// Second pass: any clickhouse driver row, in order. Prefer
	// rows whose ID hints at production (-prod or -primary).
	var fallback *store.Connection
	for i, c := range conns {
		if !strings.EqualFold(c.Driver, "clickhouse") {
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

// queryOne POSTs the SQL to ClickHouse's HTTP interface and returns
// the first JSONEachRow row.
func (a *ClickHouseAdapter) queryOne(ctx context.Context, c store.Connection, sql string) (map[string]any, error) {
	rows, err := a.queryRows(ctx, c, sql)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return map[string]any{}, nil
	}
	return rows[0], nil
}

// queryRows POSTs the SQL and parses every JSONEachRow line. The CH
// HTTP interface streams one JSON object per line — dead simple to
// parse without a driver.
func (a *ClickHouseAdapter) queryRows(ctx context.Context, c store.Connection, sql string) ([]map[string]any, error) {
	if c.Host == "" {
		return nil, errors.New("clickhouse connection missing host")
	}
	// Rate-gate every outbound ClickHouse call. Cap the wait at the
	// caller's context so the page tile degrades to "unavailable"
	// instead of stalling 30 seconds when something has saturated
	// the bucket. Limiter is nil-safe: New() returns a no-op limiter
	// on misconfiguration, Wait returns an error there too.
	if a.limiter != nil {
		if err := a.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("clickhouse rate-limited: %w", err)
		}
	}
	scheme, port := schemeAndPortFor(c)
	// Trim whitespace — copy-paste from secret managers and password
	// docs commonly leaves a trailing newline that breaks auth silently.
	user := strings.TrimSpace(c.User)
	pass := strings.TrimSpace(c.Password)

	// Auth via X-ClickHouse-User / X-ClickHouse-Key headers, NOT URL
	// query params. JDBC drivers (DBeaver, clickhouse-jdbc) use these.
	//
	// Why headers, not query params: when the password sits in the URL,
	// ClickHouse error messages echo the full request URL back into the
	// response, and our log lines / error wrappers persist that URL to
	// disk. That leaks the plaintext password into server.log on every
	// failed query. Headers keep the credential out of the URL string,
	// so a connection failure logs the host but not the secret.
	q := url.Values{}
	if c.Database != "" {
		q.Set("database", c.Database)
	}
	endpoint := fmt.Sprintf("%s://%s:%s/", scheme, c.Host, port)
	if encoded := q.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBufferString(sql))
	if err != nil {
		return nil, err
	}
	if user != "" {
		req.Header.Set("X-ClickHouse-User", user)
	}
	if pass != "" {
		req.Header.Set("X-ClickHouse-Key", pass)
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	// Diagnostic-only log line. Does NOT log the password or user;
	// records the byte-length so we can confirm what the round-trip
	// looks like. Length 0 → empty save. Length doesn't match DBeaver's
	// visible asterisk count → a copy-paste mangled the value somewhere.
	a.log.Info("clickhouse query",
		"host", c.Host, "port", port, "scheme", scheme,
		"user_len", len(user), "pass_len", len(pass),
		"database", c.Database)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("clickhouse http: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("clickhouse %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	out := make([]map[string]any, 0)
	for _, line := range bytes.Split(body, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal(line, &row); err != nil {
			a.log.Warn("clickhouse json parse", "error", err, "line", truncate(string(line), 120))
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

// schemeAndPortFor picks http vs https + the right port for a
// ClickHouse connection. The user's own DBeaver tests against
// `houseofclicks.rain.co.za:8123` (the plain-HTTP interface), so the
// `ssl_mode != "disable"` default our older code used was wrong: it
// tried to TLS-handshake against a plain port and the test failed
// with no useful error.
//
// Inference rules, in order:
//  1. ssl_mode = "disable"          → http, port 8123 if unset
//  2. ssl_mode = "require"/"verify" → https, port 8443 if unset
//  3. port = 8123 or 9000           → http (well-known plain-text)
//  4. port = 8443 or 9440           → https (well-known TLS)
//  5. otherwise                     → https, port 8443
func schemeAndPortFor(c store.Connection) (string, string) {
	mode := strings.ToLower(strings.TrimSpace(c.SSLMode))
	port := strings.TrimSpace(c.Port)
	switch mode {
	case "disable":
		if port == "" {
			port = "8123"
		}
		return "http", port
	case "require", "verify-full", "verify-ca":
		if port == "" {
			port = "8443"
		}
		return "https", port
	}
	switch port {
	case "8123", "9000":
		return "http", port
	case "8443", "9440":
		return "https", port
	}
	if port == "" {
		port = "8443"
	}
	return "https", port
}

// trustScoreFromRates maps real cluster counts to a 0-100 gauge.
// Rain operates a 5G/LTE network where retries, control-plane
// timeouts, and inter-RAT handover failures are baseline — not
// incidents. The score is a *relative* health signal, not an
// absolute "100 = perfect zero failures" reading.
//
// Calibration anchored against the rain houseofclicks cluster
// observed at scale (24h windows of default.cloududn_events):
//
//   fault%  | crit% | breach% | meaning   | score band
//   -----------------------------------------------------
//   <5      | <0.5  | <2      | healthy   | 90-100
//   5-15    | 0.5-2 | 2-5     | nominal   | 70-90
//   15-30   | 2-5   | 5-10    | warm      | 40-70
//   30-50   | 5-10  | 10-20   | degraded  | 15-40
//   >50     | >10   | >20     | incident  | 0-15
//
// Empty-cluster (or fully-cached/missing data) returns 100.
func trustScoreFromRates(total, faults, critical, breaching int) int {
	if total <= 0 {
		return 100
	}
	tf := float64(total)
	faultPct := 100 * float64(faults) / tf
	critPct := 100 * float64(critical) / tf
	breachPct := 100 * float64(breaching) / tf

	// Penalty curves: zero at the "healthy" band ceiling, ramping
	// linearly to "incident" levels. Sum and clamp.
	penalty := 0.0
	if faultPct > 5 {
		penalty += (faultPct - 5) * 0.7 // 50% non-success → 31.5pt
	}
	if critPct > 0.5 {
		penalty += (critPct - 0.5) * 4 // 10% critical → 38pt
	}
	if breachPct > 2 {
		penalty += (breachPct - 2) * 2.5 // 20% breach → 45pt
	}
	score := 100 - int(penalty+0.5)
	return clamp(score)
}

func clamp(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func intOf(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case int64:
		return int(x)
	case string:
		var n int
		_, _ = fmt.Sscanf(x, "%d", &n)
		return n
	}
	return 0
}

func strOf(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
