package server

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/SoldierOfGod1/command-centre/internal/axiom"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RegisterAxiomRoutes exposes read-only Axiom schema discovery endpoints
// plus the Snowflake-middleware-to-Axiom correlation catalogue.
// Everything here is READ-ONLY.
func RegisterAxiomRoutes(mux *http.ServeMux, api *API) {
	mux.HandleFunc("GET /api/v1/axiom/databases", api.handleAxiomDatabases)
	mux.HandleFunc("GET /api/v1/axiom/schemas", api.handleAxiomSchemas)
	mux.HandleFunc("GET /api/v1/axiom/tables", api.handleAxiomTables)
	mux.HandleFunc("GET /api/v1/axiom/columns", api.handleAxiomColumns)
	mux.HandleFunc("GET /api/v1/axiom/search", api.handleAxiomSearch)
	mux.HandleFunc("GET /api/v1/axiom/peek", api.handleAxiomPeek)
	mux.HandleFunc("GET /api/v1/axiom/endpoint-map", api.handleAxiomEndpointMap)
	mux.HandleFunc("GET /api/v1/axiom/count", api.handleAxiomCount)
	mux.HandleFunc("GET /api/v1/axiom/filter", api.handleAxiomFilter)
}

// handleAxiomFilter returns up to 20 rows matching a parameterised
// WHERE <column> = <value>. Same identifier-safety rules as /count.
// Useful for read-only debugging ("show me this customer's products").
func (a *API) handleAxiomFilter(w http.ResponseWriter, r *http.Request) {
	pool := a.resolveAxiomPool(w, r)
	if pool == nil {
		return
	}
	q := r.URL.Query()
	schema := q.Get("schema")
	table := q.Get("table")
	column := q.Get("column")
	value := q.Get("value")
	limit := 20
	if v := q.Get("limit"); v != "" {
		var n int
		fmt.Sscanf(v, "%d", &n)
		if n > 0 && n <= 50 {
			limit = n
		}
	}
	if schema == "" || table == "" || column == "" {
		jsonError(w, 400, "schema, table, column required")
		return
	}
	if !isSafeIdent(schema) || !isSafeIdent(table) || !isSafeIdent(column) {
		jsonError(w, 400, "identifiers must be alphanumeric or underscore")
		return
	}
	sql := fmt.Sprintf("SELECT * FROM %s.%s WHERE %s = $1 LIMIT %d",
		schema, table, column, limit)
	rows, err := pool.Query(r.Context(), sql, value)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	fields := rows.FieldDescriptions()
	cols := make([]string, len(fields))
	for i, f := range fields {
		cols[i] = string(f.Name)
	}
	var out [][]string
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			continue
		}
		row := make([]string, len(vals))
		for i, v := range vals {
			row[i] = fmt.Sprintf("%v", v)
			if len(row[i]) > 120 {
				row[i] = row[i][:117] + "…"
			}
		}
		out = append(out, row)
	}
	jsonOK(w, map[string]any{
		"schema": schema, "table": table, "column": column, "value": value,
		"columns": cols, "rows": out,
	})
}

// handleAxiomCount runs `SELECT COUNT(*) FROM <schema>.<table>
// WHERE <column> = $1` using a parameterised value. Read-only; the
// column/table names are validated as identifiers (alphanumeric +
// underscore only) so this can't be turned into an injection surface.
func (a *API) handleAxiomCount(w http.ResponseWriter, r *http.Request) {
	pool := a.resolveAxiomPool(w, r)
	if pool == nil {
		return
	}
	q := r.URL.Query()
	schema := q.Get("schema")
	table := q.Get("table")
	column := q.Get("column")
	value := q.Get("value")
	if schema == "" || table == "" || column == "" {
		jsonError(w, 400, "schema, table, column required")
		return
	}
	if !isSafeIdent(schema) || !isSafeIdent(table) || !isSafeIdent(column) {
		jsonError(w, 400, "identifiers must be alphanumeric or underscore")
		return
	}
	sql := "SELECT COUNT(*) FROM " + schema + "." + table + " WHERE " + column + " = $1"
	var n int
	if err := pool.QueryRow(r.Context(), sql, value).Scan(&n); err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonOK(w, map[string]any{
		"schema": schema, "table": table, "column": column,
		"value": value, "count": n,
	})
}

