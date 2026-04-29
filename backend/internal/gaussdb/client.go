package gaussdb

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SoldierOfGod1/command-centre/internal/axiomapi"
	"github.com/SoldierOfGod1/command-centre/internal/store"
)

// ErrNotConfigured signals the caller should surface 503 + "register a
// gaussdb-prod connection in Settings → Connections".
var ErrNotConfigured = errors.New("gaussdb: no usable connection registered")

// ErrPlaceholderSQL is the sentinel route handlers check at request
// time. Returned when PlaceholderSQL == true. The 503 message tells
// the operator exactly what to do — paste the SQL into queries.go and
// flip the constant.
var ErrPlaceholderSQL = errors.New("gaussdb: placeholder SQL still in queries.go — paste operator queries and flip PlaceholderSQL=false to enable")

// Client wraps the GaussDB pgxpool, the per-msisdn usage cache, and
// the schema-catalogue cache. One Client per process is enough; pools
// are lazy and connection auto-discovery handles the case where the
// connection ID isn't `gaussdb-prod`.
type Client struct {
	store *store.Store
	log   *slog.Logger
	conn  string

	mu   sync.Mutex
	pool *pgxpool.Pool
	// id of the connection the cached pool was built against, so we
	// can drop and rebuild if the operator renames or re-credentialed
	// the connection between calls.
	cachedConnID string

	// Per-msisdn usage cache. CDR fact tables on a DWS cluster are
	// large — without a cache, every Customer 360 page load runs a
	// full per-customer scan. 60s TTL matches the operator-frame
	// "refresh feels live but doesn't hammer the cluster" target.
	usageMu    sync.Mutex
	usageCache map[string]usageCacheEntry

	// Schema catalogue cache. Schemas don't change in real time and a
	// cluster crawl can be expensive — 10 minutes is plenty.
	catalogueMu      sync.Mutex
	catalogue        Catalogue
	catalogueExpires time.Time
}

type usageCacheEntry struct {
	value   axiomapi.UsageSummary
	expires time.Time
}

// NewClient wires the client to the app's Store + log. Doesn't open
// a pool — that happens lazily on first call. Default connection ID
// is `gaussdb-prod` (the seeded row in store.SeedDefaultConnections);
// override via SetConnection for tests or alternate environments.
func NewClient(s *store.Store, log *slog.Logger) *Client {
	return &Client{
		store:      s,
		log:        log.With("component", "gaussdb"),
		conn:       "gaussdb-prod",
		usageCache: map[string]usageCacheEntry{},
	}
}

