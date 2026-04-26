// Package customer owns the customer lookup data path. Postgres pools are
// keyed by connection id (defined in the store/connections registry), so a
// single Manager can serve queries against multiple rain clusters.
package customer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SoldierOfGod1/command-centre/internal/store"
)

// ErrNotConfigured signals the caller should surface a 503 + "configure
// a connection in Settings".
var ErrNotConfigured = errors.New("customer: no usable connection configured")

// ErrClickHouseUnsupported is returned when a caller asks for a pool on
// a non-postgres connection. Reserved slot — the ClickHouse driver isn't
// wired yet, but the connection entry is still useful for the UI.
var ErrClickHouseUnsupported = errors.New("customer: ClickHouse driver not yet wired — Postgres only for now")

// Manager hands out pgxpool.Pools keyed by connection id. Pools are built
// on first use and cached; Invalidate(id) drops one so the next call
// rebuilds with freshly-saved settings.
//
// Athena is attached optionally; LookupProd can reach it via
// AthenaUsage() without adding a constructor parameter. Nil means
// "Athena not configured" — callers must check before using.
type Manager struct {
	s      *store.Store
	mu     sync.Mutex
	pools  map[string]*pgxpool.Pool
	athena UsageLookerUpper
}

// UsageLookerUpper is the minimal interface LookupProd needs from
// the Athena usage service. Defined here as an interface so the
// customer package doesn't import internal/athena directly —
// main.go passes in a concrete adapter.
type UsageLookerUpper interface {
	Available() bool
	UsageSince(ctx context.Context, imsis []int64) ([]CDRUsage, error)
}

// SetAthenaUsage attaches the optional Athena usage service. Pass
// nil to disable. Called once at startup from main.go; not safe to
// call concurrently with lookups.
func (m *Manager) SetAthenaUsage(u UsageLookerUpper) { m.athena = u }

// AthenaUsage returns the attached service (may be nil).
func (m *Manager) AthenaUsage() UsageLookerUpper { return m.athena }

// LocalDB exposes the SQLite handle the Manager was built against.
// v2 Customer 360 uses it to persist NBA recommendations + outcome
// actions. Returning via an accessor avoids making the field
// exported and lets us swap the store later without touching
// lookup code.
func (m *Manager) LocalDB() *sql.DB {
	if m == nil || m.s == nil {
		return nil
	}
	return m.s.DB
}

// NewManager wires the Manager to the app's Store singleton.
func NewManager(s *store.Store) *Manager {
	return &Manager{s: s, pools: map[string]*pgxpool.Pool{}}
}

// PrimaryPool returns the pool for whichever connection is flagged primary.
// Used by Customer 360 when the UI doesn't specify a connection id.
func (m *Manager) PrimaryPool(ctx context.Context) (*pgxpool.Pool, store.Connection, error) {
	c, ok, err := m.s.PrimaryConnection()
	if err != nil {
		return nil, store.Connection{}, err
	}
	if !ok {
		return nil, store.Connection{}, ErrNotConfigured
	}
	pool, err := m.poolFor(ctx, c)
	return pool, c, err
}

// PoolByID returns the pool for a specific connection id. Used when the
// UI's connection picker selects a non-default cluster.
func (m *Manager) PoolByID(ctx context.Context, id string) (*pgxpool.Pool, store.Connection, error) {
	c, ok, err := m.s.GetConnection(id)
	if err != nil {
		return nil, store.Connection{}, err
	}
	if !ok {
		return nil, store.Connection{}, fmt.Errorf("connection %q not found", id)
	}
	pool, err := m.poolFor(ctx, c)
	return pool, c, err
}

// PoolByIDWithDB is PoolByID but lets the caller override the database
// name for a multi-DB cluster (like Axiom prod, which splits each BSS
// domain into its own database: customer / payment / party / service /
// etc.). A pool is cached per (id, db) pair so repeat calls are cheap.
func (m *Manager) PoolByIDWithDB(ctx context.Context, id, database string) (*pgxpool.Pool, store.Connection, error) {
	c, ok, err := m.s.GetConnection(id)
	if err != nil {
		return nil, store.Connection{}, err
	}
	if !ok {
		return nil, store.Connection{}, fmt.Errorf("connection %q not found", id)
	}
	if database == "" || database == c.Database {
		pool, err := m.poolFor(ctx, c)
		return pool, c, err
	}
	// Clone with the requested database name + a synthetic cache key so
	// we don't clobber the "default DB" pool for the same connection id.
	clone := c
	clone.Database = database
	clone.ID = c.ID + "::" + database
	pool, err := m.poolFor(ctx, clone)
	return pool, clone, err
}

