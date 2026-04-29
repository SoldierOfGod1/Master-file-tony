package chat

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Tool is one entry in the agent's callable surface. Phase A2 of
// the agent-orchestrator plan: the agent loop in Phase A3 picks
// from this catalogue at every step. The shape mirrors Anthropic's
// Messages API tool definition so the catalogue can ship verbatim
// in the system prompt for tool-use.
//
// The Run callback is the one piece that doesn't ship to the
// model — it's the local handler that translates a tool-use block
// into the equivalent backend HTTP call (or in-process Go call).
//
// The handler exists so Phase A3's loop can execute tools without
// needing the Go server's full ServeMux in scope. In tests, swap
// in a mock Run.
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]any         `json:"input_schema"`
	Run         func(context.Context, json.RawMessage) (any, error) `json:"-"`
	// Write tools route through Phase B2's approval gate. Read
	// tools execute immediately. Default is read; mark write
	// explicitly when the tool mutates server state.
	Write bool `json:"-"`
}

// ToolCatalogue is the registry of available tools. Constructed
// once at server start with the API base URL injected so handlers
// can call back through the existing routes (this keeps every
// tool's auth + audit trail going through the same pipeline as
// the UI). Tools that need direct DB access can side-step but
// most should not.
//
// memDB is optional and only used by the Phase D1 `remember` tool.
// Pass nil and that tool gets a no-op handler that errors clearly.
type ToolCatalogue struct {
	tools   []Tool
	baseURL string
	client  *http.Client
	memDB   *sql.DB
	// userID is set per-call by the agent loop just before
	// invoking a tool — Phase D1's remember tool needs to know
	// which user_id to attribute the memory to. The agent loop
	// already gates Write tools on UserID != "".
	pendingUserID string
}

// NewToolCatalogue builds the standard 11-tool catalogue (10
// existing endpoints plus Phase D1's remember) against the given
// base URL (typically http://127.0.0.1:8080/api/v1). memDB enables
// the remember tool — pass nil to disable D1 cleanly.
func NewToolCatalogue(baseURL string) *ToolCatalogue {
	return NewToolCatalogueWithDB(baseURL, nil)
}

func NewToolCatalogueWithDB(baseURL string, memDB *sql.DB) *ToolCatalogue {
	c := &ToolCatalogue{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 60 * time.Second},
		memDB:   memDB,
	}
	c.register()
	return c
}

// SetUserContext lets the agent loop tell the catalogue who the
// current user is, just before issuing a tool call. The remember
// tool reads from this field. Other tools ignore it (HTTP routes
// already handle user context server-side).
//
// Not thread-safe — by design. Tool calls within one Run loop are
// serialised; concurrent Run loops use separate ToolCatalogue
// instances or accept the last-writer-wins behaviour. Production
// agent runs are one user at a time per process.
func (c *ToolCatalogue) SetUserContext(userID string) {
	c.pendingUserID = userID
}

// All returns the catalogue as a slice safe for serialisation
// into the model's system prompt. Run callbacks are kept on the
// returned values for the agent loop to invoke.
func (c *ToolCatalogue) All() []Tool { return c.tools }

// Find resolves a tool by name. Returns nil if missing.
func (c *ToolCatalogue) Find(name string) *Tool {
	for i := range c.tools {
		if c.tools[i].Name == name {
			return &c.tools[i]
		}
	}
	return nil
}

// Schema for the model: each tool serialised without the Run
// callback. This is what gets sent in the Messages API request.
func (c *ToolCatalogue) Schema() []map[string]any {
	out := make([]map[string]any, 0, len(c.tools))
	for _, t := range c.tools {
		out = append(out, map[string]any{
			"name":         t.Name,
			"description":  t.Description,
			"input_schema": t.InputSchema,
		})
	}
	return out
}