// SetConnection overrides the default `gaussdb-prod` ID.
func (c *Client) SetConnection(id string) {
	if c == nil {
		return
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conn = id
	// Drop cached pool — it was built against the old ID's credentials.
	if c.pool != nil {
		c.pool.Close()
		c.pool = nil
		c.cachedConnID = ""
	}
}

// Available reports whether the client can serve gaussdb-backed
// answers right now. False when PlaceholderSQL is still set OR no
// gaussdb-driver connection is registered. Routes call this before
// touching the pool so we get a clean 503 with a precise reason.
func (c *Client) Available() (bool, error) {
	if c == nil {
		return false, ErrNotConfigured
	}
	if PlaceholderSQL {
		return false, ErrPlaceholderSQL
	}
	_, ok, err := c.connection()
	if err != nil {
		return false, err
	}
	if !ok {
		return false, ErrNotConfigured
	}
	return true, nil
}

// Catalogue is the GaussDB schema dump — parallel to darknoc.Catalogue
// but flatter (Postgres has 2 levels: schema → table; ClickHouse adds
// database on top). Scanned by Cybertron so it composes valid SQL
// against gaussdb instead of guessing.
type Catalogue struct {
	GeneratedAt time.Time         `json:"generated_at"`
	Host        string            `json:"host"`
	Source      string            `json:"source"` // "gaussdb" | "unavailable"
	Note        string            `json:"note,omitempty"`
	Schemas     []CatalogueSchema `json:"schemas"`
}

// CatalogueSchema is one Postgres schema (e.g. `axiom_usage_online`).
type CatalogueSchema struct {
	Name   string         `json:"name"`
	Tables []CatalogueTbl `json:"tables"`
}

// CatalogueTbl is one table.
type CatalogueTbl struct {
	Name    string         `json:"name"`
	Rows    int64          `json:"rows,omitempty"`
	Columns []CatalogueCol `json:"columns"`
}

// CatalogueCol is one column.
type CatalogueCol struct {
	Name     string `json:"name"`
	DataType string `json:"data_type"`
	NotNull  bool   `json:"not_null,omitempty"`
}

// UsageSummary returns the same axiomapi.UsageSummary shape the
// frontend already binds to so the tile is source-agnostic. Sets
// the Source field to "gaussdb" so the UI chip reflects provenance.
//
// Routes must call Available() first — this method does NOT short-
// circuit on PlaceholderSQL because the route returns 503 before
// here. If callers skip Available(), they get a runtime SQL error
// from the placeholder query, which is the safe failure mode.
func (c *Client) UsageSummary(ctx context.Context, msisdn string) (axiomapi.UsageSummary, error) {
	out := axiomapi.UsageSummary{
		MSISDN: msisdn,
		Series: []axiomapi.DayUsage{},
		Source: "gaussdb",
	}
	msisdn = strings.TrimSpace(msisdn)
	if msisdn == "" {
		return out, errors.New("msisdn required")
	}
	if PlaceholderSQL {
		return out, ErrPlaceholderSQL
	}

	if v, ok := c.usageCacheGet(msisdn); ok {
		return v, nil
	}

	pool, err := c.poolFor(ctx)
	if err != nil {
		return out, err
	}

	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Rollup + series in two round-trips. Could collapse to one once
	// we see the operator's working SQL — for now keeping them split
	// matches the four-query promise in the plan and lets the operator
	// paste each one independently.
	var (
		totalBytes     int64
		avgDailyBytes  int64
		activeDays     int
		peakDailyBytes int64
		peakDay        *time.Time
		firstDay       *time.Time
		lastDay        *time.Time
		windowDays     int
	)
	if err := pool.QueryRow(queryCtx, QueryUsageSummary, msisdn).Scan(
		&totalBytes, &avgDailyBytes, &activeDays,
		&peakDailyBytes, &peakDay, &firstDay, &lastDay, &windowDays,
	); err != nil {
		return out, fmt.Errorf("gaussdb usage rollup: %w", err)
	}
	out.TotalBytes = totalBytes
	out.AvgDailyBytes = avgDailyBytes
	out.ActiveDays = activeDays
	out.PeakDailyBytes = peakDailyBytes
	out.WindowDays = windowDays
	if peakDay != nil {
		out.PeakDay = peakDay.Format("2006-01-02")
	}
	if firstDay != nil {
		out.FirstDay = firstDay.Format("2006-01-02")
	}
	if lastDay != nil {
		out.LastDay = lastDay.Format("2006-01-02")
	}

	rows, err := pool.Query(queryCtx, QueryUsageSeries, msisdn)
	if err != nil {
		// Series failure isn't fatal — the rollup is enough for the
		// 4-tile strip. Log and return what we have.
		c.log.Warn("gaussdb usage series failed", "error", err, "msisdn_len", len(msisdn))
	} else {
		defer rows.Close()
		for rows.Next() {
			var day time.Time
			var bytes int64
			if err := rows.Scan(&day, &bytes); err != nil {
				c.log.Warn("gaussdb series scan", "error", err)
				continue
			}
			out.Series = append(out.Series, axiomapi.DayUsage{
				Date:  day.Format("2006-01-02"),
				Bytes: bytes,
			})
		}
		if err := rows.Err(); err != nil {
			c.log.Warn("gaussdb series rows", "error", err)
		}
	}

	c.usageCacheSet(msisdn, out)
	return out, nil
}

// CrawlCatalogue lists every schema/table/column on the cluster.
// Cached 10 minutes. Probes pg_class first; if 0 rows come back, the
// caller knows DWS may have renamed the catalog views — we surface
// that as a Note instead of crashing.
func (c *Client) CrawlCatalogue(ctx context.Context) (Catalogue, error) {
	out := Catalogue{GeneratedAt: time.Now().UTC(), Source: "unavailable"}
	if c == nil {
		out.Note = "gaussdb client not initialised"
		return out, nil
	}

	c.catalogueMu.Lock()
	if c.catalogue.Source == "gaussdb" && time.Now().Before(c.catalogueExpires) {
		cached := c.catalogue
		c.catalogueMu.Unlock()
		return cached, nil
	}
	c.catalogueMu.Unlock()

	conn, ok, err := c.connection()
	if err != nil || !ok {
		out.Note = "no GaussDB connection registered (Settings → Connections → driver: postgres, id: gaussdb-prod)"
		return out, nil
	}
	out.Host = conn.Host

	pool, err := c.poolFor(ctx)
	if err != nil {
		out.Note = "open pool: " + err.Error()
		return out, nil
	}

	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	tableRows, err := pool.Query(queryCtx, QueryCatalogueTables)
	if err != nil {
		out.Note = "list tables: " + truncate(err.Error(), 200)
		return out, nil
	}
	type tblKey struct{ schema, table string }
	tableIndex := make(map[tblKey]*CatalogueTbl)
	schemaIndex := make(map[string]int)
	for tableRows.Next() {
		var schema, table string
		var rowcount int64
		if err := tableRows.Scan(&schema, &table, &rowcount); err != nil {
			continue
		}
		idx, ok := schemaIndex[schema]
		if !ok {
			idx = len(out.Schemas)
			out.Schemas = append(out.Schemas, CatalogueSchema{Name: schema})
			schemaIndex[schema] = idx
		}
		out.Schemas[idx].Tables = append(out.Schemas[idx].Tables, CatalogueTbl{
			Name: table,
			Rows: rowcount,
		})
		t := &out.Schemas[idx].Tables[len(out.Schemas[idx].Tables)-1]
		tableIndex[tblKey{schema, table}] = t
	}
	tableRows.Close()
	if err := tableRows.Err(); err != nil {
		out.Note = "scan tables: " + truncate(err.Error(), 200)
		return out, nil
	}

	if len(out.Schemas) == 0 {
		// Empty result against pg_class — could be a DWS gs_* rename
		// or could just be a database with zero user tables. Surface
		// it as a Note so the operator can decide; don't crash.
		out.Note = "pg_class returned 0 user tables — verify DWS catalog naming on this cluster"
		return out, nil
	}

	colRows, err := pool.Query(queryCtx, QueryCatalogueColumns)
	if err != nil {
		// Columns missing isn't fatal — the table list is still
		// useful for autocomplete. Log and return what we have.
		c.log.Warn("gaussdb catalogue columns", "error", err)
	} else {
		for colRows.Next() {
			var schema, table, colName, dataType string
			var notNull bool
			if err := colRows.Scan(&schema, &table, &colName, &dataType, &notNull); err != nil {
				continue
			}
			if t, ok := tableIndex[tblKey{schema, table}]; ok {
				t.Columns = append(t.Columns, CatalogueCol{
					Name: colName, DataType: dataType, NotNull: notNull,
				})
			}
		}
		colRows.Close()
	}

	out.Source = "gaussdb"
	c.catalogueMu.Lock()
	c.catalogue = out
	c.catalogueExpires = time.Now().Add(10 * time.Minute)
	c.catalogueMu.Unlock()
	return out, nil
}

// TestConnection runs `SELECT 1` against the registered gaussdb-prod
// connection. Used by the per-row "Test connection" button in
// Settings — same shape as customer.Manager.TestConnection so the
// router can dispatch by driver.
func (c *Client) TestConnection(ctx context.Context, conn store.Connection) error {
	if conn.Host == "" {
		return errors.New("host required")
	}
	if conn.User == "" {
		return errors.New("user required")
	}
	pool, err := buildPool(ctx, conn)
	if err != nil {
		return err
	}
	defer pool.Close()
	return pool.Ping(ctx)
}

// Invalidate drops the cached pool so the next call rebuilds with
// fresh credentials. Called from the connections-routes handler when
// the operator saves a new password.
func (c *Client) Invalidate() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.pool != nil {
		c.pool.Close()
		c.pool = nil
		c.cachedConnID = ""
	}
	c.mu.Unlock()
	c.usageMu.Lock()
	c.usageCache = map[string]usageCacheEntry{}
	c.usageMu.Unlock()
	c.catalogueMu.Lock()
	c.catalogueExpires = time.Time{}
	c.catalogueMu.Unlock()
}

