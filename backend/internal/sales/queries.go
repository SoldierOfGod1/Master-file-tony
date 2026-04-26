package sales

// The SQL below is distilled from rain's Grafana "rainOne home" and
// "Loop" sales dashboards. Each statement keeps the exact business
// logic (test-email exclusion, staff-agent exclusion, ShoppingCart
// state filter, Rain Home category check) but parameterises the time
// window and test-email list. Generated off-template substitution
// happens only for the `%s` slots that expand the staff exclusion
// list + test email ARRAY — both are safe because they're internal
// constants, not user input.
//
// Two base CTE variants — rainOne filters on category='Rain Home',
// Loop filters on specification='LOOP_PRIMARY'. Everything else
// (channel roll-ups, revenue sums, trend buckets) is shared.

// rainOneOrderCTE is the common CTE used by every rainOne query.
// Parameters: $1 = start UTC, $2 = end UTC, $3 = test email ids (text[])
const rainOneOrderCTE = `
WITH order_summary AS (
  SELECT
    po.id                    AS order_id,
    po.order_date,
    po.channel_id,
    pof.price                AS order_value,
    BOOL_OR(pc.name = 'Rain Home')               AS is_home,
    BOOL_OR(poff.name ILIKE '%%speed 60 mbps%%')   AS has_60,
    BOOL_OR(poff.name ILIKE '%%speed unlimited%%') AS has_unlimited,
    SUM((poff.id IN ('Cb12-AiR19Orce5oXRQ','Cb12-AiUaCeUAdT_tkw'))::int) AS xtender_count,
    agent.sales_agent
  FROM product.product_order AS po
  JOIN product.product_order_item AS poi
    ON poi.product_order_id = po.id AND poi.action = 'ADD'
  JOIN product.product_offering AS poff
    ON poff.id = poi.product_offering_ref_id
  JOIN product.jt_prod_offering_prod_offering_price AS jt
    ON jt.product_offering_id = poff.id
  JOIN product.product_offering_price AS pof
    ON pof.id = jt.product_offering_price_id
   AND pof.price_type != 'upfront'
   AND pof.price > 0
   AND po.order_date BETWEEN pof.valid_for_from AND pof.valid_for_to
  JOIN product.category AS pc ON pc.id = poff.category_id
  LEFT JOIN LATERAL (
    SELECT jr2.related_party_id AS sales_agent
      FROM product.jt_prod_order_related_party AS jr2
     WHERE jr2.product_order_id = po.id
       AND jr2.related_party_id ILIKE '%%@rain%%'
     LIMIT 1
  ) AS agent ON true
  LEFT JOIN product.jt_prod_order_related_party jtrp
    ON po.id = jtrp.product_order_id AND jtrp.related_party_id NOT LIKE '%%@%%'
  LEFT JOIN public.mvw_individual i ON jtrp.related_party_id = i.id
  WHERE po.category = 'ShoppingCart'
    AND po.state NOT IN ('AWAITING_RETURN','CANCELLED_STATE')
    AND COALESCE(i.login_name,'') NOT ILIKE '%%@test.rain.co.za'
    AND (jtrp.related_party_id IS NULL OR jtrp.related_party_id <> ALL($3::text[]))
    AND po.order_date >= $1 AND po.order_date < $2
  GROUP BY po.id, po.order_date, po.channel_id, pof.price, agent.sales_agent
),
filtered_orders AS (
  SELECT * FROM order_summary
   WHERE is_home
     AND (sales_agent IS NULL OR LOWER(sales_agent) NOT IN (%s))
)
`

// loopOrderCTE is the Loop equivalent. Narrows to product_specification
// name = 'LOOP_PRIMARY' so accessories + care plans don't double-count.
const loopOrderCTE = `
WITH order_summary AS (
  SELECT
    po.id                    AS order_id,
    po.order_date,
    po.channel_id,
    p.tax_included_amount_value AS order_value,
    poff.name                AS product_offering_name,
    ps.name                  AS specification_name,
    agent.sales_agent
  FROM product.product_order po
  LEFT JOIN product.product_order_item poi ON po.id = poi.product_order_id
  LEFT JOIN product.order_price op ON poi.item_total_price_id = op.id
  LEFT JOIN product.price p ON p.id = op.price_id
  LEFT JOIN product.product_offering poff ON poff.id = poi.product_offering_ref_id
  LEFT JOIN product.product_specification ps ON ps.id = poff.product_specification_id
  LEFT JOIN LATERAL (
    SELECT jr2.related_party_id AS sales_agent
      FROM product.jt_prod_order_related_party AS jr2
     WHERE jr2.product_order_id = po.id
       AND jr2.related_party_id ILIKE '%%@rain%%'
     LIMIT 1
  ) AS agent ON true
  LEFT JOIN product.jt_prod_order_related_party jtrp
    ON po.id = jtrp.product_order_id AND jtrp.related_party_id NOT ILIKE '%%@%%'
  LEFT JOIN public.mvw_individual i ON jtrp.related_party_id = i.id
  WHERE po.category = 'ShoppingCart'
    AND po.state NOT IN ('AWAITING_RETURN','CANCELLED_STATE')
    AND ps.name = 'LOOP_PRIMARY'
    AND COALESCE(i.login_name,'') NOT ILIKE '%%@test.rain.co.za'
    AND (jtrp.related_party_id IS NULL OR jtrp.related_party_id <> ALL($3::text[]))
    AND po.order_date >= $1 AND po.order_date < $2
),
filtered_orders AS (
  SELECT * FROM order_summary
   WHERE (sales_agent IS NULL OR LOWER(sales_agent) NOT IN (%s))
)
`

