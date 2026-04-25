package customer

import (
	"sync/atomic"
	"time"
)

// Customer360 is the full payload returned by /api/v1/customer. Designed to
// render an entire client detail view from one HTTP call — no follow-up
// round-trips needed for the initial render.
type Customer360 struct {
	Identity             Identity        `json:"identity"`
	Contacts             []ContactMedium `json:"contacts"`
	Payments             []Payment       `json:"payments"`
	Subscriptions        []Subscription  `json:"subscriptions"`
	Tickets              []Ticket        `json:"tickets"`
	Chargebacks          []Chargeback    `json:"chargebacks"`
	RiskScore            RiskScore       `json:"risk_score"`
	LifetimeValue        float64         `json:"lifetime_value"`
	AccountAge           AccountAge      `json:"account_age"`
	DaysSinceLastPayment int             `json:"days_since_last_payment"`
	Timeline             []TimelineEvent `json:"timeline"`
	PaymentHeatmap       []int           `json:"payment_heatmap"`
	Neighbours           []Neighbour     `json:"neighbours"`
	DeepLinks            DeepLinks       `json:"deep_links"`
	LookedUpBy           string          `json:"looked_up_by"`
	LookedUpAt           time.Time       `json:"looked_up_at"`
	ChurnRisk            string          `json:"churn_risk"`
	BillingAccounts      []BillingAccount    `json:"billing_accounts,omitempty"`
	Balances             []AccountBalance    `json:"balances,omitempty"`
	Invoices             []Invoice           `json:"invoices,omitempty"`
	Promises             []PromiseToPay      `json:"promises,omitempty"`
	RecentNotifications  []NotificationEvent `json:"recent_notifications,omitempty"`
	Products             []Product           `json:"products,omitempty"`
	Usage                []UsageSnapshot     `json:"usage,omitempty"`
	DataSources          []DataSourceStatus  `json:"data_sources"`
	Candidates           []IdentityCandidate `json:"candidates,omitempty"`
	CDRUsage             []CDRUsage          `json:"cdr_usage,omitempty"`

	// ---- v2 decisioning layer (rules-based; ML replaces it later) ----
	Predictions     *Predictions     `json:"predictions,omitempty"`
	JourneyStage    *JourneyStage    `json:"journey_stage,omitempty"`
	Recommendations []Recommendation `json:"recommendations,omitempty"`

	// IMSIOverrides is an internal-only field carrying manually-
	// configured IMSIs for this customer. Loaded from
	// customer_imsi_overrides before the fetch fan-out so the
	// Usage + CDR Usage sources can use them directly. Never
	// serialised to the UI (operators manage the list via a
	// dedicated endpoint + UI input).
	IMSIOverrides []int64 `json:"-"`

	// SimDiagnostics is the panel-facing record of every IMSI we
	// resolved + which cascade phase produced it. Populated once
	// per LookupProd by the first resolveIMSIs call. Phase 2 of
	// docs/axiom/sim-diagnostics-plan.md.
	SimDiagnostics []IMSISource `json:"sim_diagnostics,omitempty"`

	// auditFailed is the fail-closed signal flipped by a deferred
	// audit write in resolveIMSIs. LookupProd reads it after the
	// fan-out and refuses to return Customer360 if set, surfacing
	// HTTP 500 to the handler. POPIA audit must never be best-effort
	// — eng-review 2A. Not serialised.
	auditFailed atomic.Bool
}

// MarkAuditFailed flips the internal fail-closed signal. Called by
// resolveIMSIs's deferred audit write when persisting an audit row
// errors. LookupProd reads this via AuditFailed() after the fetch
// fan-out completes.
func (c *Customer360) MarkAuditFailed() {
	c.auditFailed.Store(true)
}

// AuditFailed reports whether any IMSI audit write failed during
// this lookup. When true, the response must NOT be returned to the
// caller — POPIA audit is fail-closed.
func (c *Customer360) AuditFailed() bool {
	return c.auditFailed.Load()
}

// IMSISource pairs an IMSI with which cascade phase produced it.
// Returned by resolveIMSIs and surfaced to the SIM Diagnostics
// panel via Customer360.SimDiagnostics. Phase 2 of
// docs/axiom/sim-diagnostics-plan.md (eng-review 1B).
//
// Source values are stable across releases — the panel relies on
// these strings to render its phase tag-chip row:
//   override         — Phase 0, customer_imsi_overrides hit
//   product_path     — Phase 1, product → jt_prod_rs_ref → resource_ref
//   view_account     — Phase 2, vw_service_account_state_latest by billing
//   view_msisdn      — Phase 3, view by phone-number lookup
//   view_subscriber  — Phase 4, view by service_accounts.subscriber
type IMSISource struct {
	IMSI       int64     `json:"imsi"`
	Source     string    `json:"source"`
	ResolvedAt time.Time `json:"resolved_at"`
}

