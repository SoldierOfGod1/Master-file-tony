package sales

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// staffExclusionEmails is the list of internal rain emails we exclude
// from sales counts so staff test orders don't pollute the funnel.
// Extracted verbatim from the Grafana dashboard's $axiom_test_emails
// variable. Update this list as HR/sales composition changes.
var staffExclusionEmails = []string{
	"fredotest@gmail.com",
	"luther.hlatshwayo@gmail.com",
	"elizabethvdw@gmail.com",
	"gregh+9000@gmail.com",
	"gregh+9001@gmail.com",
	"vivien@rain.co.za",
	"tania@rain.co.za",
	"andrea.cerrai@rain.co.za",
	"noluthando.biyase@rain.co.za",
	"frederick.mudiata@rain.co.za",
	"eugene.crous@rain.co.za",
	"seshni.thathiah@rain.co.za",
	"lehlohonolo.dikweni@rain.co.za",
	"abdul-malik.mohamed@rain.co.za",
	"greg.hofmeyr@rain.co.za",
	"kagisov@rain.co.za",
	"noluthando.mthini@rain.co.za",
}

// xtenderOfferingIDs are the product_offering ids that represent a
// 101 xtender add-on. Used to bucket "pro + N xtenders" SKUs.
var xtenderOfferingIDs = []string{
	"Cb12-AiR19Orce5oXRQ",
	"Cb12-AiUaCeUAdT_tkw",
}

// channelIDs enumerates the channel_id values on product.product_order.
// Grafana filters rely on these literal strings. Validation happens at
// poll time — unexpected values are logged so we can adjust the map.
type channel string

const (
	channelWeb        channel = "WEB"
	channelCallCentre channel = "CALL_CENTER"
	channelRetail     channel = "RETAIL"
)

// testEmailList returns the set of party.individual ids that are test
// accounts — login_name ending in @test.rain.co.za. The poller caches
// this once per poll cycle so we don't re-run it for every stat.
func testEmailList(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT i.id
		  FROM party.individual AS i
		 WHERE i.login_name ILIKE '%@test.rain.co.za'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		out = append(out, id)
	}
	return out, nil
}

// dayBounds computes [start, end) in UTC for a given SAST local day.
// All of rain's BSS timestamps are tz-aware; casting from SAST midnight
// gives us an unambiguous window for COUNT(*) ranges.
func dayBounds(day time.Time) (time.Time, time.Time) {
	loc, _ := time.LoadLocation("Africa/Johannesburg")
	if loc == nil {
		loc = time.UTC
	}
	local := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
	return local.UTC(), local.Add(24 * time.Hour).UTC()
}

// monthBounds returns [first-of-month, first-of-next-month) in UTC.
func monthBounds(day time.Time) (time.Time, time.Time) {
	loc, _ := time.LoadLocation("Africa/Johannesburg")
	if loc == nil {
		loc = time.UTC
	}
	y, m, _ := day.In(loc).Date()
	start := time.Date(y, m, 1, 0, 0, 0, 0, loc)
	return start.UTC(), start.AddDate(0, 1, 0).UTC()
}