// salesCountByChannelSQL returns four counters: total + per channel
// for a given [window start, end). Single round-trip, one full CTE
// evaluation, driven from whichever base CTE (rainOne or Loop) is
// spliced in at %s.
const salesCountByChannelSQL = `
%s
SELECT
  COUNT(DISTINCT order_id)                                                  AS total,
  COUNT(DISTINCT order_id) FILTER (WHERE channel_id = 'WEB')                AS web,
  COUNT(DISTINCT order_id) FILTER (WHERE channel_id = 'CALL_CENTER')        AS call_centre,
  COUNT(DISTINCT order_id) FILTER (WHERE channel_id = 'RETAIL')             AS retail
FROM filtered_orders;
`

// revenueByChannelSQL sums order_value per channel for the window.
// Amounts are ex-VAT (divide by 1.15) so the written-revenue number
// matches the Grafana reference ("Total New Sales Revenue" panel).
const revenueByChannelSQL = `
%s
SELECT
  COALESCE(SUM(order_value / 1.15), 0)                                                  AS total,
  COALESCE(SUM(order_value / 1.15) FILTER (WHERE channel_id = 'WEB'), 0)                AS web,
  COALESCE(SUM(order_value / 1.15) FILTER (WHERE channel_id = 'CALL_CENTER'), 0)        AS call_centre,
  COALESCE(SUM(order_value / 1.15) FILTER (WHERE channel_id = 'RETAIL'), 0)             AS retail
FROM (SELECT DISTINCT order_id, channel_id, order_value FROM filtered_orders) d;
`

// trendHourSQL returns one row per SAST hour (0..23) with the
// cumulative count of sales up to and including that hour for a
// single window defined by $1/$2 (UTC). The poller runs this three
// times per product (today, yesterday, last-week) and the Go side
// stitches them into a single TrendPoint slice. generate_series
// guarantees 24 rows even during quiet hours so the chart line
// doesn't drop to zero between samples.
const trendHourSQL = `
%s
, per_hour AS (
  SELECT
    EXTRACT(hour FROM (order_date AT TIME ZONE 'Africa/Johannesburg'))::int AS hr,
    COUNT(DISTINCT order_id) AS n
  FROM filtered_orders
  GROUP BY 1
),
slots AS (SELECT generate_series(0, 23) AS hr)
SELECT
  LPAD(slots.hr::text, 2, '0') || ':00' AS hour_label,
  COALESCE(SUM(per_hour.n) OVER (ORDER BY slots.hr), 0)::int AS cum_count
FROM slots
LEFT JOIN per_hour USING (hr)
ORDER BY slots.hr;
`

// mtdBudgetSQL returns the current-month target (count + revenue) from
// product.targets — the same table the Grafana MTD-vs-Budget panels
// read. Columns: home_count (daily sales count target), home_revenue
// (daily revenue target in ZAR). For v1 we SUM over the whole month
// so the comparison is month-to-date vs full-month budget.
const mtdBudgetSQL = `
SELECT
  COALESCE(SUM(home_count), 0)::int        AS budget_count,
  COALESCE(SUM(home_revenue), 0)::numeric  AS budget_revenue
FROM product.targets
WHERE date_trunc('month', target_date) = date_trunc('month', (now() AT TIME ZONE 'Africa/Johannesburg'));
`

// staffExclusionSQLFragment renders the comma-separated quoted list
// of staff emails the Grafana query hard-codes as a NOT IN clause.
// Kept as a function so the queries file stays 100% SQL text.
func staffExclusionSQLFragment() string {
	out := ""
	for i, e := range staffExclusionEmails {
		if i > 0 {
			out += ","
		}
		out += "'" + e + "'"
	}
	return out
}