// Predictions bundles the per-customer scores shown in the left-rail
// "Prediction Stack" panel. v1 values are derived from rules over the
// existing Customer360 payload — not ML. The field shapes match the
// target spec so a future ML model can drop in without changing the
// UI contract.
type Predictions struct {
	Churn30Day        float64  `json:"churn_30d"`         // 0..1
	Churn60Day        float64  `json:"churn_60d"`         // 0..1
	Churn90Day        float64  `json:"churn_90d"`         // 0..1
	PaymentDefault30d float64  `json:"payment_default_30d"` // 0..1
	LTV12mExpected    float64  `json:"ltv_12m_expected"`  // ZAR
	UpsellPropensity  float64  `json:"upsell_propensity"` // 0..1
	Confidence        float64  `json:"confidence"`        // 0..1 — rules get a fixed 0.6
	ReasonCodes       []string `json:"reason_codes"`      // human-readable
	ModelVersion      string   `json:"model_version"`     // e.g. "rules_v1"
	ComputedAt        string   `json:"computed_at"`
}

// JourneyStage is the derived lifecycle marker shown in the left
// rail. v1 uses a small state machine driven by tenure + risk flags.
type JourneyStage struct {
	Stage        string   `json:"stage"` // Onboarding | Activation | Growth | Friction | Retention | Recovery | Loyalty
	EnteredAt    string   `json:"entered_at,omitempty"`
	TriggeringEvents []string `json:"triggering_events,omitempty"`
}

// Recommendation is one NBA entry shown in the left-rail "Next Best
// Action" panel. Shape matches the spec so outcomes can be fed back
// into an uplift model later. Status is the lifecycle flag —
// "presented" on first render, "accepted" / "dismissed" after the
// agent acts.
type Recommendation struct {
	ID             string   `json:"id"`
	CustomerID     string   `json:"customer_id"`
	Type           string   `json:"type"`             // retention_offer | collections_action | upsell | service_action
	Title          string   `json:"title"`
	Description    string   `json:"description,omitempty"`
	Channel        string   `json:"channel"`          // sms | email | call | agent
	PriorityRank   int      `json:"priority_rank"`
	ExpectedValue  float64  `json:"expected_value"`   // ZAR
	CostEstimate   float64  `json:"cost_estimate"`    // ZAR
	Constraints    []string `json:"constraints,omitempty"`
	ReasonCodes    []string `json:"reason_codes"`
	Status         string   `json:"status"`           // presented | accepted | dismissed | snoozed
	CreatedAt      string   `json:"created_at"`
}

// CDRUsage is one day of GPRS data usage for one SIM, sourced from
// Athena's iv_usage_cdr_detail. Aggregated server-side so the UI
// doesn't have to do the byte math.
type CDRUsage struct {
	Date           time.Time `json:"date"`
	AccountCode    string    `json:"account_code"`
	BillingAccount string    `json:"billing_account"`
	IMEI           string    `json:"imei"`
	IMSI           string    `json:"imsi"`
	MSISDN         string    `json:"msisdn"`
	UsageGB        float64   `json:"usage_gb"`
}

// IdentityCandidate is one of several party.individual rows that
// matched a given phone or email. Returned in Candidates when the
// lookup is ambiguous so the UI can show a picker rather than
// silently picking the first match (rain's family plans share a
// phone across several individuals — common enough to be a
// first-class case).
//
// AccountNumber + MSISDN come from customer.vw_service_account_state_latest
// when the candidate was resolved through the SIM inventory view.
// They help the user tell accounts apart when party.individual
// metadata is sparse (new SIM with no email / name filled yet).
type IdentityCandidate struct {
	ID            string    `json:"id"`
	FullName      string    `json:"full_name"`
	GivenName     string    `json:"given_name"`
	FamilyName    string    `json:"family_name"`
	Email         string    `json:"email"`
	CreatedAt     time.Time `json:"created_at"`
	AccountNumber string    `json:"account_number,omitempty"`
	MSISDN        string    `json:"msisdn,omitempty"`
	Source        string    `json:"source,omitempty"` // "contact_medium" | "sim_view"
}

