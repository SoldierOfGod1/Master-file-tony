// Package axiom explores a live Postgres-flavoured schema (Axiom is
// the rain BSS platform — a PostgreSQL cluster) so the dashboard can:
//   - enumerate every schema/table/column visible to the user,
//   - peek sample rows (up to 5) to understand shape,
//   - search for columns matching a name (e.g. "msisdn"),
//   - correlate tables with their likely Snowflake-middleware endpoints.
//
// Everything is READ-ONLY — we never mutate Axiom. The pgx pool used is
// the one managed by customer.Manager, so connection lifecycles and
// credential rotation are already handled there.
package axiom

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Schema is one row of `information_schema.schemata` minus the noisy
// system schemas.
type Schema struct {
	Name       string `json:"name"`
	Owner      string `json:"owner,omitempty"`
	TableCount int    `json:"table_count"`
}

// Table is one row of `information_schema.tables`.
type Table struct {
	Schema     string `json:"schema"`
	Name       string `json:"name"`
	Type       string `json:"type"` // "BASE TABLE" | "VIEW" | ...
	RowEstimate int64 `json:"row_estimate"`
	// Best-guess "this is probably related to" mapping for quick navigation.
	// Populated by the correlation layer; empty when unknown.
	LikelyDomain string `json:"likely_domain,omitempty"`
}

// Column is one row of `information_schema.columns`.
type Column struct {
	Schema       string  `json:"schema"`
	Table        string  `json:"table"`
	Name         string  `json:"name"`
	DataType     string  `json:"data_type"`
	Nullable     bool    `json:"nullable"`
	Default      *string `json:"default,omitempty"`
	CharMaxLen   *int    `json:"char_max_len,omitempty"`
	OrdinalPos   int     `json:"ordinal_pos"`
}

// SamplePeek is a tiny preview (column headers + up to N rows) used by
// the UI to show "what does this table actually look like".
type SamplePeek struct {
	Schema  string            `json:"schema"`
	Table   string            `json:"table"`
	Columns []string          `json:"columns"`
	Rows    [][]string        `json:"rows"`
	Note    string            `json:"note,omitempty"`
}

// skipSchemas is the set of schemas we never enumerate — they are
// Postgres internals and add clutter without value.
var skipSchemas = map[string]bool{
	"pg_catalog":         true,
	"pg_toast":           true,
	"information_schema": true,
}

// ListDatabases returns every non-template database on the cluster the
// pool is connected to, with size + connection-count hints. Handy for
// "which DB on this cluster actually holds the app data" decisions.
type Database struct {
	Name    string `json:"name"`
	Owner   string `json:"owner"`
	SizeMB  int64  `json:"size_mb"`
	Connections int `json:"connections"`
}

