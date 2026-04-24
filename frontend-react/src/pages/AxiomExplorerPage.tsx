/* ============================================================
   Axiom Explorer — read-only schema browser + Snowflake middleware
   correlation map. Four tabs:
     1. Schemas — every user schema with a table count
     2. Tables  — list tables in a schema, sample 5 rows on click
     3. Search  — find any column by name across all schemas
     4. Map     — Snowflake Middleware endpoint → tables it touches
   ============================================================ */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { Database, Columns3, Search, GitCompare, Eye } from 'lucide-react';
import HudPanel from '../components/shared/HudPanel';
import HudSummaryStrip from '../components/shared/HudSummaryStrip';
import { HudChip, HudStatusLed } from '../components/shared/HudChip';
import hudStyles from '../theme/hud.module.css';
import {
  listDatabases, listSchemas, listTables, listColumns, searchColumns, peekTable, listEndpointMap,
  type AxiomDatabase, type AxiomSchema, type AxiomTable, type AxiomColumn, type AxiomPeek, type AxiomEndpointMap,
} from '../api/axiom';

type DBCtx = { db: string; setDB: (v: string) => void; dbs: AxiomDatabase[] };
const DB_STORAGE_KEY = 'axiomExplorerDB';

type Tab = 'schemas' | 'tables' | 'search' | 'map';

const DOMAIN_COLOR: Record<string, string> = {
  Identity: '#6ff2a0',
  Billing: '#ffaa00',
  Service: '#00f0ff',
  Catalogue: '#c488ff',
  Sales: '#ff7de0',
  Support: '#ff7b7b',
  Subscription: '#7cc6ff',
  Charging: '#ffe08a',
};
const domainColor = (d?: string) => (d && DOMAIN_COLOR[d]) || '#7cc6ff';

const METHOD_COLOR: Record<string, string> = {
  GET: '#6ff2a0',
  POST: '#00f0ff',
  PATCH: '#ffaa00',
  PUT: '#ffaa00',
  DELETE: '#ff7b7b',
};

// Phase 0a — the backend redacts PII columns (imsi/msisdn/iccid/imei/password/
// token/passport/pin/otp/cvv/...) in PeekSample by replacing the value with the
// literal string '«redacted»'. Render that as a violet "PII" chip with a
// tooltip so ops know it's policy, not a broken query. See
// docs/axiom/sim-diagnostics-plan.md design finding D10.
const REDACTED_LITERAL = '«redacted»';

function PeekCell({ value }: { readonly value: string }) {
  if (value === REDACTED_LITERAL) {
    return (
      <span
        title="Redacted under POPIA. Unredact requires support-l2 role (coming)."
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: 4,
          padding: '1px 6px',
          fontSize: 9,
          letterSpacing: '0.08em',
          textTransform: 'uppercase',
          color: '#b980ff',
          background: 'rgba(185, 128, 255, 0.08)',
          border: '1px solid rgba(185, 128, 255, 0.35)',
          borderRadius: 3,
          fontFamily: 'var(--font-mono, monospace)',
        }}
      >
        <span aria-hidden="true">🔒</span>
        <span>PII redacted</span>
      </span>
    );
  }
  if (!value) {
    return <span style={{ opacity: 0.3 }}>—</span>;
  }
  return <>{value}</>;
}

