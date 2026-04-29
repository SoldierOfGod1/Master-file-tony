// Package gaussdb is the Huawei GaussDB DWS adapter for the daily-CDR
// usage source. Wire-compatible with Postgres, but kept in its own
// package because:
//
//  1. Some pg_catalog views are renamed in DWS (gs_* prefix). The
//     schema crawler runs through this package so the fallback logic
//     doesn't pollute the customer/ pool.
//  2. The fact tables are large enough that we set a heavier
//     statement_timeout and a per-msisdn result cache here that the
//     Postgres customer pool deliberately does not have.
//  3. The 4 daily-usage queries are operator-supplied and gated by a
//     PlaceholderSQL sentinel — the customer/ pool has no business
//     refusing to serve when a constant is flipped.
//
// Read-only. Writes still route through Snowflake middleware per the
// project rule.
package gaussdb

// PlaceholderSQL is the sentinel the routes check at request time. Set
// to true on first commit so the backend refuses to serve gaussdb
// usage answers built from a guess. Once the operator pastes the four
// real SQL queries below, flip this to false in the same commit that
// replaces the queries.
//
// Why a sentinel and not feature flag: feature flags get forgotten and
// route through env vars that may differ per host. PlaceholderSQL is a
// build-time guarantee that the binary cannot ship plausible-but-wrong
// numbers from a placeholder query — the only way to enable the path
// is to edit this file alongside the queries themselves. That makes
// review easy: did the same diff that flipped the sentinel also
// replace the placeholders? If yes, ship; if no, block.
const PlaceholderSQL = true

// QueryUsageSummary is the headline 30-day rollup the Customer 360
// Usage Overview tile needs. Must return a single row with these
// columns (names matter — the scanner binds by name):
//
//	total_bytes        BIGINT  — sum of bytes across the 30-day window
//	avg_daily_bytes    BIGINT  — total_bytes / NULLIF(active_days, 0)
//	active_days        INTEGER — count of days with bytes > 0
//	peak_daily_bytes   BIGINT  — max single-day byte total
//	peak_day           DATE    — date of peak_daily_bytes (nullable)
//	first_day          DATE    — first day in the window (nullable)
//	last_day           DATE    — last day in the window (nullable)
//	window_days        INTEGER — count of distinct days in window
//
// The placeholder below is intentionally invalid SQL — running it
// would fail loud at the database, not return wrong numbers. Once
// PlaceholderSQL flips to false, this constant must be a real query
// the operator validated in DBeaver / pgAdmin against gaussdb-prod.
//
// The $1 parameter is the MSISDN string (international or local —
// the operator's working query handles whichever rain stores).
const QueryUsageSummary = `
-- TODO(operator): paste the working SQL here, then flip PlaceholderSQL = false.
-- Must return a single row with columns: total_bytes, avg_daily_bytes,
-- active_days, peak_daily_bytes, peak_day, first_day, last_day, window_days.
-- Bind $1 = msisdn.
SELECT
  CAST(NULL AS BIGINT)  AS total_bytes,
  CAST(NULL AS BIGINT)  AS avg_daily_bytes,
  CAST(NULL AS INTEGER) AS active_days,
  CAST(NULL AS BIGINT)  AS peak_daily_bytes,
  CAST(NULL AS DATE)    AS peak_day,
  CAST(NULL AS DATE)    AS first_day,
  CAST(NULL AS DATE)    AS last_day,
  CAST(NULL AS INTEGER) AS window_days
WHERE 1 = 0
`

// QueryUsageSeries returns one row per day in the 30-day window.
// Must return columns:
//
//	day    DATE
//	bytes  BIGINT
//
// Driven by the trend chart below the 4-tile strip. If the operator's
// existing query already returns the series alongside the rollup,
// merge them — the scanner can read both shapes.
//
// Bind $1 = msisdn.
const QueryUsageSeries = `
-- TODO(operator): paste the per-day series SQL here.
-- Must return columns: day (DATE), bytes (BIGINT).
-- Bind $1 = msisdn.
SELECT CAST(NULL AS DATE) AS day, CAST(NULL AS BIGINT) AS bytes
WHERE 1 = 0
`

// QueryCatalogueTables walks every user table on the cluster.
// Must return columns:
//
//	schemaname  TEXT
//	tablename   TEXT
//	rowcount    BIGINT  — best-effort estimate (pg_class.reltuples is fine)
//
// pg_catalog filter excludes the system schemas. DWS-specific catalog
// quirks: if pg_class is renamed (gs_pg_class), see the fallback in
// catalogue.go — we do a 0-row probe before committing to either name.
const QueryCatalogueTables = `
SELECT n.nspname AS schemaname,
       c.relname AS tablename,
       COALESCE(c.reltuples, 0)::BIGINT AS rowcount
  FROM pg_class c
  JOIN pg_namespace n ON n.oid = c.relnamespace
 WHERE c.relkind IN ('r', 'p', 'f')
   AND n.nspname NOT IN ('pg_catalog','information_schema','pg_toast')
   AND n.nspname NOT LIKE 'pg_temp_%'
   AND n.nspname NOT LIKE 'pg_toast_temp_%'
 ORDER BY n.nspname, c.relname
`

// QueryCatalogueColumns walks every column on every user table.
// Must return columns:
//
//	schemaname  TEXT
//	tablename   TEXT
//	columnname  TEXT
//	datatype    TEXT
//	notnull     BOOLEAN
//
// Single round-trip vs per-table fan-out — the rain ClickHouse adapter
// learned this lesson the hard way (24-min hang → 5s with collapsed
// queries). Same pattern here.
const QueryCatalogueColumns = `
SELECT n.nspname AS schemaname,
       c.relname AS tablename,
       a.attname AS columnname,
       pg_catalog.format_type(a.atttypid, a.atttypmod) AS datatype,
       a.attnotnull AS notnull
  FROM pg_attribute a
  JOIN pg_class c     ON c.oid = a.attrelid
  JOIN pg_namespace n ON n.oid = c.relnamespace
 WHERE a.attnum > 0
   AND NOT a.attisdropped
   AND c.relkind IN ('r', 'p', 'f')
   AND n.nspname NOT IN ('pg_catalog','information_schema','pg_toast')
   AND n.nspname NOT LIKE 'pg_temp_%'
   AND n.nspname NOT LIKE 'pg_toast_temp_%'
 ORDER BY n.nspname, c.relname, a.attnum
`