// Close tears down the pool. Call from orderly shutdown.
func (c *Client) Close() { c.Invalidate() }

// connection picks the gaussdb row to use. Looks for the configured
// ID first (default `gaussdb-prod`); if absent, falls back to any
// postgres-driver row whose ID contains "gauss" so the operator
// doesn't have to rename their existing row.
func (c *Client) connection() (store.Connection, bool, error) {
	conns, err := c.store.ListConnections()
	if err != nil {
		return store.Connection{}, false, err
	}
	for _, conn := range conns {
		if conn.ID == c.conn && strings.EqualFold(conn.Driver, "postgres") {
			if conn.Filled() {
				return conn, true, nil
			}
		}
	}
	for _, conn := range conns {
		if !strings.EqualFold(conn.Driver, "postgres") {
			continue
		}
		if !strings.Contains(strings.ToLower(conn.ID), "gauss") {
			continue
		}
		if conn.Filled() {
			return conn, true, nil
		}
	}
	return store.Connection{}, false, nil
}

// poolFor returns the cached pool, building it on first call. Drops
// and rebuilds if the connection ID has changed since the cache was
// populated (operator edited credentials between calls).
func (c *Client) poolFor(ctx context.Context) (*pgxpool.Pool, error) {
	conn, ok, err := c.connection()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotConfigured
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pool != nil && c.cachedConnID == conn.ID {
		return c.pool, nil
	}
	if c.pool != nil {
		c.pool.Close()
		c.pool = nil
		c.cachedConnID = ""
	}
	pool, err := buildPool(ctx, conn)
	if err != nil {
		return nil, err
	}
	c.pool = pool
	c.cachedConnID = conn.ID
	return pool, nil
}

