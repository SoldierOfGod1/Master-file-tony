/* ============================================================
   rain Service — full monitoring console (NOC-style).
   Mirrors the Platform Monitor on the Dashboard but shows every
   service (not just the top-6), plus DB health, severity-tiered
   alerts with root-cause hints, and a live incident timeline.
   ============================================================ */

import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Activity, AlertTriangle, Database, ExternalLink, ShieldCheck,
  ShieldAlert, Clock, CheckCircle2, XCircle, Flame, BookOpen, RefreshCw,
} from 'lucide-react';
import HudPanel from '../components/shared/HudPanel';
import HudSummaryStrip from '../components/shared/HudSummaryStrip';
import { HudChip, HudStatusLed } from '../components/shared/HudChip';
import {
  listPlatformHealth,
  listDatabaseHealth,
  listPlatformAlerts,
  listPlatformIncidents,
  getIncidentTimeline,
  ackIncident,
  resolveIncident,
  type PlatformStatus,
  type PlatformState,
  type DatabaseHealth,
  type StoredAlert,
  type Incident,
  type IncidentTimeline,
  type Severity,
} from '../api/platforms';
import hudStyles from '../theme/hud.module.css';

const STATE_COLOR: Record<PlatformState, string> = {
  up: '#6ff2a0',
  degraded: '#ffaa00',
  down: '#ff7b7b',
  unknown: '#7cc6ff',
};

const SEVERITY_COLOR: Record<Severity, string> = {
  info:     '#7cc6ff',
  warning:  '#ffaa00',
  critical: '#ff7b7b',
  p1:       '#ff3355',
};

function fmtDate(iso?: string): string {
  if (!iso) return '—';
  try {
    return new Date(iso).toLocaleString('en-ZA', {
      hour: '2-digit', minute: '2-digit', day: '2-digit', month: 'short',
    });
  } catch {
    return iso;
  }
}

function useAutoRefresh<T>(
  fetchFn: () => Promise<T>,
  intervalMs: number,
): [T | null, () => Promise<void>] {
  const [data, setData] = useState<T | null>(null);
  const refresh = useCallback(async () => {
    setData(await fetchFn());
  }, [fetchFn]);
  useEffect(() => {
    void refresh();
    const t = window.setInterval(() => { void refresh(); }, intervalMs);
    return () => window.clearInterval(t);
  }, [refresh, intervalMs]);
  return [data, refresh];
}