// Pool keeps the old-style API working for legacy callers (the test route
// and anyone who hasn't been migrated to pass an explicit id yet).
func (m *Manager) Pool(ctx context.Context) (*pgxpool.Pool, error) {
	pool, _, err := m.PrimaryPool(ctx)
	return pool, err
}

// PoolByIDReadOnlyWithDB is PoolByIDWithDB but prefers the connection's
// configured read replica when one is available. Read-heavy callers
// (Customer 360 candidate fan-out, sales CTE poller) should route
// through this method instead of the primary-facing variant — even if
// the replica is not configured it falls back to the primary, so it's
// safe to adopt immediately. When a replica URL is provisioned in
// the UI, traffic shifts automatically without a code change.
func (m *Manager) PoolByIDReadOnlyWithDB(ctx context.Context, id, database string) (*pgxpool.Pool, store.Connection, error) {
	c, ok, err := m.s.GetConnection(id)
	if err != nil {
		return nil, store.Connection{}, err
	}
	if !ok {
		return nil, store.Connection{}, fmt.Errorf("connection %q not found", id)
	}
	// No replica configured → fall through to the primary pool so
	// the caller doesn't have to branch.
	if c.ReadReplicaHost == "" {
		return m.PoolByIDWithDB(ctx, id, database)
	}
	ro := c
	ro.Host = c.ReadReplicaHost
	if c.ReadReplicaPort != "" {
		ro.Port = c.ReadReplicaPort
	}
	if database != "" {
		ro.Database = database
	}
	// Cache under a synthetic id so the replica pool is separate
	// from the primary one — avoids clobbering either cache entry.
	ro.ID = c.ID + "::ro"
	if database != "" && database != c.Database {
		ro.ID = ro.ID + "::" + database
	}
	pool, err := m.poolFor(ctx, ro)
	return pool, ro, err
}

// TestConnection opens a pool for one specific connection id and pings it.
// Used by the per-row "Test connection" button in the Settings UI.
func (m *Manager) TestConnection(ctx context.Context, id string) error {
	c, ok, err := m.s.GetConnection(id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("connection %q not found", id)
	}
	pool, err := m.poolFor(ctx, c)
	if err != nil {
		return err
	}
	return pool.Ping(ctx)
}

// Invalidate drops the cached pool for a specific connection id.
func (m *Manager) Invalidate(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if pool := m.pools[id]; pool != nil {
		pool.Close()
		delete(m.pools, id)
	}
}

// InvalidateAll drops every cached pool — used on coarse rotation events.
func (m *Manager) InvalidateAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, pool := range m.pools {
		pool.Close()
		delete(m.pools, id)
	}
}

// Close tears down every pool. Call from orderly shutdown.
func (m *Manager) Close() {
	m.InvalidateAll()
}

// poolFor is the internal cache + build path.
func (m *Manager) poolFor(ctx context.Context, c store.Connection) (*pgxpool.Pool, error) {
	if !c.Filled() {
		return nil, ErrNotConfigured
	}
	if c.Driver != "postgres" {
		return nil, ErrClickHouseUnsupported
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if pool, ok := m.pools[c.ID]; ok && pool != nil {
		return pool, nil
	}

	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s&connect_timeout=5",
		url.QueryEscape(c.User),
		url.QueryEscape(c.Password),
		c.Host,
		c.Port,
		url.QueryEscape(c.Database),
		url.QueryEscape(c.SSLMode),
	)

	pcfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn for %s: %w", c.ID, err)
	}
	pcfg.MaxConns = 5
	pcfg.MinConns = 0

	// Server-side statement timeout. Client-side context timeouts alone
	// aren't enough to protect the primary from runaway scans on
	// unindexed columns — when the client abandons the query, the
	// backend keeps executing until it notices the closed socket,
	// holding a snapshot that blocks WAL application on replicas.
	// Setting statement_timeout at session level makes Postgres itself
	// cancel the query at the same deadline, releasing the snapshot
	// immediately. 10s is generous for normal analytics reads; every
	// candidate query adds its own tighter context on top.
	pcfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET statement_timeout = 10000")
		return err
	}

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("pool %s: %w", c.ID, err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping %s: %w", c.ID, err)
	}
	m.pools[c.ID] = pool
	return pool, nil
}