// buildPool opens a pgxpool against a Connection. Same shape as
// customer/pool.go but with a longer statement_timeout (CDR fact
// tables can take seconds to scan) and a smaller MaxConns (we don't
// expect Customer 360 fan-out as wide as the BSS cluster).
func buildPool(ctx context.Context, conn store.Connection) (*pgxpool.Pool, error) {
	if !conn.Filled() {
		return nil, ErrNotConfigured
	}
	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s&connect_timeout=5",
		url.QueryEscape(conn.User),
		url.QueryEscape(conn.Password),
		conn.Host,
		conn.Port,
		url.QueryEscape(conn.Database),
		url.QueryEscape(conn.SSLMode),
	)
	pcfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn for %s: %w", conn.ID, err)
	}
	pcfg.MaxConns = 4
	pcfg.MinConns = 0
	pcfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		// 30s is generous for a per-MSISDN fact-table scan; every
		// query also runs under a context deadline so the client
		// gives up earlier when needed.
		_, err := c.Exec(ctx, "SET statement_timeout = 30000")
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("pool %s: %w", conn.ID, err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping %s: %w", conn.ID, err)
	}
	return pool, nil
}

func (c *Client) usageCacheGet(msisdn string) (axiomapi.UsageSummary, bool) {
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	e, ok := c.usageCache[msisdn]
	if !ok || time.Now().After(e.expires) {
		return axiomapi.UsageSummary{}, false
	}
	return e.value, true
}

func (c *Client) usageCacheSet(msisdn string, v axiomapi.UsageSummary) {
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	c.usageCache[msisdn] = usageCacheEntry{
		value:   v,
		expires: time.Now().Add(60 * time.Second),
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