// BillingAccount is one row from account.account.billing_account. State
// + payment status drive the dunning flag + colour in the UI.
type BillingAccount struct {
	ID                  string    `json:"id"`
	Name                string    `json:"name"`
	State               string    `json:"state"`
	AccountType         string    `json:"account_type"`
	PaymentStatus       string    `json:"payment_status"`
	CreditLimit         float64   `json:"credit_limit"`
	PaymentDay          int       `json:"payment_day"`
	FinancialAccountID  string    `json:"financial_account_id,omitempty"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// AccountBalance collapses account.account.account_balance rows per
// balance_type (e.g. MAIN, CREDIT, PREPAID_BUCKET).
type AccountBalance struct {
	BalanceType      string    `json:"balance_type"`
	Amount           float64   `json:"amount"`
	LastInvoiceAmount float64  `json:"last_invoice_amount,omitempty"`
	ValidFrom        time.Time `json:"valid_from,omitempty"`
	ValidTo          time.Time `json:"valid_to,omitempty"`
}

// Invoice is one row from customer.public.invoices.
type Invoice struct {
	InvoiceNumber string    `json:"invoice_number"`
	InvoiceDate   time.Time `json:"invoice_date"`
	DueDate       time.Time `json:"due_date"`
	Amount        float64   `json:"amount"`
	Balance       float64   `json:"balance"`
	Status        string    `json:"status"`
	Source        string    `json:"source"`
}

// PromiseToPay captures an active or recent instalment plan.
type PromiseToPay struct {
	ID                string    `json:"id"`
	Status            string    `json:"status"`
	TotalAmount       float64   `json:"total_amount"`
	TotalAllocated    float64   `json:"total_allocated"`
	Balance           float64   `json:"balance"`
	NumberOfPayments  int       `json:"number_of_payments"`
	InstalmentAmount  float64   `json:"installment_amount"`
	PaymentFrequency  string    `json:"payment_frequency"`
	ValidFrom         time.Time `json:"valid_from,omitempty"`
	ValidTo           time.Time `json:"valid_to,omitempty"`
}

// NotificationEvent is a recent SMS/email/push touch from the
// communication DB — used for "we tried to reach them" evidence.
type NotificationEvent struct {
	Channel   string    `json:"channel"` // "sms" | "email" | "whatsapp" | "push"
	MSISDN    string    `json:"msisdn,omitempty"`
	Status    string    `json:"status,omitempty"`
	Message   string    `json:"message,omitempty"`
	InsertedAt time.Time `json:"inserted_at"`
}

// Product is one thing the customer holds — a mobile SIM, a loop CPE
// router, or a 101 device. Family is the coarse grouping the UI uses
// for per-section rendering; the specific identifier (msisdn/imei/iccid/
// serial) comes from whichever column populated it.
//
// ProductLine is the marketing-grade label resolved from rain's
// service_category taxonomy (children of parent_id 'Bi4-NgkENagt6KVe_sg'
// — i.e. "Rain Home", "Rain Work", "Rain Mobile", "Rain Loop", etc.),
// falling back to a name-pattern guess when the lookup can't match.
// ImageURL is the product shot to render in the UI.
type Product struct {
	ID               string    `json:"id"`
	Family           string    `json:"family"` // "mobile" | "loop" | "101" | "other"
	ProductLine      string    `json:"product_line,omitempty"`
	ImageURL         string    `json:"image_url,omitempty"`
	Name             string    `json:"name"`
	Category         string    `json:"category,omitempty"`
	ServiceType      string    `json:"service_type,omitempty"`
	State            string    `json:"state,omitempty"`
	StartDate        time.Time `json:"start_date,omitempty"`
	EndDate          time.Time `json:"end_date,omitempty"`
	HasStarted       bool      `json:"has_started,omitempty"`
	IsBundle         bool      `json:"is_bundle,omitempty"`
	ParentID         string    `json:"parent_id,omitempty"`
	ColourVariant    string    `json:"colour_variant,omitempty"`
	MSISDN           string    `json:"msisdn,omitempty"`
	IMEI             string    `json:"imei,omitempty"`
	ICCID            string    `json:"iccid,omitempty"`
	IMSI             string    `json:"imsi,omitempty"`
	MasterPolicy     string    `json:"master_policy,omitempty"`
	AccountNumber    string    `json:"account_number,omitempty"`
}

// UsageSnapshot is the most recent resource_policy row for one MSISDN —
// what the user is seeing on their current plan: policy name, quota,
// load ("used"), remaining.
type UsageSnapshot struct {
	MSISDN     string    `json:"msisdn"`
	IMSI       string    `json:"imsi,omitempty"`
	IMEI       string    `json:"imei,omitempty"`
	PolicyName string    `json:"policy_name"`
	Quota      string    `json:"quota"`
	Load       string    `json:"load"`
	QuotaStatus string   `json:"quota_status,omitempty"`
	ServiceName string   `json:"service_name,omitempty"`
	IPAddress   string   `json:"ip_address,omitempty"`
	State       string   `json:"state,omitempty"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
}

