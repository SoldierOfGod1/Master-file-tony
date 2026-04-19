package customer

import "time"

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
	Raingo  string `json:"raingo"`
}

// NotFoundError is returned by the lookup when neither phone nor email
// resolves to an individual. Callers map it to HTTP 404.
type NotFoundError struct {
	Query string
}

func (e *NotFoundError) Error() string {
	return "customer not found for " + e.Query
}
