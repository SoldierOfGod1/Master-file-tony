package sales

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SoldierOfGod1/command-centre/internal/customer"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Poller is the singleton background worker that refreshes the sales
// snapshot on a fixed cadence. One Poller per process. HTTP requests
// never touch Axiom directly — they read from poller.snap via Load().
//
// Safety guarantees:
//   - Never more than one poll in flight at a time (mu.TryLock).
//   - Each underlying SQL query has its own timeout (queryTimeout).
//   - Total poll duration is capped by ctx deadline so a slow query
//     can never block the next tick.
//   - On error, we leave the previous snapshot in place and log so the
//     UI falls back gracefully to stale-but-visible data.
type Poller struct {
	mgr        *customer.Manager
	log        *slog.Logger
	interval   time.Duration
	timeout    time.Duration
	snap       atomic.Pointer[Snapshot]
	pollInFly  atomic.Bool
	stopOnce   sync.Once
	stopCh     chan struct{}
}

// Default cadence: 15 min. Was 3 min but the sales CTEs join six
// multi-million-row tables (product_order, product_order_item,
// product_offering_price, category, jt_prod_order_related_party,
// mvw_individual) and ran every 3 min against Axiom prod, which was
// contributing to replication lag. 15 min is cheap enough to keep
// the dashboard fresh without pressuring the primary. Callers who
// need fresher numbers trigger a one-shot refresh via the /sales/
// refresh endpoint (which honours the same poll-in-flight guard).
const (
	defaultInterval     = 15 * time.Minute
	// 5s per sub-query (was 8s). The sales CTEs hold a read
	// snapshot for the duration of execution; anything that can't
	// finish in 5s is statistically as likely to hurt replication
	// as return. The 10s pool-level statement_timeout catches
	// stragglers at the Postgres side.
	defaultQueryTimeout = 5 * time.Second
	defaultPollBudget   = 60 * time.Second
)

// NewPoller wires the manager + logger, loads an initial empty
// snapshot so first-HTTP-request doesn't see nil.
func NewPoller(mgr *customer.Manager, log *slog.Logger) *Poller {
	p := &Poller{
		mgr:      mgr,
		log:      log.With("component", "sales.poller"),
		interval: defaultInterval,
		timeout:  defaultQueryTimeout,
		stopCh:   make(chan struct{}),
	}
	// Seed with an empty snapshot so the /api/v1/sales endpoint returns
	// valid JSON before the first poll completes.
	p.snap.Store(&Snapshot{
		AsOf:       time.Time{},
		Window:     "today",
		TimezoneTZ: "Africa/Johannesburg",
	})
	return p
}

// Start kicks off the background loop. Blocking call: spawn in a
// goroutine from the caller. First poll fires immediately, then on
// interval.
func (p *Poller) Start(ctx context.Context) {
	p.log.Info("sales poller starting", "interval", p.interval.String())
	// Fire once immediately so the dashboard has data without waiting
	// a full interval.
	p.tick(ctx)
	t := time.NewTicker(p.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			p.log.Info("sales poller stopped by context")
			return
		case <-p.stopCh:
			p.log.Info("sales poller stopped")
			return
		case <-t.C:
			p.tick(ctx)
		}
	}
}

// Stop signals the run loop to exit. Safe to call more than once.
func (p *Poller) Stop() {
	p.stopOnce.Do(func() { close(p.stopCh) })
}

// Snapshot returns the most recently completed snapshot. Always
// non-nil because Start() seeds an empty one.
func (p *Poller) Snapshot() *Snapshot {
	return p.snap.Load()
}

// Refresh runs one poll cycle on demand. Honours the pollInFly
// guard, so a tide of refresh button mashes collapses into a single
// in-flight poll rather than fanning out to N concurrent loads on
// the BSS primary. Returns the snapshot stored after the attempt
// (may be the prior one if another tick was already running).
func (p *Poller) Refresh(ctx context.Context) *Snapshot {
	p.tick(ctx)
	return p.snap.Load()
}

