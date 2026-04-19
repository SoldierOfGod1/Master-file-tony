// Package customer owns the customer lookup data path. Postgres pools are
// keyed by connection id (defined in the store/connections registry), so a
// single Manager can serve queries against multiple rain clusters.
package customer

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"

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
type Manager struct {
	s     *store.Store
	mu    sync.Mutex
	pools map[string]*pgxpool.Pool
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

// Pool keeps the old-style API working for legacy callers (the test route
// and anyone who hasn't been migrated to pass an explicit id yet).
func (m *Manager) Pool(ctx context.Context) (*pgxpool.Pool, error) {
	pool, _, err := m.PrimaryPool(ctx)
	return pool, err
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