// register defines the 10 highest-frequency endpoints the agent
// needs. Order matters loosely: the model picks based on
// description, but putting the most common one first reduces
// average latency on long catalogues.
func (c *ToolCatalogue) register() {
	c.tools = []Tool{
		{
			Name:        "customer_360",
			Description: "Look up the full Customer 360 view by email, phone, or party id. Returns identity, billing accounts, payments, products, IMSIs (if cascade resolves), risk score, recent invoices, balances, and timeline. Use this whenever you need to know anything about a specific customer.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"mode":  map[string]any{"type": "string", "enum": []string{"phone", "email", "id"}, "description": "Which identifier the value is."},
					"value": map[string]any{"type": "string", "description": "The phone, email, or party id."},
				},
				"required": []string{"mode", "value"},
			},
			Run: c.runGet("customer", []string{"mode", "value"}),
		},
		{
			Name:        "axiom_peek",
			Description: "Fetch up to 20 redacted sample rows from one Axiom table. PII columns (imsi/msisdn/iccid/imei/password/etc.) come back as «redacted». Use to inspect schema or data shape — never for bulk export.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"db":     map[string]any{"type": "string", "description": "Database name, e.g. resource, account, payment."},
					"schema": map[string]any{"type": "string"},
					"table":  map[string]any{"type": "string"},
					"limit":  map[string]any{"type": "integer", "description": "1-20, defaults to 5."},
				},
				"required": []string{"db", "schema", "table"},
			},
			Run: c.runGet("axiom/peek", []string{"db", "schema", "table", "limit"}),
		},
		{
			Name:        "axiom_search_columns",
			Description: "Search the Axiom catalogue for columns whose name contains the query (case-insensitive). Use when you don't know which table holds a piece of data — e.g. 'where does msisdn live'.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"q":  map[string]any{"type": "string"},
					"db": map[string]any{"type": "string", "description": "Optional — narrows to one database."},
				},
				"required": []string{"q"},
			},
			Run: c.runGet("axiom/search", []string{"q", "db"}),
		},
		{
			Name:        "platform_health",
			Description: "Current health of every monitored target — Axiom, GaussDB, satellite apps. Returns latency, uptime rollups, last failure. Use to answer 'is X up?' or 'any incidents?'.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Run: c.runGet("platforms/services", nil),
		},
		{
			Name:        "platform_alerts",
			Description: "List recent alerts (default last 24h, max 100). Severity, kind, service, message, state.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"since_hours": map[string]any{"type": "integer", "description": "Look-back window in hours, default 24, max 168."},
				},
			},
			Run: c.runGet("platforms/alerts", []string{"since_hours"}),
		},
		{
			Name:        "incident_list",
			Description: "List open or recent incidents on the platform. Each one carries timeline, severity, mitigation status.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"state": map[string]any{"type": "string", "enum": []string{"open", "investigating", "mitigated", "resolved"}, "description": "Optional filter."},
				},
			},
			Run: c.runGet("platforms/incidents", []string{"state"}),
		},
		{
			Name:        "imsi_audit_search",
			Description: "Search the imsi_lookup_audit log for past resolveIMSIs calls — by individual_id, source, winning_phase, or time range. Useful for forensics ('did we look up X yesterday') and for cascade-drift inspection.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"individual_id":  map[string]any{"type": "string"},
					"winning_phase":  map[string]any{"type": "string"},
					"since_hours":    map[string]any{"type": "integer"},
					"limit":          map[string]any{"type": "integer", "description": "Max rows, default 50, hard cap 500."},
				},
			},
			Run: c.runGet("customer/imsi-audit", []string{"individual_id", "winning_phase", "since_hours", "limit"}),
		},
		{
			Name:        "imsi_override_get",
			Description: "Read the manually-configured IMSI override list for a customer. Returns empty array if none set.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"customer_id": map[string]any{"type": "string"},
				},
				"required": []string{"customer_id"},
			},
			Run: c.runGetPath("customer/{customer_id}/imsi-override", []string{"customer_id"}),
		},
		{
			Name:        "imsi_override_set",
			Description: "WRITE. Replace the IMSI override list for a customer. Requires RAIN_SUPPORT_L2=true on the server. Every write produces an audit row. Use sparingly; prefer fixing the cascade where possible.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"customer_id": map[string]any{"type": "string"},
					"imsis":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
				"required": []string{"customer_id", "imsis"},
			},
			Write: true,
			Run:   c.runPutPath("customer/{customer_id}/imsi-override", []string{"customer_id"}, []string{"imsis"}),
		},
		{
			Name:        "approval_create",
			Description: "WRITE. Create an Approval row for a destructive or high-impact action. The agent uses this to gate any write tool that's not in the auto-allow list. The agent loop injects the requester (current user_id) automatically; do not set it yourself.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":   map[string]any{"type": "string"},
					"summary": map[string]any{"type": "string"},
					"context": map[string]any{"type": "object", "description": "Free-form metadata."},
				},
				"required": []string{"title"},
			},
			Write: true,
			// requester is injected by the agent loop's Phase B1
			// hook (see injectRequester in agent_loop.go); listed
			// here so runPostJSON forwards it through.
			Run: c.runPostJSON("approvals", []string{"title", "summary", "context", "requester"}),
		},
		{
			Name: "remember",
			Description: "Persist one short observation to your cross-session memory for the current user. " +
				"Use sparingly — only durable preferences, incident findings, or recurring patterns. Do NOT use " +
				"for transient session details. Bodies > 2KB are truncated. Returns the persisted entry id. " +
				"Memory entries are NOT shared across users.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kind": map[string]any{
						"type": "string",
						"enum": []string{"preference", "incident_context", "pattern", "note"},
						"description": "Memory category. preference = user habit, incident_context = current operational context, pattern = observed behaviour, note = anything else.",
					},
					"body": map[string]any{
						"type":        "string",
						"description": "One sentence ideally. Max 2KB.",
					},
				},
				"required": []string{"body"},
			},
			// Not Write=true — agent_memory is local per-user
			// state, not a destructive ops mutation. The
			// approval gate would block useful learning.
			Run: c.runRemember(),
		},
		{
			// Cybertron's read-only window into Dark NOC. Backend
			// gates the underlying /api/v1/darknoc/* endpoints by
			// DARK_NOC_ENABLED, so this tool degrades gracefully on
			// SIT installs (operator just gets the "disabled" note).
			Name: "darknoc_overview",
			Description: "Cybertron / Dark NOC. Returns the current network HUD overview: " +
				"24h fault counts, critical-fault counts, slice rollup, network trust score (0-100), " +
				"and which data source produced it. Use for 'how's the network right now' questions.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Run: c.runGet("darknoc/overview", nil),
		},
		{
			Name: "darknoc_faults",
			Description: "Cybertron / Dark NOC. Lists the latest 50 faults from the last 24h: " +
				"timestamp, severity, source service, region, technology (5G/4G/FWA/Loop), title, detail. " +
				"Use to answer 'what just broke' or 'show me 5G CRITICAL alerts'.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Run: c.runGet("darknoc/faults", nil),
		},
		{
			Name: "darknoc_registry",
			Description: "Cybertron / Dark NOC. Returns the static reference list of 41 telecom-AI " +
				"agents from the Capgemini Open Registry (RCA Master, SLA Guardian, Self-Healing Trigger " +
				"Manager, etc.). Useful for 'is there an agent that does X' research, but the agents are " +
				"NOT live at rain — this is a roadmap reference, not an inventory.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Run: c.runGet("darknoc/registry", nil),
		},
		{
			Name: "clickhouse_schema",
			Description: "Return the live ClickHouse schema (databases / tables / columns) the rain " +
				"telemetry cluster currently exposes. Use this BEFORE composing any SQL query against " +
				"ClickHouse so you don't hallucinate table or column names. Cached server-side for 10 " +
				"minutes; cheap to call repeatedly.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Run: c.runGet("darknoc/catalogue", nil),
		},
		{
			Name: "axiom_daily_usage",
			Description: "Fetch the raw daily CDR usage series for a MSISDN from the rain Axiom HTTP " +
				"API. Returns 30-day window with date[] + actualUsage{} + events{}. Use this when you " +
				"need the per-day breakdown; for headline numbers prefer axiom_usage_summary which " +
				"pre-computes total/avg/active-days/peak.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"msisdn": map[string]any{
						"type":        "string",
						"description": "Subscriber MSISDN. International format without leading +.",
					},
				},
				"required": []string{"msisdn"},
			},
			Run: c.runGet("customer/usage/daily", []string{"msisdn"}),
		},
		{
			Name: "axiom_usage_summary",
			Description: "Headline data-usage rollup for one MSISDN over the last 30 days: total bytes, " +
				"avg bytes per active day, active-days count, peak day + peak bytes, plus the full " +
				"per-day series. This is the same data the Customer 360 'Usage Overview' tile shows.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"msisdn": map[string]any{
						"type":        "string",
						"description": "Subscriber MSISDN. International format without leading +.",
					},
				},
				"required": []string{"msisdn"},
			},
			Run: c.runGet("customer/usage/summary", []string{"msisdn"}),
		},
	}
}