// tick runs all queries for one poll cycle. Skips if a previous
// tick is still in flight — prevents overlapping polls from piling
// up on the BSS when a query slows down.
func (p *Poller) tick(parent context.Context) {
	if !p.pollInFly.CompareAndSwap(false, true) {
		p.log.Warn("sales poll skipped — previous still running")
		return
	}
	defer p.pollInFly.Store(false)

	start := time.Now()
	ctx, cancel := context.WithTimeout(parent, defaultPollBudget)
	defer cancel()

	// All the sales SQL lives on the `product` database. We pull it via
	// the primary connection's id to inherit its credentials + host,
	// then swap to the product db. Using PrimaryPool directly gives you
	// whichever db the connection was seeded with (usually `postgresdb`)
	// which doesn't contain product.product_order.
	//
	// SALES_POLLER_CONNECTION_ID env var overrides the primary so an
	// operator running a SIT-default install can still target prod for
	// sales — Loop / rainOne CTEs only have a known-good schema on prod
	// (e.g. product.product_order_item.product_offering_ref_id is a
	// prod-only column; SIT errors with SQLSTATE 42703).
	connID := strings.TrimSpace(os.Getenv("SALES_POLLER_CONNECTION_ID"))
	if connID == "" {
		_, primaryConn, err := p.mgr.PrimaryPool(ctx)
		if err != nil {
			p.log.Error("sales poll: no primary connection", "error", err)
			return
		}
		connID = primaryConn.ID
	}
	pool, _, err := p.mgr.PoolByIDWithDB(ctx, connID, "product")
	if err != nil {
		p.log.Error("sales poll: product pool unavailable", "error", err, "connection_id", connID)
		return
	}
	// The test-email list lookup targets party.individual — different db.
	partyPool, _, err := p.mgr.PoolByIDWithDB(ctx, connID, "party")
	if err != nil {
		p.log.Warn("sales poll: party pool unavailable — using empty test-email list", "error", err)
		partyPool = nil
	}

	// Resolve test-email ids once per tick so every query gets the
	// same exclusion list. Uses party DB since that's where
	// party.individual lives.
	var testEmails []string
	if partyPool != nil {
		list, terr := testEmailList(ctx, partyPool)
		if terr != nil {
			p.log.Warn("sales poll: testEmailList failed — using empty", "error", terr)
		} else {
			testEmails = list
		}
	}
	if testEmails == nil {
		testEmails = []string{}
	}

	// Run the two products in parallel — they touch the same tables
	// but with different CTE selections so concurrent execution helps.
	rainOneCh := make(chan ProductSnapshot, 1)
	loopCh := make(chan ProductSnapshot, 1)

	go func() { rainOneCh <- p.pollProduct(ctx, pool, productRainOne, testEmails) }()
	go func() { loopCh <- p.pollProduct(ctx, pool, productLoop, testEmails) }()

	rain := <-rainOneCh
	loop := <-loopCh

	snap := &Snapshot{
		AsOf:        time.Now().UTC(),
		Window:      "today",
		TimezoneTZ:  "Africa/Johannesburg",
		RainOne:     rain,
		Loop:        loop,
		PollLatency: time.Since(start).Milliseconds(),
		PollErrors:  len(rain.Errors) + len(loop.Errors),
	}
	p.snap.Store(snap)

	p.log.Info("sales poll complete",
		"latency_ms", snap.PollLatency,
		"errors", snap.PollErrors,
		"rainone_total", rain.SalesCount.Total,
		"loop_total", loop.SalesCount.Total,
	)
}

type productKind int

const (
	productRainOne productKind = iota
	productLoop
)