// DataSourceStatus tells the UI which per-DB fetch succeeded / was
// skipped / errored so we can show honest "partial load" badges instead
// of silently hiding sections.
type DataSourceStatus struct {
	Name     string `json:"name"`
	Database string `json:"database"`
	State    string `json:"state"` // "ok" | "empty" | "error" | "skipped"
	Rows     int    `json:"rows"`
	Error    string `json:"error,omitempty"`
	LatencyMS int64 `json:"latency_ms,omitempty"`
}

// Identity is the core party.individual row.
type Identity struct {
	ID         string    `json:"id"`
	FullName   string    `json:"full_name"`
	GivenName  string    `json:"given_name"`
	FamilyName string    `json:"family_name"`
	Email      string    `json:"email"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

// ContactMedium is one row from party.contact_medium. Address fields may
// all be empty strings if the row is an email- or phone-only entry.
type ContactMedium struct {
	Email        string    `json:"email"`
	Phone        string    `json:"phone"`
	StreetNumber string    `json:"street_number"`
	StreetName   string    `json:"street_name"`
	Suburb       string    `json:"suburb"`
	City         string    `json:"city"`
	Province     string    `json:"province"`
	PostalCode   string    `json:"postal_code"`
	Preferred    bool      `json:"preferred"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Payment is one row from payment.payment.
type Payment struct {
	ID          string    `json:"id"`
	Amount      float64   `json:"amount"`
	Channel     string    `json:"channel"`
	Status      string    `json:"status"`
	PaymentDate time.Time `json:"payment_date"`
}

// Subscription is a minimal view of an active service/product for the
// customer. Schema-light because Axiom's subscription table set is not
// fully documented; we return whatever we reliably find.
type Subscription struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	StartedAt time.Time `json:"started_at"`
	Price     float64   `json:"price"`
}

// Ticket is a support/trouble ticket summary.
type Ticket struct {
	ID        string    `json:"id"`
	Subject   string    `json:"subject"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// Chargeback links out to RAPIDS or to the payment table's dispute record.
type Chargeback struct {
	ID        string    `json:"id"`
	Amount    float64   `json:"amount"`
	Reason    string    `json:"reason"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// RiskScore is the derived 0..100 composite risk indicator.
type RiskScore struct {
	Value  int    `json:"value"`
	Band   string `json:"band"`   // "low" | "medium" | "high"
	Reason string `json:"reason"` // short human-readable explanation
}

// AccountAge is a pre-computed, human-friendly rendering of tenure so the
// frontend doesn't have to do the arithmetic.
type AccountAge struct {
	Days         int       `json:"days"`
	HumanFriendly string   `json:"human_friendly"`
	Since        time.Time `json:"since"`
}

// TimelineEvent is one row on the service-timeline panel. Type helps the
// frontend pick the right icon + colour.
type TimelineEvent struct {
	At   time.Time `json:"at"`
	Type string    `json:"type"`   // "created" | "payment" | "payment_failed" | "ticket_opened" | "chargeback" | "status_change"
	Label string   `json:"label"`
	Detail string  `json:"detail,omitempty"`
}

// Neighbour is another individual sharing the same street address.
type Neighbour struct {
	ID       string `json:"id"`
	FullName string `json:"full_name"`
}

// DeepLinks holds the pre-built URLs to each rain system the dashboard
// doesn't render inline. Frontend just renders three buttons.
type DeepLinks struct {
	Station string `json:"station"`
	Athena  string `json:"athena"`
}

// NotFoundError is returned by the lookup when neither phone nor email
// resolves to an individual. Callers map it to HTTP 404.
type NotFoundError struct {
	Query string
}

func (e *NotFoundError) Error() string {
	return "customer not found for " + e.Query
}
