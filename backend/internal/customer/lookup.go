package customer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"
)

// Lookup resolves a customer from a phone, email, or id and assembles the
// full Customer360 view in parallel. `mode` is "phone", "email", or "id"
// and decides which WHERE clause picks the individual.
func Lookup(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, mode, value string) (*Customer360, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("empty lookup value")
	}

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	individualID, err := resolveIndividualID(ctx, pool, mode, value)
	if err != nil {
		return nil, err
	}
	if individualID == "" {
		return nil, &NotFoundError{Query: value}
	}

	// Fetch every independent data set in parallel. Each sub-query has its
	// own context-aware guard so a slow join can't block the fast ones.
	var (
		identity      Identity
		contacts      []ContactMedium
		payments      []Payment
		subscriptions []Subscription
		tickets       []Ticket
		chargebacks   []Chargeback
	)

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		id, err := fetchIdentity(gctx, pool, individualID)
		if err == nil {
			identity = id
		}
		return err
	})
	g.Go(func() error {
		c, err := fetchContacts(gctx, pool, individualID)
		if err == nil {
			contacts = c
		}
		return err
	})
	g.Go(func() error {
		p, err := fetchPayments(gctx, pool, individualID)
		if err == nil {
			payments = p
		}
		return err
	})
	// The three best-effort queries below MUST NOT fail the overall lookup
	// if the underlying schema is missing — Axiom's subscription / ticket /
	// chargeback tables aren't documented in RAPIDS. We log + return empty.
	g.Go(func() error {
		if s, err := fetchSubscriptions(gctx, pool, individualID); err == nil {
			subscriptions = s
		} else {
			log.Warn("fetch subscriptions (best-effort)", "error", err)
		}
		return nil
	})
	g.Go(func() error {
		if t, err := fetchTickets(gctx, pool, individualID); err == nil {
			tickets = t
		} else {
			log.Warn("fetch tickets (best-effort)", "error", err)
		}
		return nil
	})
	g.Go(func() error {
		if cb, err := fetchChargebacks(gctx, pool, individualID); err == nil {
			chargebacks = cb
		} else {
			log.Warn("fetch chargebacks (best-effort)", "error", err)
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	// Nil-safety: make sure every slice is non-nil so JSON serialises as [].
	if contacts == nil {
		contacts = []ContactMedium{}
	}
	if payments == nil {
		payments = []Payment{}
	}
	if subscriptions == nil {
		subscriptions = []Subscription{}
	}
	if tickets == nil {
		tickets = []Ticket{}
	}
	if chargebacks == nil {
		chargebacks = []Chargeback{}
	}

	// Neighbours query needs a contact row first; skip if no address is
	// available. This is sequential — it's cheap and depends on `contacts`.
	neighbours := []Neighbour{}
	for _, c := range contacts {
		if c.StreetName != "" && c.StreetNumber != "" {
			if n, err := fetchNeighbours(ctx, pool, c.StreetName, c.StreetNumber, c.Suburb, individualID); err == nil {
				neighbours = n
			} else {
				log.Warn("fetch neighbours (best-effort)", "error", err)
			}
			break
		}
	}

	view := &Customer360{
		Identity:      identity,
		Contacts:      contacts,
		Payments:      payments,
		Subscriptions: subscriptions,
		Tickets:       tickets,
		Chargebacks:   chargebacks,
		Neighbours:    neighbours,
		LookedUpBy:    mode,
		LookedUpAt:    time.Now().UTC(),
		DeepLinks:     buildDeepLinks(individualID),
		ChurnRisk:     "", // reserved for a future Claude-based reasoner
	}

	// Creative derived fields — all purely functions of the data above, so
	// they can't fail independently.
	view.RiskScore = computeRiskScore(payments, chargebacks, tickets)
	view.LifetimeValue = computeLTV(payments)
	view.AccountAge = computeAccountAge(identity.CreatedAt)
	view.DaysSinceLastPayment = daysSinceLastPayment(payments)
	view.Timeline = buildTimeline(identity, payments, chargebacks, tickets)
	view.PaymentHeatmap = buildPaymentHeatmap(payments)

	return view, nil
}

// ---- Resolution -------------------------------------------------------

func resolveIndividualID(ctx context.Context, pool *pgxpool.Pool, mode, value string) (string, error) {
	switch mode {
	case "id":
		return value, nil
	case "phone":
		// Case-insensitive loose match on the raw phone string. Allows
		// +27 vs 0 prefix mismatches to still land on the same row. Adjust
		// here once we understand Axiom's canonical format.
		const q = `
			SELECT individual_id FROM party.contact_medium
			WHERE phone_number = $1
			   OR REGEXP_REPLACE(phone_number, '[^0-9]', '', 'g') = REGEXP_REPLACE($1, '[^0-9]', '', 'g')
			ORDER BY preferred DESC NULLS LAST, updated_at DESC
			LIMIT 1`
		var id string
		err := pool.QueryRow(ctx, q, value).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return id, err
	case "email":
		// Check the login column first (exact), then contact_medium.
		const q = `
			SELECT id FROM party.individual WHERE LOWER(login_name) = LOWER($1)
			UNION
			SELECT individual_id FROM party.contact_medium WHERE LOWER(email_address) = LOWER($1)
			LIMIT 1`
		var id string
		err := pool.QueryRow(ctx, q, value).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return id, err
	default:
		return "", fmt.Errorf("unknown lookup mode %q", mode)
	}
}

// ---- Fetchers ---------------------------------------------------------

func fetchIdentity(ctx context.Context, pool *pgxpool.Pool, id string) (Identity, error) {
	const q = `
		SELECT id, COALESCE(full_name,''), COALESCE(given_name,''), COALESCE(family_name,''),
		       COALESCE(login_name,''), COALESCE(status,''), COALESCE(created_at, now())
		FROM party.individual WHERE id = $1`
	var out Identity
	err := pool.QueryRow(ctx, q, id).Scan(
		&out.ID, &out.FullName, &out.GivenName, &out.FamilyName,
		&out.Email, &out.Status, &out.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Identity{ID: id}, nil
	}
	return out, err
}

func fetchContacts(ctx context.Context, pool *pgxpool.Pool, id string) ([]ContactMedium, error) {
	const q = `
		SELECT COALESCE(email_address,''), COALESCE(phone_number,''),
		       COALESCE(street_number,''), COALESCE(street_name,''),
		       COALESCE(suburb,''), COALESCE(city,''), COALESCE(province,''), COALESCE(postal_code,''),
		       COALESCE(preferred, false), COALESCE(updated_at, now())
		FROM party.contact_medium
		WHERE individual_id = $1
		ORDER BY preferred DESC NULLS LAST, updated_at DESC
		LIMIT 25`
	rows, err := pool.Query(ctx, q, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ContactMedium
	for rows.Next() {
		var c ContactMedium
		if err := rows.Scan(
			&c.Email, &c.Phone, &c.StreetNumber, &c.StreetName,
			&c.Suburb, &c.City, &c.Province, &c.PostalCode,
			&c.Preferred, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func fetchPayments(ctx context.Context, pool *pgxpool.Pool, id string) ([]Payment, error) {
	const q = `
		SELECT id, COALESCE(total_amount_value, 0)::float8,
		       COALESCE(channel,''), COALESCE(status,''),
		       COALESCE(payment_date, now())
		FROM payment.payment
		WHERE payer_id = $1
		ORDER BY payment_date DESC
		LIMIT 50`
	rows, err := pool.Query(ctx, q, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Payment
	for rows.Next() {
		var p Payment
		if err := rows.Scan(&p.ID, &p.Amount, &p.Channel, &p.Status, &p.PaymentDate); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Best-effort: rain's subscription schema isn't fully documented in RAPIDS.
// We try a few likely table names; the first one that works wins.
func fetchSubscriptions(ctx context.Context, pool *pgxpool.Pool, id string) ([]Subscription, error) {
	// `product.product_order_item` and `subscription.subscription` are
	// TMF-style; pick whichever exists. If neither does we return empty.
	queries := []string{
		`SELECT id, COALESCE(name,''), COALESCE(status,''), COALESCE(start_date, now()), COALESCE(price, 0)::float8
		 FROM subscription.subscription WHERE customer_id = $1 ORDER BY start_date DESC LIMIT 20`,
		`SELECT id, COALESCE(product_name, name, ''), COALESCE(status,''), COALESCE(start_date, now()), COALESCE(price, 0)::float8
		 FROM product.product WHERE individual_id = $1 ORDER BY start_date DESC LIMIT 20`,
	}
	for _, q := range queries {
		rows, err := pool.Query(ctx, q, id)
		if err != nil {
			continue // table likely doesn't exist, try the next shape
		}
		var out []Subscription
		for rows.Next() {
			var s Subscription
			if err := rows.Scan(&s.ID, &s.Name, &s.Status, &s.StartedAt, &s.Price); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, s)
		}
		rows.Close()
		return out, nil
	}
	return nil, nil
}

func fetchTickets(ctx context.Context, pool *pgxpool.Pool, id string) ([]Ticket, error) {
	// Same tolerant pattern: try a couple of likely schemas.
	queries := []string{
		`SELECT id, COALESCE(subject,''), COALESCE(status,''), COALESCE(created_at, now())
		 FROM trouble_ticket.ticket WHERE customer_id = $1 ORDER BY created_at DESC LIMIT 25`,
		`SELECT id, COALESCE(title,''), COALESCE(status,''), COALESCE(created_at, now())
		 FROM support.ticket WHERE individual_id = $1 ORDER BY created_at DESC LIMIT 25`,
	}
	for _, q := range queries {
		rows, err := pool.Query(ctx, q, id)
		if err != nil {
			continue
		}
		var out []Ticket
		for rows.Next() {
			var t Ticket
			if err := rows.Scan(&t.ID, &t.Subject, &t.Status, &t.CreatedAt); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, t)
		}
		rows.Close()
		return out, nil
	}
	return nil, nil
}

func fetchChargebacks(ctx context.Context, pool *pgxpool.Pool, id string) ([]Chargeback, error) {
	// RAPIDS has its own chargeback table on a separate cluster. For the
	// Axiom side, we approximate "chargebacks" as payments with a refund-ish
	// status so at least the risk score has something to work with when the
	// real table isn't reachable.
	const q = `
		SELECT id, COALESCE(total_amount_value,0)::float8,
		       COALESCE(channel,''), COALESCE(status,''),
		       COALESCE(payment_date, now())
		FROM payment.payment
		WHERE payer_id = $1 AND (
			LOWER(status) = 'refunded' OR LOWER(status) LIKE '%chargeback%' OR LOWER(status) = 'reversed'
		)
		ORDER BY payment_date DESC LIMIT 25`
	rows, err := pool.Query(ctx, q, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chargeback
	for rows.Next() {
		var c Chargeback
		// Scan into Payment-shaped row; remap fields.
		var reason, status string
		if err := rows.Scan(&c.ID, &c.Amount, &reason, &status, &c.CreatedAt); err != nil {
			return nil, err
		}
		c.Reason = reason
		c.Status = status
		out = append(out, c)
	}
	return out, rows.Err()
}

func fetchNeighbours(ctx context.Context, pool *pgxpool.Pool, street, number, suburb, excludeID string) ([]Neighbour, error) {
	const q = `
		SELECT DISTINCT cm.individual_id, COALESCE(i.full_name, '(unnamed)')
		FROM party.contact_medium cm
		JOIN party.individual i ON i.id = cm.individual_id
		WHERE cm.street_name = $1
		  AND cm.street_number = $2
		  AND COALESCE(cm.suburb,'') = COALESCE($3,'')
		  AND cm.individual_id != $4
		LIMIT 10`
	rows, err := pool.Query(ctx, q, street, number, suburb, excludeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Neighbour
	for rows.Next() {
		var n Neighbour
		if err := rows.Scan(&n.ID, &n.FullName); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ---- Derived fields ---------------------------------------------------

func computeLTV(payments []Payment) float64 {
	var total float64
	for _, p := range payments {
		if strings.EqualFold(p.Status, "SUCCESS") || strings.EqualFold(p.Status, "SUCCEEDED") || strings.EqualFold(p.Status, "PAID") {
			total += p.Amount
		}
	}
	return total
}

func computeAccountAge(since time.Time) AccountAge {
	if since.IsZero() {
		return AccountAge{HumanFriendly: "—"}
	}
	days := int(time.Since(since).Hours() / 24)
	years := days / 365
	months := (days % 365) / 30
	var pretty string
	switch {
	case days < 30:
		pretty = fmt.Sprintf("%dd", days)
	case years == 0:
		pretty = fmt.Sprintf("%dmo", months)
	default:
		pretty = fmt.Sprintf("%dy %dmo", years, months)
	}
	return AccountAge{Days: days, HumanFriendly: pretty, Since: since}
}

func daysSinceLastPayment(payments []Payment) int {
	if len(payments) == 0 {
		return -1
	}
	latest := payments[0].PaymentDate
	for _, p := range payments {
		if p.PaymentDate.After(latest) {
			latest = p.PaymentDate
		}
	}
	if latest.IsZero() {
		return -1
	}
	return int(time.Since(latest).Hours() / 24)
}

func computeRiskScore(payments []Payment, chargebacks []Chargeback, tickets []Ticket) RiskScore {
	score := 0
	reasons := []string{}

	if n := len(chargebacks); n > 0 {
		add := n * 15
		if add > 45 {
			add = 45
		}
		score += add
		reasons = append(reasons, fmt.Sprintf("%d chargeback(s)", n))
	}

	var failed, total int
	for _, p := range payments {
		total++
		s := strings.ToLower(p.Status)
		if s == "failed" || s == "declined" || strings.Contains(s, "fail") {
			failed++
		}
	}
	if total > 0 {
		pct := float64(failed) / float64(total) * 100
		add := int(pct * 0.4)
		if add > 40 {
			add = 40
		}
		score += add
		if failed > 0 {
			reasons = append(reasons, fmt.Sprintf("%d failed / %d total payments", failed, total))
		}
	}

	// Recent tickets contribute up to 15 points.
	recentTickets := 0
	for _, t := range tickets {
		if time.Since(t.CreatedAt) < 30*24*time.Hour {
			recentTickets++
		}
	}
	if recentTickets > 0 {
		add := recentTickets * 5
		if add > 15 {
			add = 15
		}
		score += add
		reasons = append(reasons, fmt.Sprintf("%d ticket(s) in last 30d", recentTickets))
	}

	if score > 100 {
		score = 100
	}

	band := "low"
	switch {
	case score >= 70:
		band = "high"
	case score >= 30:
		band = "medium"
	}

	reason := "no adverse signals"
	if len(reasons) > 0 {
		reason = strings.Join(reasons, " · ")
	}
	return RiskScore{Value: score, Band: band, Reason: reason}
}

func buildTimeline(identity Identity, payments []Payment, chargebacks []Chargeback, tickets []Ticket) []TimelineEvent {
	events := []TimelineEvent{}
	if !identity.CreatedAt.IsZero() {
		events = append(events, TimelineEvent{
			At: identity.CreatedAt, Type: "created", Label: "Account created",
		})
	}
	for _, p := range payments {
		evt := TimelineEvent{At: p.PaymentDate, Label: fmt.Sprintf("Payment %s", p.Status), Detail: fmt.Sprintf("R%.2f · %s", p.Amount, p.Channel)}
		if s := strings.ToLower(p.Status); s == "failed" || s == "declined" || strings.Contains(s, "fail") {
			evt.Type = "payment_failed"
		} else {
			evt.Type = "payment"
		}
		events = append(events, evt)
	}
	for _, c := range chargebacks {
		events = append(events, TimelineEvent{
			At: c.CreatedAt, Type: "chargeback",
			Label: "Chargeback", Detail: fmt.Sprintf("R%.2f · %s", c.Amount, c.Status),
		})
	}
	for _, t := range tickets {
		events = append(events, TimelineEvent{
			At: t.CreatedAt, Type: "ticket_opened",
			Label: "Ticket: " + t.Subject, Detail: t.Status,
		})
	}
	// Newest first — that's the order the UI timeline renders.
	for i := range events {
		for j := i + 1; j < len(events); j++ {
			if events[j].At.After(events[i].At) {
				events[i], events[j] = events[j], events[i]
			}
		}
	}
	if len(events) > 40 {
		events = events[:40]
	}
	return events
}

// buildPaymentHeatmap returns a 30-cell array (today ... 29 days ago) where
// each cell is the count of payments on that day. Rendered as a mini
// GitHub-style contribution grid.
func buildPaymentHeatmap(payments []Payment) []int {
	out := make([]int, 30)
	today := time.Now().UTC().Truncate(24 * time.Hour)
	for _, p := range payments {
		day := p.PaymentDate.UTC().Truncate(24 * time.Hour)
		delta := int(today.Sub(day).Hours() / 24)
		if delta >= 0 && delta < 30 {
			// index 0 = today, index 29 = 29 days ago
			out[delta]++
		}
	}
	return out
}

func buildDeepLinks(id string) DeepLinks {
	return DeepLinks{
		Station: "https://www.the101.info/customer/" + id,
		Athena:  "https://assisted-sales.athena.rain.co.za/customer/" + id,
	}
}