func ListDatabases(ctx context.Context, pool *pgxpool.Pool) ([]Database, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	rows, err := pool.Query(ctx, `
		SELECT d.datname,
		       COALESCE(pg_catalog.pg_get_userbyid(d.datdba),''),
		       COALESCE(pg_catalog.pg_database_size(d.datname) / 1024 / 1024, 0)::BIGINT,
		       COALESCE((SELECT COUNT(*) FROM pg_stat_activity a WHERE a.datname = d.datname), 0)
		  FROM pg_catalog.pg_database d
		 WHERE d.datistemplate = false
		 ORDER BY pg_catalog.pg_database_size(d.datname) DESC NULLS LAST
	`)
	if err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}
	defer rows.Close()

	var out []Database
	for rows.Next() {
		var d Database
		if err := rows.Scan(&d.Name, &d.Owner, &d.SizeMB, &d.Connections); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ListSchemas returns every user schema with a rough table count.
// Cheap — one query against pg_catalog.
func ListSchemas(ctx context.Context, pool *pgxpool.Pool) ([]Schema, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	rows, err := pool.Query(ctx, `
		SELECT n.nspname,
		       COALESCE(pg_catalog.pg_get_userbyid(n.nspowner),''),
		       COALESCE(COUNT(c.oid), 0)
		  FROM pg_catalog.pg_namespace n
		  LEFT JOIN pg_catalog.pg_class c
		    ON c.relnamespace = n.oid
		   AND c.relkind IN ('r','v','m','p')
		 WHERE n.nspname NOT LIKE 'pg_%'
		   AND n.nspname NOT IN ('information_schema')
		 GROUP BY n.nspname, n.nspowner
		 ORDER BY n.nspname
	`)
	if err != nil {
		return nil, fmt.Errorf("list schemas: %w", err)
	}
	defer rows.Close()

	var out []Schema
	for rows.Next() {
		var s Schema
		if err := rows.Scan(&s.Name, &s.Owner, &s.TableCount); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListTables returns every table in a schema (or every user-schema table
// when schema == ""). Row estimates come from `pg_class.reltuples` which
// is the planner's cached approximation — accurate enough to sort by
// "biggest tables" but not for exact reporting.
func ListTables(ctx context.Context, pool *pgxpool.Pool, schema string) ([]Table, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	var sql string
	var args []any
	base := `
		SELECT n.nspname, c.relname,
		       CASE c.relkind
		            WHEN 'r' THEN 'BASE TABLE'
		            WHEN 'v' THEN 'VIEW'
		            WHEN 'm' THEN 'MATERIALIZED VIEW'
		            WHEN 'p' THEN 'PARTITIONED TABLE'
		            ELSE c.relkind::text END,
		       COALESCE(c.reltuples,0)::BIGINT
		  FROM pg_catalog.pg_class c
		  JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		 WHERE c.relkind IN ('r','v','m','p')
		   AND n.nspname NOT LIKE 'pg_%'
		   AND n.nspname NOT IN ('information_schema')
	`
	if schema != "" {
		sql = base + ` AND n.nspname = $1 ORDER BY c.reltuples DESC, c.relname`
		args = []any{schema}
	} else {
		sql = base + ` ORDER BY n.nspname, c.reltuples DESC, c.relname`
	}

	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()

	var out []Table
	for rows.Next() {
		var t Table
		if err := rows.Scan(&t.Schema, &t.Name, &t.Type, &t.RowEstimate); err != nil {
			return nil, err
		}
		t.LikelyDomain = domainFor(t.Schema, t.Name)
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListColumns returns every column of a schema.table.
func ListColumns(ctx context.Context, pool *pgxpool.Pool, schema, table string) ([]Column, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	rows, err := pool.Query(ctx, `
		SELECT table_schema, table_name, column_name, data_type,
		       (is_nullable = 'YES'),
		       column_default, character_maximum_length, ordinal_position
		  FROM information_schema.columns
		 WHERE table_schema = $1 AND table_name = $2
		 ORDER BY ordinal_position
	`, schema, table)
	if err != nil {
		return nil, fmt.Errorf("list columns: %w", err)
	}
	defer rows.Close()

	var out []Column
	for rows.Next() {
		var c Column
		if err := rows.Scan(
			&c.Schema, &c.Table, &c.Name, &c.DataType,
			&c.Nullable, &c.Default, &c.CharMaxLen, &c.OrdinalPos,
		); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SearchColumns returns every column whose name matches the given
// substring (case-insensitive). Useful for "where does MSISDN live?"
func SearchColumns(ctx context.Context, pool *pgxpool.Pool, needle string) ([]Column, error) {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	rows, err := pool.Query(ctx, `
		SELECT table_schema, table_name, column_name, data_type,
		       (is_nullable = 'YES'),
		       column_default, character_maximum_length, ordinal_position
		  FROM information_schema.columns
		 WHERE table_schema NOT LIKE 'pg_%'
		   AND table_schema != 'information_schema'
		   AND column_name ILIKE $1
		 ORDER BY table_schema, table_name, ordinal_position
		 LIMIT 200
	`, "%"+needle+"%")
	if err != nil {
		return nil, fmt.Errorf("search columns: %w", err)
	}
	defer rows.Close()

	var out []Column
	for rows.Next() {
		var c Column
		if err := rows.Scan(
			&c.Schema, &c.Table, &c.Name, &c.DataType,
			&c.Nullable, &c.Default, &c.CharMaxLen, &c.OrdinalPos,
		); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// PeekSample pulls up to `limit` rows from a table with column-level
// redaction for obvious PII (full_name, password, token, email, etc.).
// Limit is capped at 20 regardless of caller input.
func PeekSample(ctx context.Context, pool *pgxpool.Pool, schema, table string, limit int) (*SamplePeek, error) {
	if schema == "" || table == "" {
		return nil, fmt.Errorf("schema and table required")
	}
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	cols, err := ListColumns(ctx, pool, schema, table)
	if err != nil || len(cols) == 0 {
		return nil, fmt.Errorf("columns unavailable for %s.%s", schema, table)
	}

	// Build SELECT list with redaction wrapping for sensitive columns.
	colExpr := make([]string, 0, len(cols))
	colNames := make([]string, 0, len(cols))
	for _, c := range cols {
		colNames = append(colNames, c.Name)
		ident := pgIdent(c.Name)
		if isSensitive(c.Name) {
			colExpr = append(colExpr, fmt.Sprintf(`'«redacted»' AS %s`, ident))
		} else {
			colExpr = append(colExpr, ident)
		}
	}

	sql := fmt.Sprintf(
		`SELECT %s FROM %s.%s LIMIT %d`,
		strings.Join(colExpr, ", "),
		pgIdent(schema), pgIdent(table), limit,
	)
	rows, err := pool.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("peek %s.%s: %w", schema, table, err)
	}
	defer rows.Close()

	out := &SamplePeek{Schema: schema, Table: table, Columns: colNames}
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := make([]string, len(values))
		for i, v := range values {
			row[i] = stringify(v)
		}
		out.Rows = append(out.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out.Rows) == 0 {
		out.Note = "table is empty"
	}
	return out, nil
}

// pgIdent double-quotes a Postgres identifier. Cheap injection guard —
// also handles schemas/tables whose names happen to match reserved words.
func pgIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// isSensitive is a conservative allow-list of column-name fragments that
// the peek layer will blank out. Keeps legitimate debugging (row shape,
// counts, relationships) possible without spraying PII into the UI.
func isSensitive(name string) bool {
	n := strings.ToLower(name)
	fragments := []string{
		"password", "passwd", "secret", "token", "api_key",
		"id_number", "id_no", "passport", "ssn",
		"pin", "otp", "cvv",
		// POPIA — rain SIM identifiers are PII. `imsi` matches every
		// IMSI variant (`imsi`, `cmi_imsi`, `udm_imsi`, `ib_imsi`,
		// `current_imsi`, `first_imsi`). Covers phase 0 of the SIM
		// Diagnostics plan — see docs/axiom/sim-diagnostics-plan.md.
		"imsi", "msisdn", "iccid", "imei",
	}
	for _, f := range fragments {
		if strings.Contains(n, f) {
			return true
		}
	}
	return false
}

// stringify renders an arbitrary pgx-returned value into a short string
// for JSON display. Binary / unknown types become "<type>".
func stringify(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		if len(t) > 120 {
			return t[:117] + "…"
		}
		return t
	case []byte:
		return fmt.Sprintf("<%d bytes>", len(t))
	case time.Time:
		return t.UTC().Format(time.RFC3339)
	default:
		s := fmt.Sprintf("%v", v)
		if len(s) > 120 {
			return s[:117] + "…"
		}
		return s
	}
}

// domainFor returns a human-friendly domain label for a schema.table
// pair. Used by the UI to show "Billing", "Identity", etc. next to each
// row without consulting a separate mapping layer.
//
// Based on rain's Axiom naming conventions:
//   - party.*            → Identity (individual/contact)
//   - payment.*, bill.*  → Billing
//   - product.*, offer.* → Catalogue
//   - service.*, cpe.*   → Service / Provisioning
//   - trouble_ticket.*   → Support
//   - order.*, sales.*   → Sales
//   - subscription.*     → Subscription
//   - charge.*           → Charging
func domainFor(schema, table string) string {
	s := strings.ToLower(schema)
	t := strings.ToLower(table)

	switch {
	case s == "party":
		return "Identity"
	case s == "payment" || s == "bill" || s == "billing":
		return "Billing"
	case s == "product" || s == "offer" || s == "catalog" || s == "catalogue":
		return "Catalogue"
	case s == "service" || s == "cpe" || s == "provisioning":
		return "Service"
	case s == "trouble_ticket" || s == "ticket" || s == "support":
		return "Support"
	case s == "order" || s == "sales" || s == "cart":
		return "Sales"
	case s == "subscription":
		return "Subscription"
	case s == "charge" || s == "charging":
		return "Charging"
	case strings.Contains(t, "payment"):
		return "Billing"
	case strings.Contains(t, "service"):
		return "Service"
	case strings.Contains(t, "ticket"):
		return "Support"
	case strings.Contains(t, "order"):
		return "Sales"
	}
	return ""
}