// runRemember writes a memory entry tied to the current user.
// Phase D1: the agent self-remembers via this tool. Refuses on
// nil memDB or empty user context — both of which are operator
// configuration errors and should be visible to the agent.
func (c *ToolCatalogue) runRemember() func(context.Context, json.RawMessage) (any, error) {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		if c.memDB == nil {
			return nil, fmt.Errorf("remember disabled: agent_memory store not wired")
		}
		args, err := decodeArgs(raw)
		if err != nil {
			return nil, err
		}
		body, _ := args["body"].(string)
		kind, _ := args["kind"].(string)
		userID := c.pendingUserID
		id, err := WriteMemory(c.memDB, userID, kind, body)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"id":      id,
			"kind":    kind,
			"user_id": userID,
			"saved":   true,
		}, nil
	}
}

// ── HTTP plumbing ──────────────────────────────────────────────

// runGet returns a Run handler that maps named query params from
// the model's input JSON into a GET URL on the catalogue's base.
// Missing params are silently dropped — the receiving endpoint
// validates required ones.
func (c *ToolCatalogue) runGet(path string, params []string) func(context.Context, json.RawMessage) (any, error) {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		args, err := decodeArgs(raw)
		if err != nil {
			return nil, err
		}
		u := c.baseURL + "/" + path
		if len(params) > 0 {
			q := url.Values{}
			for _, p := range params {
				if v, ok := args[p]; ok && v != nil {
					q.Set(p, fmt.Sprintf("%v", v))
				}
			}
			if encoded := q.Encode(); encoded != "" {
				u += "?" + encoded
			}
		}
		return c.do(ctx, http.MethodGet, u, nil)
	}
}