/* ---- Axiom pinned banner: surfaces DB health at top of the page ---- */
function AxiomBanner({ dbs, alerts }: { readonly dbs: DatabaseHealth[]; readonly alerts: StoredAlert[] }) {
  const axiom = dbs.filter((d) => d.is_axiom);
  if (axiom.length === 0) return null;
  const allUp = axiom.every((d) => d.reachable);
  const accent = allUp ? '#6ff2a0' : '#ff3355';
  const axiomP1 = alerts.filter((a) => a.severity === 'p1' && axiom.some((d) => d.id === a.service_id));
  return (
    <HudPanel
      title={allUp ? 'Axiom Databases — HEALTHY' : 'Axiom Databases — INCIDENT'}
      accent={accent}
      icon={<Flame size={12} />}
      leading={<HudStatusLed color={accent} animate={!allUp} />}
      meta={<HudChip color={accent}>{axiom.filter((d) => d.reachable).length}/{axiom.length} reachable</HudChip>}
    >
      <div style={{
        display: 'grid',
        gridTemplateColumns: 'repeat(auto-fit, minmax(280px, 1fr))',
        gap: 10, padding: 6,
      }}>
        {axiom.map((d) => (
          <div key={d.id} style={{
            padding: 10, borderLeft: `3px solid ${d.reachable ? '#6ff2a0' : '#ff3355'}`,
            background: 'rgba(255,51,85,0.04)',
            fontFamily: 'var(--font-mono, monospace)', fontSize: 11,
          }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline' }}>
              <span style={{ color: d.reachable ? '#6ff2a0' : '#ff3355', fontSize: 13 }}>
                {d.label}
              </span>
              <HudChip color={d.reachable ? '#6ff2a0' : '#ff3355'}>
                {d.reachable ? 'UP' : 'DOWN'}
              </HudChip>
            </div>
            <div style={{ fontSize: 10, opacity: 0.75, marginTop: 3 }}>
              {d.host} · ping {d.ping_ms}ms · query {d.query_ms}ms · {d.active_sessions} sessions
            </div>
            {d.error && (
              <div style={{ fontSize: 10, color: '#ff7b7b', marginTop: 3, wordBreak: 'break-word' }}>
                {d.error.slice(0, 200)}
              </div>
            )}
          </div>
        ))}
      </div>
      {axiomP1.length > 0 && (
        <div style={{
          padding: '6px 10px', background: 'rgba(255,51,85,0.1)',
          borderTop: '1px solid rgba(255,51,85,0.3)',
          fontSize: 11, color: '#ff3355',
        }}>
          <AlertTriangle size={12} style={{ verticalAlign: 'middle', marginRight: 4 }} />
          {axiomP1.length} open P1 alert{axiomP1.length === 1 ? '' : 's'} — expect Risk Portal + Assisted Sales degradation.
        </div>
      )}
    </HudPanel>
  );
}

/* ---- Services grid: every monitored URL with detailed status ---- */
function ServicesGrid({ rows }: { readonly rows: PlatformStatus[] }) {
  if (rows.length === 0) {
    return (
      <HudPanel title="Services" accent="#7cc6ff" leading={<HudStatusLed color="#7cc6ff" />}>
        <div style={{ padding: 10, fontSize: 11, opacity: 0.7 }}>// no health data yet — first probe in flight</div>
      </HudPanel>
    );
  }
  // Split by environment first so prod / public services sit
  // above SIT — operators scan top-down for the ones customers
  // are actually on. Within each environment we still stack by
  // `group` (BSS / Customer / Ops / …) so related services
  // cluster together.
  const ENV_ORDER: Array<{ key: string; label: string; accent: string }> = [
    { key: 'public',   label: 'Production · public',   accent: '#ff7de0' },
    { key: 'internal', label: 'Production · internal', accent: '#6ff2a0' },
    { key: 'sit',      label: 'SIT',                   accent: '#ffaa00' },
  ];
  const byEnv = new Map<string, Map<string, PlatformStatus[]>>();
  for (const r of rows) {
    const env = r.environment || 'internal';
    const groupMap = byEnv.get(env) ?? new Map<string, PlatformStatus[]>();
    const arr = groupMap.get(r.group) ?? [];
    arr.push(r);
    groupMap.set(r.group, arr);
    byEnv.set(env, groupMap);
  }
  return (
    <HudPanel title={`Services · ${rows.length}`} accent="#00f0ff" leading={<HudStatusLed color="#00f0ff" />}>
      {ENV_ORDER.map((env) => {
        const groups = byEnv.get(env.key);
        if (!groups || groups.size === 0) return null;
        const count = Array.from(groups.values()).reduce((a, b) => a + b.length, 0);
        return (
          <div key={env.key} style={{ marginBottom: 14 }}>
            {/* Environment divider — bold ribbon across the panel
                with the env label + count. Keeps prod services
                visually separated from SIT so operators can't
                confuse the two at a glance. */}
            <div style={{
              display: 'flex', alignItems: 'center', gap: 10,
              padding: '6px 8px', marginBottom: 6,
              borderTop: `2px solid ${env.accent}66`,
              borderBottom: `1px solid ${env.accent}22`,
              background: `${env.accent}0A`,
            }}>
              <HudStatusLed color={env.accent} animate={false} />
              <span style={{
                fontSize: 11, color: env.accent,
                fontFamily: 'var(--font-display, Orbitron, monospace)',
                textTransform: 'uppercase', letterSpacing: '0.12em',
              }}>
                {env.label}
              </span>
              <span style={{ fontSize: 10, opacity: 0.7, marginLeft: 'auto' }}>
                {count} service{count === 1 ? '' : 's'}
              </span>
            </div>
            {Array.from(groups.entries()).map(([group, items]) => (
              <div key={group} style={{ marginBottom: 10 }}>
                <div style={{
                  fontSize: 9, textTransform: 'uppercase', letterSpacing: '0.1em',
                  color: '#7cc6ff', padding: '4px 8px', opacity: 0.7,
                }}>
                  {group} · {items.length}
                </div>
                <div style={{
                  display: 'grid',
                  gridTemplateColumns: 'repeat(auto-fill, minmax(320px, 1fr))',
                  gap: 8, padding: '0 4px',
                }}>
                  {items.map((p) => <ServiceCard key={p.id} p={p} />)}
                </div>
              </div>
            ))}
          </div>
        );
      })}
    </HudPanel>
  );
}

function ServiceCard({ p }: { readonly p: PlatformStatus }) {
  const colour = STATE_COLOR[p.state];
  const sslColour = p.tls.days_to_expiry <= 7 ? '#ff3355'
                  : p.tls.days_to_expiry <= 14 ? '#ffaa00'
                  : '#6ff2a0';
  return (
    <div style={{
      padding: 10, borderLeft: `3px solid ${colour}`,
      background: 'rgba(0,240,255,0.03)',
      fontFamily: 'var(--font-mono, monospace)', fontSize: 11,
      display: 'flex', flexDirection: 'column', gap: 4,
    }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', gap: 6 }}>
        <span style={{ color: colour, fontSize: 13 }}>{p.name}</span>
        <HudChip color={colour}>{p.state.toUpperCase()}</HudChip>
      </div>
      <div style={{ fontSize: 10, opacity: 0.75, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
        {p.url}
      </div>
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', fontSize: 10, opacity: 0.85 }}>
        <span>latency <b style={{ color: colour }}>{p.latency_ms}ms</b></span>
        {p.http_code > 0 && <span>HTTP {p.http_code}</span>}
        <span>uptime 24h <b>{p.uptime_24h.toFixed(1)}%</b></span>
      </div>
      <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', fontSize: 9, marginTop: 2 }}>
        {p.tls.expires_at && (
          <HudChip color={sslColour}>
            SSL {p.tls.days_to_expiry}d
          </HudChip>
        )}
        {p.dns.resolved ? (
          <HudChip color="#6ff2a0">DNS ok</HudChip>
        ) : p.dns.error ? (
          <HudChip color="#ff3355">DNS FAIL</HudChip>
        ) : null}
        {p.content.checked && (!p.content.title_ok || !p.content.body_ok) && (
          <HudChip color="#ffaa00">CONTENT FAIL</HudChip>
        )}
        {p.failure_streak >= 3 && (
          <HudChip color="#ff3355">STREAK {p.failure_streak}</HudChip>
        )}
        {p.owner && <span style={{ opacity: 0.55 }}>@{p.owner}</span>}
      </div>
      {p.error && (
        <div style={{ fontSize: 10, color: '#ff7b7b', wordBreak: 'break-word' }}>
          {p.error.slice(0, 200)}
        </div>
      )}
      <div style={{ display: 'flex', gap: 6, marginTop: 2 }}>
        <a href={p.url} target="_blank" rel="noreferrer" style={iconBtn('#7cc6ff')}>
          <ExternalLink size={10} /> open
        </a>
        {p.docs_url && (
          <a href={p.docs_url} target="_blank" rel="noreferrer" style={iconBtn('#00f0ff')}>
            <BookOpen size={10} /> docs
          </a>
        )}
      </div>
    </div>
  );
}

function iconBtn(color: string): React.CSSProperties {
  return {
    display: 'inline-flex', alignItems: 'center', gap: 3,
    padding: '2px 6px', fontSize: 9,
    color, background: 'transparent',
    border: `1px solid ${color}66`, borderRadius: 3,
    textDecoration: 'none', textTransform: 'uppercase', letterSpacing: '0.06em',
  };
}

/* ---- Database health panel (non-Axiom + Axiom passthrough) ---- */
function DatabaseHealthPanel({ dbs }: { readonly dbs: DatabaseHealth[] }) {
  const nonAxiom = dbs.filter((d) => !d.is_axiom);
  return (
    <HudPanel
      title={`Database Health · ${dbs.length}`}
      accent="#c488ff"
      icon={<Database size={12} />}
      leading={<HudStatusLed color="#c488ff" />}
    >
      {dbs.length === 0 && (
        <div style={{ padding: 10, fontSize: 11, opacity: 0.7 }}>
          // no DB connections configured — add one in Settings.
        </div>
      )}
      <div style={{ display: 'grid', gap: 4, padding: 4 }}>
        {dbs.map((d) => {
          const colour = d.reachable ? '#6ff2a0' : '#ff3355';
          return (
            <div key={d.id} style={{
              padding: '6px 8px', borderLeft: `2px solid ${colour}55`,
              fontFamily: 'var(--font-mono, monospace)', fontSize: 11,
              display: 'grid', gridTemplateColumns: '1fr auto auto auto', gap: 6, alignItems: 'baseline',
            }}>
              <span>
                <span style={{ color: colour }}>{d.label}</span>
                {d.is_axiom && <span style={{ fontSize: 9, opacity: 0.7, marginLeft: 5 }}>P1</span>}
              </span>
              <span style={{ fontSize: 10, opacity: 0.7 }}>{d.host}</span>
              <span style={{ fontSize: 10 }}>{d.reachable ? `${d.query_ms}ms` : 'down'}</span>
              <HudChip color={colour}>{d.reachable ? 'UP' : 'DOWN'}</HudChip>
            </div>
          );
        })}
      </div>
      {nonAxiom.length === 0 && dbs.length > 0 && (
        <div style={{ fontSize: 10, opacity: 0.6, padding: '4px 8px' }}>
          Only Axiom connections configured — non-Axiom DBs would appear here.
        </div>
      )}
    </HudPanel>
  );
}

/* ---- Alert center ---- */
function AlertCenter({ alerts }: { readonly alerts: StoredAlert[] }) {
  const open = alerts.filter((a) => a.state === 'open');
  return (
    <HudPanel
      title={`Alert Center · ${open.length} open`}
      accent="#ffaa00"
      icon={<AlertTriangle size={12} />}
      leading={<HudStatusLed color="#ffaa00" animate={open.length > 0} />}
    >
      {open.length === 0 && (
        <div style={{ padding: 10, fontSize: 11, opacity: 0.7 }}>// no open alerts — all clear</div>
      )}
      <div style={{ display: 'grid', gap: 4, padding: 4 }}>
        {open.slice(0, 30).map((a) => {
          const colour = SEVERITY_COLOR[a.severity];
          return (
            <div key={a.id} style={{
              padding: '6px 8px', borderLeft: `2px solid ${colour}`,
              fontFamily: 'var(--font-mono, monospace)', fontSize: 11,
            }}>
              <div style={{ display: 'flex', gap: 6, alignItems: 'baseline' }}>
                <HudChip color={colour}>{a.severity.toUpperCase()}</HudChip>
                <span style={{ color: colour }}>{a.service_id}</span>
                <span style={{ fontSize: 10, opacity: 0.7, marginLeft: 'auto' }}>{fmtDate(a.created_at)}</span>
              </div>
              <div style={{ marginTop: 2 }}>{a.message}</div>
              {a.cause && <div style={{ fontSize: 10, opacity: 0.75, marginTop: 1 }}><b>cause:</b> {a.cause}</div>}
              {a.next_step && <div style={{ fontSize: 10, opacity: 0.75 }}><b>next:</b> {a.next_step}</div>}
            </div>
          );
        })}
      </div>
    </HudPanel>
  );
}

/* ---- Incident timeline ---- */
function IncidentTimeline({
  incidents, onAck, onResolve,
}: {
  readonly incidents: Incident[];
  readonly onAck: (id: number) => void;
  readonly onResolve: (id: number) => void;
}) {
  if (incidents.length === 0) {
    return (
      <HudPanel
        title="Incidents"
        accent="#00f0ff"
        icon={<Clock size={12} />}
        leading={<HudStatusLed color="#6ff2a0" animate={false} />}
      >
        <div style={{ padding: 10, fontSize: 11, opacity: 0.7 }}>// no incidents on record</div>
      </HudPanel>
    );
  }
  return (
    <HudPanel
      title={`Incidents · ${incidents.length}`}
      accent="#00f0ff"
      icon={<Clock size={12} />}
      leading={<HudStatusLed color="#00f0ff" />}
    >
      <div style={{ display: 'grid', gap: 6, padding: 4 }}>
        {incidents.map((i) => {
          const colour = SEVERITY_COLOR[i.severity];
          const stateColour =
            i.state === 'open' ? '#ff3355' :
            i.state === 'investigating' ? '#ffaa00' :
            i.state === 'mitigated' ? '#6ff2a0' :
            '#7cc6ff';
          return (
            <div key={i.id} style={{
              padding: 8, borderLeft: `3px solid ${colour}`,
              background: 'rgba(0,240,255,0.04)',
              fontFamily: 'var(--font-mono, monospace)', fontSize: 11,
            }}>
              <div style={{ display: 'flex', gap: 6, alignItems: 'baseline', flexWrap: 'wrap' }}>
                <HudChip color={colour}>{i.severity.toUpperCase()}</HudChip>
                <HudChip color={stateColour}>{i.state.toUpperCase()}</HudChip>
                <span style={{ color: colour }}>{i.service_id}</span>
                <span style={{ fontSize: 10, opacity: 0.7, marginLeft: 'auto' }}>
                  opened {fmtDate(i.opened_at)}
                </span>
              </div>
              <div style={{ marginTop: 3 }}>{i.title}</div>
              {i.summary && <div style={{ fontSize: 10, opacity: 0.75 }}>{i.summary}</div>}
              {i.timeline && i.timeline.length > 0 && (
                <ul style={{ margin: '4px 0 0 0', padding: '0 0 0 14px', fontSize: 10, opacity: 0.8 }}>
                  {i.timeline.slice(-5).map((ev) => (
                    <li key={ev.id}>
                      <b>{ev.kind}</b> · {ev.message} <span style={{ opacity: 0.6 }}>({fmtDate(ev.at)})</span>
                    </li>
                  ))}
                </ul>
              )}
              <div style={{ display: 'flex', gap: 5, marginTop: 5 }}>
                {i.state === 'open' && (
                  <button type="button" onClick={() => onAck(i.id)} style={actionBtn('#ffaa00')}>
                    <ShieldAlert size={10} /> ack
                  </button>
                )}
                {i.state !== 'resolved' && (
                  <button type="button" onClick={() => onResolve(i.id)} style={actionBtn('#6ff2a0')}>
                    <CheckCircle2 size={10} /> resolve
                  </button>
                )}
              </div>
              {/* Phase D2 — correlation rollup. Cheap one-shot fetch
                  that pulls every chat / audit / approval / spend
                  row tagged with the incident id. Folded by default
                  to keep the panel scannable. */}
              <IncidentTimelineExpand incidentID={String(i.id)} />
            </div>
          );
        })}
      </div>
    </HudPanel>
  );
}

/* Per-incident timeline expander — Phase D2. Lazy loads on first
   open so we don't fan out N requests on render. Renders four
   parallel lists (chat / approvals / IMSI / spend) each capped
   server-side. Empty sections render empty (zero noise). */
function IncidentTimelineExpand({ incidentID }: { readonly incidentID: string }) {
  const [open, setOpen] = useState(false);
  const [data, setData] = useState<IncidentTimeline | null>(null);
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const loadIfNeeded = useCallback(async () => {
    if (data || loading) return;
    setLoading(true);
    try {
      const t = await getIncidentTimeline(incidentID);
      setData(t);
      setErr(null);
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'unknown error');
    } finally {
      setLoading(false);
    }
  }, [data, loading, incidentID]);

  const onToggle = useCallback(() => {
    setOpen((wasOpen) => {
      if (!wasOpen) void loadIfNeeded();
      return !wasOpen;
    });
  }, [loadIfNeeded]);

  return (
    <div style={{ marginTop: 6, fontSize: 10, fontFamily: 'var(--font-mono, monospace)' }}>
      <button
        type="button"
        onClick={onToggle}
        style={{
          padding: '2px 8px',
          fontSize: 9,
          letterSpacing: '0.06em',
          textTransform: 'uppercase',
          color: '#b980ff',
          background: 'rgba(185, 128, 255, 0.06)',
          border: '1px solid rgba(185, 128, 255, 0.3)',
          borderRadius: 3,
          cursor: 'pointer',
          fontFamily: 'inherit',
        }}
      >
        {open ? '▼' : '▶'} correlation rollup
      </button>
      {open && (
        <div style={{ marginTop: 4, paddingLeft: 8 }}>
          {loading && <div style={{ opacity: 0.6 }}>loading…</div>}
          {err && <div style={{ color: '#ff7b7b' }}>{err}</div>}
          {data && (
            <>
              <TimelineSection title="conversations" rows={data.conversations} fields={['id', 'title', 'user_id', 'created_at']} />
              <TimelineSection title="approvals"     rows={data.approvals}     fields={['id', 'title', 'requester', 'status', 'priority', 'created_at']} />
              <TimelineSection title="imsi audits"   rows={data.imsi_audits}   fields={['individual_id', 'source', 'winning_phase', 'imsi_count', 'at']} />
              <TimelineSection title="spend"         rows={data.cost_records}  fields={['model_name', 'amount_zar', 'tokens_used', 'user_id', 'date']} />
              <div style={{ marginTop: 4, fontSize: 10, color: '#b980ff' }}>
                Σ R{data.total_zar.toFixed(2)} agent spend during incident
              </div>
            </>
          )}
        </div>
      )}
    </div>
  );
}

function TimelineSection({
  title, rows, fields,
}: {
  readonly title: string;
  readonly rows: ReadonlyArray<Record<string, unknown>>;
  readonly fields: readonly string[];
}) {
  if (rows.length === 0) return null;
  return (
    <div style={{ marginTop: 4 }}>
      <div style={{ fontSize: 9, letterSpacing: '0.08em', textTransform: 'uppercase', opacity: 0.7 }}>
        {title} · {rows.length}
      </div>
      <div style={{ marginTop: 2 }}>
        {rows.slice(0, 8).map((r, i) => (
          <div
            key={i}
            style={{
              padding: '2px 6px',
              borderLeft: '2px solid rgba(185, 128, 255, 0.3)',
              marginBottom: 2,
              opacity: 0.85,
              wordBreak: 'break-word',
            }}
          >
            {fields.map((f) => {
              const v = r[f];
              if (v === undefined || v === null || v === '') return null;
              return (
                <span key={f} style={{ marginRight: 8 }}>
                  <span style={{ opacity: 0.55 }}>{f}=</span>
                  {String(v).slice(0, 40)}
                </span>
              );
            })}
          </div>
        ))}
        {rows.length > 8 && (
          <div style={{ fontSize: 9, opacity: 0.55, paddingLeft: 8 }}>
            (+{rows.length - 8} more — server cap)
          </div>
        )}
      </div>
    </div>
  );
}

function actionBtn(color: string): React.CSSProperties {
  return {
    display: 'inline-flex', alignItems: 'center', gap: 3,
    padding: '3px 8px', fontSize: 9,
    color, background: 'transparent',
    border: `1px solid ${color}66`, borderRadius: 3,
    textTransform: 'uppercase', letterSpacing: '0.06em',
    cursor: 'pointer',
  };
}

/* ---- Page root ---- */
export default function ServicePage() {
  const [services] = useAutoRefresh(listPlatformHealth, 30_000);
  const [dbs] = useAutoRefresh(listDatabaseHealth, 30_000);
  const [alerts, refreshAlerts] = useAutoRefresh(() => listPlatformAlerts(), 20_000);
  const [incidents, refreshIncidents] = useAutoRefresh(() => listPlatformIncidents(), 20_000);

  const rows = services ?? [];
  const dbRows = dbs ?? [];
  const alertRows = alerts ?? [];
  const incidentRows = incidents ?? [];

  const counters = useMemo(() => {
    const healthy = rows.filter((r) => r.state === 'up').length;
    const degraded = rows.filter((r) => r.state === 'degraded').length;
    const down = rows.filter((r) => r.state === 'down').length;
    const p1 = incidentRows.filter((i) => i.severity === 'p1' && i.state !== 'resolved').length;
    return { healthy, degraded, down, p1, total: rows.length };
  }, [rows, incidentRows]);

  const headerAccent = counters.p1 > 0 ? '#ff3355'
                    : counters.down > 0 ? '#ff7b7b'
                    : counters.degraded > 0 ? '#ffaa00'
                    : '#6ff2a0';

  const onAck = useCallback(async (id: number) => {
    await ackIncident(id);
    await refreshIncidents();
  }, [refreshIncidents]);
  const onResolve = useCallback(async (id: number) => {
    await resolveIncident(id);
    await refreshIncidents();
    await refreshAlerts();
  }, [refreshIncidents, refreshAlerts]);

  return (
    <div className={hudStyles.page}>
      <HudSummaryStrip
        title="rain Service · Ops Console"
        subtitle={
          counters.p1 > 0
            ? `P1 OPEN — ${counters.p1} incident${counters.p1 === 1 ? '' : 's'} demanding attention`
            : `${counters.healthy}/${counters.total} services healthy · ${alertRows.filter((a) => a.state === 'open').length} open alerts`
        }
        gaugeValue={counters.total > 0 ? counters.healthy / counters.total : 0}
        gaugeReadout={counters.total > 0 ? `${(counters.healthy / counters.total * 100).toFixed(2)}%` : '—'}
        gaugeLabel="HEALTH"
        gaugeColor={headerAccent}
        segments={[
          { label: 'up',       value: counters.healthy,  color: '#6ff2a0' },
          { label: 'degraded', value: counters.degraded, color: '#ffaa00' },
          { label: 'down',     value: counters.down,     color: '#ff7b7b' },
        ]}
        extra={
          <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
            {counters.p1 > 0 && <HudChip color="#ff3355"><Flame size={10} /> P1</HudChip>}
            <HudStatusLed color={headerAccent} animate={counters.down > 0 || counters.p1 > 0} />
          </div>
        }
      />

      <div style={{ display: 'flex', flexDirection: 'column', gap: 12, marginTop: 12 }}>
        <AxiomBanner dbs={dbRows} alerts={alertRows} />
        <div style={{
          display: 'grid', gap: 12,
          // 2:1 layout above 1200px (services wide, ops sidebar);
          // stacks on anything narrower so nothing gets squashed.
          gridTemplateColumns: 'repeat(auto-fit, minmax(min(420px, 100%), 1fr))',
        }}>
          <ServicesGrid rows={rows} />
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            <DatabaseHealthPanel dbs={dbRows} />
            <AlertCenter alerts={alertRows} />
          </div>
        </div>
        <IncidentTimeline incidents={incidentRows} onAck={onAck} onResolve={onResolve} />
      </div>
    </div>
  );
}
