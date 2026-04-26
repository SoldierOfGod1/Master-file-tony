// Package sales owns the rain Sales dashboard data feed. A single
// background poller runs every few minutes, executes a small set of
// expensive analytic queries against Axiom, and stores the result in
// an atomic.Value. HTTP requests read from that snapshot — users
// opening the tab never trigger a DB query directly. That's the only
// way the SA BSS team is comfortable letting this dashboard run
// against prod: one producer, N consumers, all reads coming from
// memory.
package sales

import "time"

// ChannelStats is the Total / Web / Call Centre / Retail roll-up the
// Grafana reference dashboard puts across the top. We mirror the
// exact channel breakdown so the UI lines up 1:1.
type ChannelStats struct {
	Total      int `json:"total"`
	Web        int `json:"web"`
	CallCentre int `json:"call_centre"`
	Retail     int `json:"retail"`
}

// RevenueByChannel is the ZAR counterpart to ChannelStats.
type RevenueByChannel struct {
	Total      float64 `json:"total"`
	Web        float64 `json:"web"`
	CallCentre float64 `json:"call_centre"`
	Retail     float64 `json:"retail"`
}

// TrendPoint is one hour of the "today vs yesterday vs 7 days ago"
// comparison chart. Counts are cumulative within the day so the line
// is monotonic — same contract as the Grafana panel.
type TrendPoint struct {
	Hour      string `json:"hour"`       // "HH:00" local SAST
	Today     int    `json:"today"`
	Yesterday int    `json:"yesterday"`
	LastWeek  int    `json:"last_week"`
}

// MTDProgress compares month-to-date actuals to a budget target
// pulled from product.targets. Pct is the derived % of budget hit.
type MTDProgress struct {
	Actual float64 `json:"actual"`
	Budget float64 `json:"budget"`
	Pct    float64 `json:"pct"`
}

// ProductSnapshot is one tab's worth of data (rainOne or Loop).
type ProductSnapshot struct {
	SalesCount          ChannelStats           `json:"sales_count"`
	YesterdaySalesCount ChannelStats           `json:"yesterday_sales_count"`
	WrittenRevenue      RevenueByChannel       `json:"written_revenue"`
	MTDSalesCount       MTDProgress            `json:"mtd_sales_count"`
	MTDRevenue          MTDProgress            `json:"mtd_revenue"`
	Trend               []TrendPoint           `json:"trend"`
	Fulfilment          FulfilmentStats        `json:"fulfilment"`
	PaymentHealth       PaymentHealthStats     `json:"payment_health"`
	CallCentre          CallCentreKPIs         `json:"call_centre_kpis"`
	CallCentreTrend     []CallCentreTrendPoint `json:"call_centre_trend"`
	BillRunErrors       []BillRunErrorBucket   `json:"bill_run_errors"`
	Errors              []SourceError          `json:"errors,omitempty"`
	LatencyMS           map[string]int64       `json:"latency_ms,omitempty"`
}

// FulfilmentStats mirrors the tv-final "Order Fulfilment" panel —
// four counters flowing from manufactured to delivered, plus a
// failed bucket. Zero-valued until the order-fulfilment SQL is
// wired against the rain OMS/warehouse view.
type FulfilmentStats struct {
	Manufactured int     `json:"manufactured"`
	InTransit    int     `json:"in_transit"`
	Delivered    int     `json:"delivered"`
	Failed       int     `json:"failed"`
	PctDelivered float64 `json:"pct_delivered"`
}

// PaymentStatusBucket is one slice of the "Payment Health" panel.
type PaymentStatusBucket struct {
	Count int     `json:"count"`
	Pct   float64 `json:"pct"`
}

// PaymentHealthStats mirrors the tv-final "Payment Health" panel.
// The total_value is ex-VAT ZAR. Zero-valued until the payment SQL
// is wired against the payment DB.
type PaymentHealthStats struct {
	TotalPayments int                 `json:"total_payments"`
	TotalValue    float64             `json:"total_value"`
	Successful    PaymentStatusBucket `json:"successful"`
	Failed        PaymentStatusBucket `json:"failed"`
	Retry         PaymentStatusBucket `json:"retry"`
	Pending       PaymentStatusBucket `json:"pending"`
}

// CallCentreKPIs is the 5-tile cluster at bottom-left of tv-final.
// Zero-valued until the CC source (Genesys/Five9 export?) is wired.
type CallCentreKPIs struct {
	CallsToday    int     `json:"calls_today"`
	AnswerRatePct float64 `json:"answer_rate_pct"`
	AvgWaitSec    float64 `json:"avg_wait_sec"`
	Abandoned     int     `json:"abandoned"`
	ServiceLevel  float64 `json:"service_level_pct"`
}

// CallCentreTrendPoint feeds the "Call Centre — Orders Today" line
// chart at bottom-centre of tv-final (today vs yesterday).
type CallCentreTrendPoint struct {
	Hour      string `json:"hour"`
	Today     int    `json:"today"`
	Yesterday int    `json:"yesterday"`
}

// BillRunErrorBucket feeds the horizontal bars at bottom-right of
// tv-final: Insufficient Funds, Limit Exceeded, Wrong Expiry Date,
// On Hold, Do Not Honour, etc.
type BillRunErrorBucket struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

// SourceError carries per-query failure detail so the UI can render a
// "Call Centre — data unavailable" chip rather than silently hiding a
// tile. Same contract as customer.DataSourceStatus.
type SourceError struct {
	Source string `json:"source"`
	Error  string `json:"error"`
}

// Snapshot is the whole payload. Today-window only for v1 — date
// picker will re-query on demand via a separate handler later.
type Snapshot struct {
	AsOf        time.Time       `json:"as_of"`
	Window      string          `json:"window"` // "today" for v1
	TimezoneTZ  string          `json:"timezone"`
	RainOne     ProductSnapshot `json:"rainone"`
	Loop        ProductSnapshot `json:"loop"`
	PollLatency int64           `json:"poll_latency_ms"`
	PollErrors  int             `json:"poll_errors"`
}