export default function AxiomExplorerPage() {
  const [tab, setTab] = useState<Tab>('schemas');
  const [dbs, setDBs] = useState<AxiomDatabase[]>([]);
  const [db, setDBState] = useState<string>(() => localStorage.getItem(DB_STORAGE_KEY) ?? '');

  const setDB = useCallback((v: string) => {
    setDBState(v);
    if (v) localStorage.setItem(DB_STORAGE_KEY, v);
    else localStorage.removeItem(DB_STORAGE_KEY);
  }, []);

  useEffect(() => { listDatabases().then(setDBs).catch(() => void 0); }, []);

  const ctx: DBCtx = { db, setDB, dbs };

  return (
    <div className={hudStyles.page}>
      <HudSummaryStrip
        title="Axiom Explorer"
        subtitle={`Read-only schema discovery · ${db ? `db=${db}` : 'primary db'}`}
        gaugeValue={1}
        gaugeReadout="LIVE"
        gaugeLabel="AXIOM"
        gaugeColor="#00f0ff"
        extra={
          <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', alignItems: 'center' }}>
            <select
              value={db}
              onChange={(e) => setDB(e.target.value)}
              title="Pick which database on the cluster to explore"
              style={{
                ...selectStyle,
                width: 'auto',
                minWidth: 160,
                padding: '3px 6px',
                fontSize: 10,
              }}
            >
              <option value="">// primary db</option>
              {dbs.map((d) => (
                <option key={d.name} value={d.name}>
                  {d.name} ({formatSize(d.size_mb)})
                </option>
              ))}
            </select>
            {(['schemas', 'tables', 'search', 'map'] as const).map((t) => (
              <button
                key={t}
                type="button"
                onClick={() => setTab(t)}
                style={tabButtonStyle(tab === t)}
              >
                {iconFor(t)} {labelFor(t)}
              </button>
            ))}
          </div>
        }
      />

      {tab === 'schemas' && <SchemasTab ctx={ctx} />}
      {tab === 'tables' && <TablesTab ctx={ctx} />}
      {tab === 'search' && <SearchTab ctx={ctx} />}
      {tab === 'map' && <EndpointMapTab />}
    </div>
  );
}

function formatSize(mb: number): string {
  if (mb >= 1024 * 1024) return `${(mb / 1024 / 1024).toFixed(1)}TB`;
  if (mb >= 1024) return `${(mb / 1024).toFixed(1)}GB`;
  return `${mb}MB`;
}

function iconFor(t: Tab) {
  switch (t) {
    case 'schemas': return <Database size={11} />;
    case 'tables':  return <Columns3 size={11} />;
    case 'search':  return <Search size={11} />;
    case 'map':     return <GitCompare size={11} />;
  }
}
function labelFor(t: Tab) {
  return t === 'schemas' ? 'Schemas' : t === 'tables' ? 'Tables' : t === 'search' ? 'Search' : 'Endpoint Map';
}

function tabButtonStyle(active: boolean): React.CSSProperties {
  return {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 4,
    padding: '3px 10px',
    fontSize: 10,
    textTransform: 'uppercase',
    letterSpacing: '0.08em',
    color: active ? '#0a0c12' : 'var(--ink, #e6f6ff)',
    background: active ? '#00f0ff' : 'transparent',
    border: `1px solid #00f0ff55`,
    borderRadius: 4,
    cursor: 'pointer',
    fontFamily: 'inherit',
  };
}

/* ── Schemas tab ─────────────────────────────────────────────── */

function SchemasTab({ ctx }: { readonly ctx: DBCtx }) {
  const [rows, setRows] = useState<AxiomSchema[]>([]);
  const [err, setErr] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    setLoading(true);
    listSchemas(undefined, ctx.db || undefined)
      .then((r) => { setRows(r); setErr(null); })
      .catch((e) => setErr(e instanceof Error ? e.message : 'load failed'))
      .finally(() => setLoading(false));
  }, [ctx.db]);

  if (loading) return <HudPanel title="Loading" accent="#00f0ff"><div style={{ padding: 8 }}>// fetching schemas…</div></HudPanel>;
  if (err) return <HudPanel title="Error" accent="#ff7b7b"><div style={{ padding: 8, color: '#ff7b7b' }}>{err}</div></HudPanel>;

  const total = rows.reduce((s, r) => s + r.table_count, 0);

  return (
    <HudPanel
      icon={<Database size={12} />}
      title="User Schemas"
      subtitle={`${rows.length} schemas · ${total} total tables`}
      leading={<HudStatusLed color="#00f0ff" />}
    >
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(220px, 1fr))', gap: 6 }}>
        {rows.map((s) => (
          <div key={s.name} style={rowCardStyle('#00f0ff')}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <span style={{ fontFamily: 'var(--font-mono, monospace)', fontWeight: 600 }}>{s.name}</span>
              <HudChip color="#7cc6ff">{s.table_count}</HudChip>
            </div>
            {s.owner && <div style={{ fontSize: 10, opacity: 0.7 }}>owner: {s.owner}</div>}
          </div>
        ))}
      </div>
    </HudPanel>
  );
}