// runGetPath substitutes path-template segments like
// /customer/{customer_id} from the input args before issuing the GET.
func (c *ToolCatalogue) runGetPath(template string, pathParams []string) func(context.Context, json.RawMessage) (any, error) {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		args, err := decodeArgs(raw)
		if err != nil {
			return nil, err
		}
		path := template
		for _, p := range pathParams {
			v, ok := args[p]
			if !ok {
				return nil, fmt.Errorf("missing required path arg %q", p)
			}
			path = stringReplace(path, "{"+p+"}", fmt.Sprintf("%v", v))
		}
		return c.do(ctx, http.MethodGet, c.baseURL+"/"+path, nil)
	}
}

// runPutPath substitutes path-template segments and PUTs the
// remaining args (those listed in bodyParams) as JSON.
func (c *ToolCatalogue) runPutPath(template string, pathParams, bodyParams []string) func(context.Context, json.RawMessage) (any, error) {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		args, err := decodeArgs(raw)
		if err != nil {
			return nil, err
		}
		path := template
		for _, p := range pathParams {
			v, ok := args[p]
			if !ok {
				return nil, fmt.Errorf("missing required path arg %q", p)
			}
			path = stringReplace(path, "{"+p+"}", fmt.Sprintf("%v", v))
		}
		body := map[string]any{}
		for _, p := range bodyParams {
			if v, ok := args[p]; ok {
				body[p] = v
			}
		}
		return c.do(ctx, http.MethodPut, c.baseURL+"/"+path, body)
	}
}

// runPostJSON POSTs the named fields as a JSON body to the path.
func (c *ToolCatalogue) runPostJSON(path string, fields []string) func(context.Context, json.RawMessage) (any, error) {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		args, err := decodeArgs(raw)
		if err != nil {
			return nil, err
		}
		body := map[string]any{}
		for _, f := range fields {
			if v, ok := args[f]; ok {
				body[f] = v
			}
		}
		return c.do(ctx, http.MethodPost, c.baseURL+"/"+path, body)
	}
}

func (c *ToolCatalogue) do(ctx context.Context, method, u string, body any) (any, error) {
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, reqBody)
	if err != nil {
		return nil, err
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var decoded any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil && err != io.EOF {
		return nil, fmt.Errorf("tool response decode: %w", err)
	}
	if resp.StatusCode >= 400 {
		return decoded, fmt.Errorf("tool call returned HTTP %d", resp.StatusCode)
	}
	return decoded, nil
}

// decodeArgs parses the model's tool input into a string-keyed
// map. The Anthropic Messages API delivers tool inputs as raw
// JSON objects so we keep flexibility on the value type.
func decodeArgs(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("tool args parse: %w", err)
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

// stringReplace is strings.Replace(...,-1) inline so we don't pull
// strings into this file just for one call site.
func stringReplace(s, old, new string) string {
	for {
		i := indexOf(s, old)
		if i < 0 {
			return s
		}
		s = s[:i] + new + s[i+len(old):]
	}
}

func indexOf(s, sub string) int {
	if len(sub) == 0 {
		return 0
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