// pollProduct runs all queries for one product (rainOne or Loop)
// and collects them into a ProductSnapshot. Each query is
// independently timed so one slow one doesn't sink the whole tab.
func (p *Poller) pollProduct(ctx context.Context, pool *pgxpool.Pool, kind productKind, testEmails []string) ProductSnapshot {
	snap := ProductSnapshot{LatencyMS: map[string]int64{}}
	todayStart, todayEnd := dayBounds(nowSAST())
	yesterdayStart, yesterdayEnd := dayBounds(nowSAST().AddDate(0, 0, -1))
	lastWeekStart, lastWeekEnd := dayBounds(nowSAST().AddDate(0, 0, -7))

	var mu sync.Mutex
	record := func(source string, lat time.Duration, err error) {
		mu.Lock()
		defer mu.Unlock()
		snap.LatencyMS[source] = lat.Milliseconds()
		if err != nil {
			snap.Errors = append(snap.Errors, SourceError{Source: source, Error: err.Error()})
			p.log.Warn("sales query failed", "source", source, "error", err)
		}
	}

	// Sales count by channel — today window
	countSQL := fmt.Sprintf(salesCountByChannelSQL, cteFor(kind))
	countSQL = fmt.Sprintf(countSQL, staffExclusionSQLFragment())
	{
		t0 := time.Now()
		qctx, cancel := context.WithTimeout(ctx, p.timeout)
		row := pool.QueryRow(qctx, countSQL, todayStart, todayEnd, testEmails)
		var c ChannelStats
		err := row.Scan(&c.Total, &c.Web, &c.CallCentre, &c.Retail)
		cancel()
		if err == nil {
			snap.SalesCount = c
		}
		record("sales_count", time.Since(t0), err)
	}

	// Sales count by channel — same time-of-day yesterday. Needed so
	// the top-row KPI tiles can render a % change vs yesterday badge
	// (matches the tv-final "+12.4% vs yesterday" chip).
	{
		t0 := time.Now()
		qctx, cancel := context.WithTimeout(ctx, p.timeout)
		// Use yesterday's full-day window so the delta is a like-for-like
		// day comparison; the sparkline itself is today-only.
		row := pool.QueryRow(qctx, countSQL, yesterdayStart, yesterdayEnd, testEmails)
		var c ChannelStats
		err := row.Scan(&c.Total, &c.Web, &c.CallCentre, &c.Retail)
		cancel()
		if err == nil {
			snap.YesterdaySalesCount = c
		}
		record("sales_count_yesterday", time.Since(t0), err)
	}

	// Written revenue by channel — today window
	revSQL := fmt.Sprintf(revenueByChannelSQL, cteFor(kind))
	revSQL = fmt.Sprintf(revSQL, staffExclusionSQLFragment())
	{
		t0 := time.Now()
		qctx, cancel := context.WithTimeout(ctx, p.timeout)
		row := pool.QueryRow(qctx, revSQL, todayStart, todayEnd, testEmails)
		var r RevenueByChannel
		err := row.Scan(&r.Total, &r.Web, &r.CallCentre, &r.Retail)
		cancel()
		if err == nil {
			snap.WrittenRevenue = r
		}
		record("revenue", time.Since(t0), err)
	}

	// MTD sales count — this is just the count query with a wider window
	monthStart, monthEnd := monthBounds(nowSAST())
	{
		t0 := time.Now()
		qctx, cancel := context.WithTimeout(ctx, p.timeout)
		row := pool.QueryRow(qctx, countSQL, monthStart, monthEnd, testEmails)
		var c ChannelStats
		err := row.Scan(&c.Total, &c.Web, &c.CallCentre, &c.Retail)
		cancel()
		if err == nil {
			snap.MTDSalesCount.Actual = float64(c.Total)
		}
		record("mtd_count", time.Since(t0), err)
	}
	{
		t0 := time.Now()
		qctx, cancel := context.WithTimeout(ctx, p.timeout)
		row := pool.QueryRow(qctx, revSQL, monthStart, monthEnd, testEmails)
		var r RevenueByChannel
		err := row.Scan(&r.Total, &r.Web, &r.CallCentre, &r.Retail)
		cancel()
		if err == nil {
			snap.MTDRevenue.Actual = r.Total
		}
		record("mtd_revenue", time.Since(t0), err)
	}

	// MTD budget — single query, shared between rainOne/Loop but
	// home_count/home_revenue columns are rainOne-specific. Loop's
	// budget lives in a different column set we don't have yet, so
	// for v1 the loop MTD budget stays 0 and the UI shows "budget
	// not configured".
	if kind == productRainOne {
		t0 := time.Now()
		qctx, cancel := context.WithTimeout(ctx, p.timeout)
		row := pool.QueryRow(qctx, mtdBudgetSQL)
		var budgetCount int
		var budgetRev float64
		err := row.Scan(&budgetCount, &budgetRev)
		cancel()
		if err == nil {
			snap.MTDSalesCount.Budget = float64(budgetCount)
			snap.MTDRevenue.Budget = budgetRev
		}
		record("mtd_budget", time.Since(t0), err)
	}

	// Derive MTD percentages
	if snap.MTDSalesCount.Budget > 0 {
		snap.MTDSalesCount.Pct = snap.MTDSalesCount.Actual / snap.MTDSalesCount.Budget * 100
	}
	if snap.MTDRevenue.Budget > 0 {
		snap.MTDRevenue.Pct = snap.MTDRevenue.Actual / snap.MTDRevenue.Budget * 100
	}

	// Trend — three windows run serially to keep pressure bounded
	trendSQL := fmt.Sprintf(trendHourSQL, cteFor(kind))
	trendSQL = fmt.Sprintf(trendSQL, staffExclusionSQLFragment())
	todayTrend := runTrend(ctx, pool, p.timeout, trendSQL, todayStart, todayEnd, testEmails)
	yesterdayTrend := runTrend(ctx, pool, p.timeout, trendSQL, yesterdayStart, yesterdayEnd, testEmails)
	lastWeekTrend := runTrend(ctx, pool, p.timeout, trendSQL, lastWeekStart, lastWeekEnd, testEmails)
	snap.Trend = stitchTrend(todayTrend, yesterdayTrend, lastWeekTrend)
	record("trend", 0, nil) // latency is per-subquery; coarse 0 here is fine

	return snap
}