// isSafeIdent enforces that a SQL identifier is alphanumeric or
// underscore so it can be concatenated into a query without injection.
func isSafeIdent(s string) bool {
	if s == "" || len(s) > 63 {
		return false
	}
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_'
		if !ok {
			return false
		}
	}
	return true
}

// resolveAxiomPool returns a pgxpool for the `conn` query param (default
// = primary), with optional `db` override for multi-DB clusters.
// Writes an HTTP error when no pool is reachable.
func (a *API) resolveAxiomPool(w http.ResponseWriter, r *http.Request) *pgxpool.Pool {
	if a.CustomerMgr == nil {
		jsonError(w, http.StatusServiceUnavailable, "customer manager not initialised")
		return nil
	}
	ctx := r.Context()
	connID := r.URL.Query().Get("conn")
	db := r.URL.Query().Get("db")

	if connID == "" {
		// Resolve primary first so we know which connection id to clone
		// when a db override is present.
		pool, conn, err := a.CustomerMgr.PrimaryPool(ctx)
		if err != nil {
			jsonError(w, http.StatusServiceUnavailable, "axiom primary: "+err.Error())
			return nil
		}
		if db == "" || db == conn.Database {
			return pool
		}
		connID = conn.ID
	}
	pool, _, err := a.CustomerMgr.PoolByIDWithDB(ctx, connID, db)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "axiom conn "+connID+" db="+db+": "+err.Error())
		return nil
	}
	return pool
}

func (a *API) handleAxiomDatabases(w http.ResponseWriter, r *http.Request) {
	pool := a.resolveAxiomPool(w, r)
	if pool == nil {
		return
	}
	dbs, err := axiom.ListDatabases(r.Context(), pool)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, dbs)
}

func (a *API) handleAxiomSchemas(w http.ResponseWriter, r *http.Request) {
	pool := a.resolveAxiomPool(w, r)
	if pool == nil {
		return
	}
	schemas, err := axiom.ListSchemas(r.Context(), pool)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, schemas)
}

func (a *API) handleAxiomTables(w http.ResponseWriter, r *http.Request) {
	pool := a.resolveAxiomPool(w, r)
	if pool == nil {
		return
	}
	schema := r.URL.Query().Get("schema")
	tables, err := axiom.ListTables(r.Context(), pool, schema)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, tables)
}

func (a *API) handleAxiomColumns(w http.ResponseWriter, r *http.Request) {
	pool := a.resolveAxiomPool(w, r)
	if pool == nil {
		return
	}
	schema := r.URL.Query().Get("schema")
	table := r.URL.Query().Get("table")
	if schema == "" || table == "" {
		jsonError(w, http.StatusBadRequest, "schema and table query params required")
		return
	}
	cols, err := axiom.ListColumns(r.Context(), pool, schema, table)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, cols)
}

func (a *API) handleAxiomSearch(w http.ResponseWriter, r *http.Request) {
	pool := a.resolveAxiomPool(w, r)
	if pool == nil {
		return
	}
	needle := r.URL.Query().Get("q")
	if needle == "" {
		jsonError(w, http.StatusBadRequest, "q query param required")
		return
	}
	hits, err := axiom.SearchColumns(r.Context(), pool, needle)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, hits)
}

func (a *API) handleAxiomPeek(w http.ResponseWriter, r *http.Request) {
	pool := a.resolveAxiomPool(w, r)
	if pool == nil {
		return
	}
	schema := r.URL.Query().Get("schema")
	table := r.URL.Query().Get("table")
	limit := 5
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	peek, err := axiom.PeekSample(r.Context(), pool, schema, table, limit)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, peek)
}

// handleAxiomEndpointMap returns the hand-curated Snowflake-middleware
// endpoint → Axiom table map. No DB call; pure static data.
func (a *API) handleAxiomEndpointMap(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, axiom.DefaultEndpointMap)
}
