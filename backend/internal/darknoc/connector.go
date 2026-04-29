// Package darknoc is the read-only data layer for the rain Dark NOC
// HUD. Three responsibilities, kept explicit:
//
//  1. Connector — interface every UI tile + chat tool reads through.
//     Implementations: ClickHouseAdapter (real telemetry) +
//     GrafanaProxy (templated panel queries via the Grafana API).
//
//  2. Registry — the 41-agent reference list parsed from
//     Downloads/DarkNoc.md at startup. Static for v1; live fetch
//     from the Capgemini Open Registry is in TODOS.md.
//
//  3. Cache — TTL-bounded process-wide cache mirroring the skills
//     scanner pattern. ClickHouse responses are cached for 30s so a
//     page refresh doesn't hammer the cluster.
//
// Everything in here is read-only. Writes (resolve alert, append
// memory, create incident) flow through the existing routes; this
// package never mutates state.
package darknoc

import (
	"context"
	"sync"
	"time"
)

// Connector is the surface every Dark NOC consumer (HTTP routes,
// Cybertron chat tool) calls into. New backing stores plug in by
// implementing this interface.
type Connector interface {
	// Overview returns the rolled-up KPI snapshot — fault counts,
	// slice health, fleet trust gauge. Cached.
	Overview(ctx context.Context) (Overview, error)

	// Faults returns the latest faults (capped at 50). Cached.
	Faults(ctx context.Context) ([]Fault, error)

	// Registry returns the static 41-agent reference list. Loaded once.
	Registry() []RegistryAgent

	// CrawlCatalogue walks system.databases / .tables / .columns and
	// returns the full schema. Powers the Cybertron `clickhouse_schema`
	// chat tool so the agent composes valid SQL instead of guessing
	// table names. Cached for 10 minutes server-side.
	CrawlCatalogue(ctx context.Context) (Catalogue, error)
}

// Overview is the at-a-glance KPI bundle the page header uses.
//
// TotalEvents24h is the denominator for the fault/critical/breaching
// counts — exposed in the response so operators (and Cybertron) can
// reason about ratios directly. Trust score uses the ratios; raw
// counts alone don't tell you whether 9M breaches is normal baseline
// (rain does ~1B procedures/day in steady state) or an incident.
type Overview struct {
	GeneratedAt        time.Time `json:"generated_at"`
	TotalEvents24h     int       `json:"total_events_24h"`
	FaultsLast24h      int       `json:"faults_last_24h"`
	CriticalFaults24h  int       `json:"critical_faults_24h"`
	ActiveSlices       int       `json:"active_slices"`
	SlicesBreachingSLA int       `json:"slices_breaching_sla"`
	NetworkTrustScore  int       `json:"network_trust_score"` // 0-100
	Source             string    `json:"source"`              // "clickhouse" | "stub" | "unavailable"
	SourceLatencyMS    int64     `json:"source_latency_ms"`
	Note               string    `json:"note,omitempty"`
}

// Fault is one row from the upstream ClickHouse fault stream. Shape
// matches what the Grafana isoc dashboard renders so operators read
// the same numbers in both places.
type Fault struct {
	ID         string    `json:"id"`
	OccurredAt time.Time `json:"occurred_at"`
	Severity   string    `json:"severity"` // critical | warning | info
	Source     string    `json:"source"`   // service / element id
	Region     string    `json:"region,omitempty"`
	Technology string    `json:"technology,omitempty"` // 5G / 4G / FWA / Loop
	Title      string    `json:"title"`
	Detail     string    `json:"detail,omitempty"`
}

// RegistryAgent is one entry from the Capgemini Open Registry of
// Telecom AI Agents. Static reference data; not live.
type RegistryAgent struct {
	Name     string `json:"name"`
	Domain   string `json:"domain"`    // RAN / Core / OSS / Service / Customer / etc.
	Category string `json:"category"`  // Analytics / Automation / Optimization / etc.
	Summary  string `json:"summary"`
	Protocol string `json:"protocol"`
	UseCase  string `json:"use_case"`
}

// ttlCache is a 1-key TTL cache the adapters share for Overview
// (the most frequently hit endpoint). Mirrors the skills scanner
// cache pattern — small, lock-around-the-snapshot, no eviction loop.
type ttlCache[T any] struct {
	mu      sync.Mutex
	value   T
	loaded  bool
	expires time.Time
}

func (c *ttlCache[T]) get() (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.loaded || time.Now().After(c.expires) {
		var zero T
		return zero, false
	}
	return c.value, true
}

func (c *ttlCache[T]) set(v T, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value = v
	c.loaded = true
	c.expires = time.Now().Add(ttl)
}

func (c *ttlCache[T]) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loaded = false
}