/* ── Tables tab ─────────────────────────────────────────────── */

function TablesTab({ ctx }: { readonly ctx: DBCtx }) {
  const [schema, setSchema] = useState<string>('');
  const [schemas, setSchemas] = useState<AxiomSchema[]>([]);
  const [tables, setTables] = useState<AxiomTable[]>([]);
  const [selected, setSelected] = useState<AxiomTable | null>(null);
  const [columns, setColumns] = useState<AxiomColumn[]>([]);
  const [peek, setPeek] = useState<AxiomPeek | null>(null);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    listSchemas(undefined, ctx.db || undefined).then(setSchemas).catch(() => void 0);
    setSchema('');
    setTables([]);
    setSelected(null);
  }, [ctx.db]);

  useEffect(() => {
    setTables([]);
    setSelected(null);
    if (!schema) return;
    listTables(schema, undefined, ctx.db || undefined)
      .then((r) => { setTables(r); setErr(null); })
      .catch((e) => setErr(e instanceof Error ? e.message : 'load failed'));
  }, [schema, ctx.db]);

  const select = useCallback(async (t: AxiomTable) => {
    setSelected(t);
    setColumns([]); setPeek(null);
    try {
      const [cols, p] = await Promise.all([
        listColumns(t.schema, t.name, undefined, ctx.db || undefined),
        peekTable(t.schema, t.name, 5, undefined, ctx.db || undefined),
      ]);
      setColumns(cols);
      setPeek(p);
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'load failed');
    }
  }, [ctx.db]);

  return (
    <div style={{ display: 'grid', gridTemplateColumns: '1fr 2fr', gap: 12 }}>
      <HudPanel title="Tables" accent="#00f0ff" leading={<HudStatusLed color="#00f0ff" />}>
        <select
          value={schema}
          onChange={(e) => setSchema(e.target.value)}
          style={selectStyle}
        >
          <option value="">// pick a schema</option>
          {schemas.map((s) => (
            <option key={s.name} value={s.name}>{s.name} ({s.table_count})</option>
          ))}
        </select>
        {err && <div style={{ color: '#ff7b7b', fontSize: 11, padding: 6 }}>{err}</div>}
        <div style={{ display: 'grid', gap: 3, maxHeight: '70vh', overflowY: 'auto', marginTop: 6 }}>
          {tables.map((t) => (
            <button
              key={`${t.schema}.${t.name}`}
              type="button"
              onClick={() => select(t)}
              style={{
                ...rowCardStyle(domainColor(t.likely_domain)),
                textAlign: 'left',
                cursor: 'pointer',
                background: selected?.name === t.name ? 'rgba(0,240,255,0.08)' : 'transparent',
              }}
            >
              <div style={{ display: 'flex', justifyContent: 'space-between', gap: 6 }}>
                <span style={{ fontFamily: 'var(--font-mono, monospace)' }}>{t.name}</span>
                <HudChip color={domainColor(t.likely_domain)}>
                  {t.likely_domain || t.type}
                </HudChip>
              </div>
              <div style={{ fontSize: 9, opacity: 0.6 }}>
                ~{t.row_estimate.toLocaleString()} rows
              </div>
            </button>
          ))}
        </div>
      </HudPanel>

      <HudPanel
        title={selected ? `${selected.schema}.${selected.name}` : 'Select a table'}
        accent="#00f0ff"
        leading={<Eye size={12} />}
        meta={selected && <HudChip color={domainColor(selected.likely_domain)}>{selected.likely_domain || selected.type}</HudChip>}
      >
        {!selected && <div style={{ padding: 8, fontSize: 11, opacity: 0.6 }}>// click a table on the left to inspect columns + sample rows</div>}
        {selected && (
          <>
            <div style={{ marginBottom: 8 }}>
              <div style={headerStyle}>Columns ({columns.length})</div>
              <div style={{ display: 'grid', gap: 2, fontSize: 10, fontFamily: 'var(--font-mono, monospace)' }}>
                {columns.map((c) => (
                  <div key={c.name} style={colRowStyle}>
                    <span style={{ color: '#00f0ff' }}>{c.name}</span>
                    <span style={{ opacity: 0.7 }}>{c.data_type}</span>
                    {c.nullable && <span style={{ color: '#ffb86b', fontSize: 9 }}>NULL</span>}
                    {!c.nullable && <span style={{ color: '#6ff2a0', fontSize: 9 }}>NOT NULL</span>}
                  </div>
                ))}
              </div>
            </div>
            {peek && (
              <div>
                <div style={headerStyle}>Sample rows ({peek.rows.length})</div>
                {peek.note && <div style={{ fontSize: 11, opacity: 0.7, padding: 4 }}>{peek.note}</div>}
                {peek.rows.length > 0 && (
                  <div style={{ overflowX: 'auto', fontSize: 10, fontFamily: 'var(--font-mono, monospace)' }}>
                    <table style={{ borderCollapse: 'collapse', width: '100%' }}>
                      <thead>
                        <tr>
                          {peek.columns.map((c) => (
                            <th key={c} style={thStyle}>{c}</th>
                          ))}
                        </tr>
                      </thead>
                      <tbody>
                        {peek.rows.map((r, i) => (
                          <tr key={i}>
                            {r.map((v, j) => (
                              <td key={j} style={tdStyle}><PeekCell value={v} /></td>
                            ))}
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </div>
            )}
          </>
        )}
      </HudPanel>
    </div>
  );
}

/* ── Search tab ─────────────────────────────────────────────── */

function SearchTab({ ctx }: { readonly ctx: DBCtx }) {
  const [q, setQ] = useState('');
  const [hits, setHits] = useState<AxiomColumn[]>([]);
  const [loading, setLoading] = useState(false);

  const go = useCallback(async () => {
    if (!q.trim()) return;
    setLoading(true);
    try {
      setHits(await searchColumns(q.trim(), undefined, ctx.db || undefined));
    } finally { setLoading(false); }
  }, [q, ctx.db]);

  return (
    <HudPanel title="Search columns" accent="#ffaa00" leading={<Search size={12} />}>
      <div style={{ display: 'flex', gap: 6, marginBottom: 8 }}>
        <input
          type="search"
          value={q}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter') void go(); }}
          placeholder="// e.g. msisdn, email, payer_id, charged"
          style={{ ...selectStyle, flex: 1 }}
        />
        <button type="button" onClick={go} style={tabButtonStyle(true)}>Search</button>
      </div>
      {loading && <div style={{ fontSize: 11, opacity: 0.7, padding: 6 }}>// searching…</div>}
      <div style={{ display: 'grid', gap: 3, fontSize: 10, fontFamily: 'var(--font-mono, monospace)' }}>
        {hits.map((c, i) => (
          <div key={`${c.schema}.${c.table}.${c.name}-${i}`} style={rowCardStyle('#ffaa00')}>
            <div style={{ display: 'flex', gap: 6, alignItems: 'baseline', justifyContent: 'space-between' }}>
              <span>
                <span style={{ opacity: 0.7 }}>{c.schema}.</span>
                <span>{c.table}</span>
                <span style={{ color: '#00f0ff' }}>.{c.name}</span>
              </span>
              <HudChip color="#7cc6ff">{c.data_type}</HudChip>
            </div>
          </div>
        ))}
      </div>
      {!loading && hits.length === 0 && q && (
        <div style={{ fontSize: 11, opacity: 0.6, padding: 6 }}>
          // no columns matched "{q}"
        </div>
      )}
    </HudPanel>
  );
}

/* ── Endpoint Map tab ───────────────────────────────────────── */

function EndpointMapTab() {
  const [rows, setRows] = useState<AxiomEndpointMap[]>([]);
  const [query, setQuery] = useState('');

  useEffect(() => { listEndpointMap().then(setRows); }, []);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return rows;
    return rows.filter((r) => {
      const hay = `${r.method} ${r.path} ${r.summary} ${r.domain} ${(r.reads || []).join(' ')} ${(r.writes || []).join(' ')}`.toLowerCase();
      return hay.includes(q);
    });
  }, [rows, query]);

  const byDomain = useMemo(() => {
    const m = new Map<string, AxiomEndpointMap[]>();
    for (const r of filtered) {
      const arr = m.get(r.domain) ?? [];
      arr.push(r);
      m.set(r.domain, arr);
    }
    return Array.from(m.entries()).sort((a, b) => b[1].length - a[1].length);
  }, [filtered]);

  return (
    <div>
      <input
        type="search"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder="// filter endpoints or tables (e.g. payment, sim-swap, party.individual)"
        style={{ ...selectStyle, width: '100%', marginBottom: 10 }}
      />
      <div style={{ display: 'grid', gap: 10 }}>
        {byDomain.map(([domain, items]) => (
          <HudPanel
            key={domain}
            title={domain || 'Unclassified'}
            accent={domainColor(domain)}
            leading={<HudStatusLed color={domainColor(domain)} />}
            meta={<HudChip color={domainColor(domain)}>{items.length}</HudChip>}
          >
            <div style={{ display: 'grid', gap: 4 }}>
              {items.map((e) => (
                <div key={`${e.method}-${e.path}`} style={rowCardStyle(domainColor(domain))}>
                  <div style={{ display: 'flex', gap: 6, alignItems: 'baseline' }}>
                    <span style={{
                      minWidth: 54, fontSize: 10, fontWeight: 700,
                      color: METHOD_COLOR[e.method] || '#7cc6ff',
                      fontFamily: 'var(--font-mono, monospace)',
                    }}>{e.method}</span>
                    <span style={{ fontSize: 11, fontFamily: 'var(--font-mono, monospace)', flex: 1 }}>
                      {e.path}
                    </span>
                    <span style={{ fontSize: 10, opacity: 0.75 }}>{e.summary}</span>
                  </div>
                  <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap', marginTop: 3 }}>
                    {(e.reads || []).map((t) => (
                      <HudChip key={`r-${t}`} color="#7cc6ff">R: {t}</HudChip>
                    ))}
                    {(e.writes || []).map((t) => (
                      <HudChip key={`w-${t}`} color="#ff7de0">W: {t}</HudChip>
                    ))}
                  </div>
                  {e.notes && (
                    <div style={{ fontSize: 10, opacity: 0.7, marginTop: 3 }}>{e.notes}</div>
                  )}
                </div>
              ))}
            </div>
          </HudPanel>
        ))}
      </div>
    </div>
  );
}

/* ── Shared styles ──────────────────────────────────────────── */

const rowCardStyle = (accent: string): React.CSSProperties => ({
  padding: 6,
  borderLeft: `2px solid ${accent}55`,
  borderRadius: 2,
});
const selectStyle: React.CSSProperties = {
  width: '100%',
  padding: '6px 8px',
  background: 'rgba(124,198,255,0.08)',
  color: 'var(--ink, #e6f6ff)',
  border: '1px solid rgba(124,198,255,0.25)',
  borderRadius: 4,
  fontFamily: 'inherit',
  fontSize: 12,
};
const headerStyle: React.CSSProperties = {
  fontSize: 10,
  textTransform: 'uppercase',
  letterSpacing: '0.1em',
  opacity: 0.7,
  padding: '4px 0',
};
const colRowStyle: React.CSSProperties = {
  display: 'grid',
  gridTemplateColumns: '1fr 1fr auto',
  gap: 6,
  padding: '2px 4px',
  borderBottom: '1px dashed rgba(124,198,255,0.15)',
};
const thStyle: React.CSSProperties = {
  textAlign: 'left',
  padding: '3px 6px',
  background: 'rgba(0,240,255,0.08)',
  color: '#00f0ff',
  fontSize: 9,
  textTransform: 'uppercase',
};
const tdStyle: React.CSSProperties = {
  padding: '3px 6px',
  borderBottom: '1px solid rgba(124,198,255,0.1)',
  maxWidth: 220,
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
};