// cteFor returns the right base CTE for the product.
func cteFor(kind productKind) string {
	if kind == productLoop {
		return loopOrderCTE
	}
	return rainOneOrderCTE
}

type hourCount struct {
	hour  string
	count int
}

func runTrend(ctx context.Context, pool *pgxpool.Pool, timeout time.Duration, sql string, start, end time.Time, testEmails []string) []hourCount {
	qctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	rows, err := pool.Query(qctx, sql, start, end, testEmails)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []hourCount
	for rows.Next() {
		var h hourCount
		if err := rows.Scan(&h.hour, &h.count); err != nil {
			continue
		}
		out = append(out, h)
	}
	return out
}

// stitchTrend zips the three per-hour slices into TrendPoint rows.
// If any series failed to run (empty slice), its column stays 0.
func stitchTrend(today, yesterday, lastWeek []hourCount) []TrendPoint {
	n := 24
	out := make([]TrendPoint, n)
	for i := 0; i < n; i++ {
		hour := fmt.Sprintf("%02d:00", i)
		out[i] = TrendPoint{Hour: hour}
		if i < len(today) {
			out[i].Today = today[i].count
		}
		if i < len(yesterday) {
			out[i].Yesterday = yesterday[i].count
		}
		if i < len(lastWeek) {
			out[i].LastWeek = lastWeek[i].count
		}
	}
	return out
}

func nowSAST() time.Time {
	loc, _ := time.LoadLocation("Africa/Johannesburg")
	if loc == nil {
		loc = time.UTC
	}
	return time.Now().In(loc)
}
