/* ============================================================
   Security Page — actionable revamp.

   Old version was a trust-score gauge plus three counters
   (Critical / Warning / Info) with no detail and no actions.
   New version reads from /platforms/alerts directly so the
   operator sees:

     - top-10 open alerts with severity, service, message,
       cause, suggested next step, and a × Resolve button
       (mirrors the Service-page Alert Center)
     - asset chip row showing per-service current alert count
     - recent audit trail of resolved alerts so an operator
       can see what they (or auto-recovery) closed today.

   Trust gauge stays — it's the headline number — but its
   value is now derived from the live alert mix instead of
   a stale `state.security` blob.
   ============================================================ */

import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  ShieldCheck,
  AlertTriangle,
  Clock,
  BookOpen,
  CheckCircle2,
} from 'lucide-react';
import HudGauge from '../components/shared/HudGauge';
import HudPanel from '../components/shared/HudPanel';
import HudSummaryStrip from '../components/shared/HudSummaryStrip';
import { HudChip, HudStatusLed } from '../components/shared/HudChip';
import {
  listPlatformAlerts,
  resolveAlert,
  type StoredAlert,
  type Severity,
} from '../api/platforms';
import hudStyles from '../theme/hud.module.css';
import styles from './SecurityPage.module.css';

const SEVERITY_COLOR: Record<Severity, string> = {
  info:     '#7cc6ff',
  warning:  '#ffaa00',
  critical: '#ff7b7b',
  p1:       '#ff3355',
};

function fmtDate(iso?: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  const diff = Math.max(0, Math.floor((Date.now() - d.getTime()) / 1000));
  if (diff < 60) return `${diff}s ago`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  return d.toLocaleString();
}

// Trust score is a quick mapping from open-alert mix to a 0-100
// gauge. P1 dominates; criticals erode quickly; warnings drag a
// bit; info is free. Calibrated so a clean inbox lands at 100, one
// P1 drops you below 50, one critical without a P1 lands ~70.
function trustFromAlerts(open: StoredAlert[]): number {
  let score = 100;
  for (const a of open) {
    switch (a.severity) {
      case 'p1':       score -= 50; break;
      case 'critical': score -= 15; break;
      case 'warning':  score -=  4; break;
      default:         score -=  1;
    }
  }
  return Math.max(0, Math.min(100, score));
}

function trustPalette(score: number): { color: string; status: string } {
  if (score >= 80) return { color: '#6ff2a0', status: 'Healthy' };
  if (score >= 50) return { color: '#ffaa00', status: 'Caution' };
  return { color: '#ff3355', status: 'Critical' };
}

/* ---- Open Alerts panel (the new hero tile) ---- */
function OpenAlertsTile({
  alerts,
  onResolve,
}: {
  readonly alerts: StoredAlert[];
  readonly onResolve: (id: number) => void | Promise<void>;
}) {
  const open = alerts.filter((a) => a.state === 'open').slice(0, 10);
  return (
    <HudPanel
      title={`Open Alerts · top ${open.length}`}
      accent="#ff7b7b"
      icon={<AlertTriangle size={12} />}
      leading={<HudStatusLed color={open.length > 0 ? '#ff3355' : '#6ff2a0'} animate={open.length > 0} />}
      meta={<HudChip color={open.length > 0 ? '#ff3355' : '#6ff2a0'}>{open.length > 0 ? 'attention' : 'all clear'}</HudChip>}
    >
      {open.length === 0 ? (
        <div style={{ padding: 14, fontSize: 12, opacity: 0.7 }}>
          // no open alerts. Last clean signal at {fmtDate(new Date().toISOString())}.
        </div>
      ) : (
        <div style={{ display: 'grid', gap: 4, padding: 4 }}>
          {open.map((a) => {
            const c = SEVERITY_COLOR[a.severity];
            return (
              <div
                key={a.id}
                style={{
                  padding: '8px 10px',
                  borderLeft: `3px solid ${c}`,
                  fontFamily: 'var(--font-mono, monospace)',
                  fontSize: 11,
                }}
              >
                <div style={{ display: 'flex', gap: 6, alignItems: 'baseline', flexWrap: 'wrap' }}>
                  <HudChip color={c}>{a.severity.toUpperCase()}</HudChip>
                  <span style={{ color: c, fontWeight: 600 }}>{a.service_id}</span>
                  <span style={{ opacity: 0.7 }}>{a.kind}</span>
                  <span style={{ marginLeft: 'auto', opacity: 0.6, fontSize: 10 }}>{fmtDate(a.created_at)}</span>
                  <button
                    type="button"
                    onClick={() => {
                      if (window.confirm(`Resolve alert "${a.message}"?\n\nFlips state to resolved. Won't fix the cause — only takes the row out of the inbox.`)) {
                        void onResolve(a.id);
                      }
                    }}
                    title="Resolve alert"
                    style={{
                      background: 'transparent',
                      border: `1px solid ${c}55`,
                      color: c,
                      fontFamily: 'inherit',
                      fontSize: 10,
                      lineHeight: 1,
                      padding: '2px 5px',
                      cursor: 'pointer',
                      borderRadius: 2,
                    }}
                  >× resolve</button>
                </div>
                <div style={{ marginTop: 4 }}>{a.message}</div>
                {a.cause && (
                  <div style={{ marginTop: 1, fontSize: 10, opacity: 0.75 }}>
                    <strong>cause:</strong> {a.cause}
                  </div>
                )}
                {a.next_step && (
                  <div style={{ fontSize: 10, opacity: 0.75, display: 'flex', gap: 6, alignItems: 'baseline' }}>
                    <strong>next:</strong> {a.next_step}
                    <a
                      href={runbookFor(a)}
                      target="_blank"
                      rel="noreferrer"
                      style={{ color: '#00f0ff', display: 'inline-flex', alignItems: 'center', gap: 3, fontSize: 10 }}
                    >
                      runbook <BookOpen size={10} />
                    </a>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
    </HudPanel>
  );
}

// runbookFor maps an alert to the closest runbook URL we know.
// Today's runbooks live in docs/runbooks/ — pointing at the repo
// browse path so the operator can read offline. When the team
// stands up a real runbook portal, swap this resolver.
function runbookFor(a: StoredAlert): string {
  const slug = (a.kind || '').toLowerCase().replace(/[^a-z0-9]+/g, '-');
  return `/docs/runbooks/${slug || 'general'}.md`;
}

/* ---- Asset chip row ---- */
function AssetChipRow({ alerts }: { readonly alerts: StoredAlert[] }) {
  const byAsset = useMemo(() => {
    const m = new Map<string, { open: number; worst: Severity }>();
    for (const a of alerts) {
      if (a.state !== 'open') continue;
      const cur = m.get(a.service_id) ?? { open: 0, worst: 'info' as Severity };
      cur.open += 1;
      if (sevRank(a.severity) > sevRank(cur.worst)) cur.worst = a.severity;
      m.set(a.service_id, cur);
    }
    return Array.from(m.entries()).sort((a, b) => sevRank(b[1].worst) - sevRank(a[1].worst));
  }, [alerts]);
  if (byAsset.length === 0) {
    return (
      <HudPanel
        title="Asset state"
        accent="#6ff2a0"
        leading={<HudStatusLed color="#6ff2a0" animate={false} />}
        icon={<ShieldCheck size={12} />}
      >
        <div style={{ padding: 12, fontSize: 12, opacity: 0.7 }}>// every monitored asset is clean.</div>
      </HudPanel>
    );
  }
  return (
    <HudPanel
      title={`Asset state · ${byAsset.length} assets with open alerts`}
      accent="#ffaa00"
      leading={<HudStatusLed color="#ffaa00" animate={true} />}
      icon={<AlertTriangle size={12} />}
    >
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, padding: 12 }}>
        {byAsset.map(([asset, info]) => (
          <HudChip key={asset} color={SEVERITY_COLOR[info.worst]}>
            {asset} · {info.open}
          </HudChip>
        ))}
      </div>
    </HudPanel>
  );
}

function sevRank(s: Severity): number {
  switch (s) {
    case 'p1':       return 4;
    case 'critical': return 3;
    case 'warning':  return 2;
    case 'info':     return 1;
    default:         return 0;
  }
}

/* ---- Audit trail ---- */
function AuditTrail({ alerts }: { readonly alerts: StoredAlert[] }) {
  const recentlyResolved = useMemo(
    () => alerts
      .filter((a) => a.state === 'resolved')
      .slice(0, 10),
    [alerts],
  );
  return (
    <HudPanel
      title={`Recently resolved · last ${recentlyResolved.length}`}
      accent="#6ff2a0"
      icon={<CheckCircle2 size={12} />}
      leading={<HudStatusLed color="#6ff2a0" animate={false} />}
    >
      {recentlyResolved.length === 0 ? (
        <div style={{ padding: 12, fontSize: 12, opacity: 0.7 }}>
          // nothing resolved in the recent window.
        </div>
      ) : (
        <div style={{ display: 'grid', gap: 4, padding: 4 }}>
          {recentlyResolved.map((a) => {
            const c = SEVERITY_COLOR[a.severity];
            return (
              <div
                key={a.id}
                style={{
                  padding: '6px 8px',
                  borderLeft: `2px solid ${c}55`,
                  fontFamily: 'var(--font-mono, monospace)',
                  fontSize: 11,
                  opacity: 0.85,
                }}
              >
                <div style={{ display: 'flex', gap: 6, alignItems: 'baseline' }}>
                  <HudChip color={c}>{a.severity.toUpperCase()}</HudChip>
                  <span style={{ color: c }}>{a.service_id}</span>
                  <span style={{ opacity: 0.7 }}>{a.kind}</span>
                  <span style={{ marginLeft: 'auto', opacity: 0.6, fontSize: 10 }}>
                    resolved {fmtDate(a.resolved_at)}
                  </span>
                </div>
                <div style={{ marginTop: 2, opacity: 0.8 }}>{a.message}</div>
              </div>
            );
          })}
        </div>
      )}
    </HudPanel>
  );
}

/* ---- Page root ---- */
export default function SecurityPage() {
  const [alerts, setAlerts] = useState<StoredAlert[]>([]);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    setBusy(true);
    try {
      // Pull both states so we can populate the audit trail and the
      // open list from the same fetch. 100 rows is enough headroom.
      setAlerts(await listPlatformAlerts(undefined, 100));
    } finally {
      setBusy(false);
    }
  }, []);
  useEffect(() => {
    void load();
    const t = window.setInterval(() => { void load(); }, 20_000);
    return () => window.clearInterval(t);
  }, [load]);

  const onResolve = useCallback(async (id: number) => {
    await resolveAlert(id);
    await load();
  }, [load]);

  const open = alerts.filter((a) => a.state === 'open');
  const trustScore = trustFromAlerts(open);
  const { color, status } = useMemo(() => trustPalette(trustScore), [trustScore]);
  const critical = open.filter((a) => a.severity === 'critical' || a.severity === 'p1').length;
  const warning  = open.filter((a) => a.severity === 'warning').length;
  const info     = open.filter((a) => a.severity === 'info').length;

  return (
    <div className={hudStyles.page}>
      <HudSummaryStrip
        title="Security · operator console"
        subtitle={`Trust score ${trustScore}/100 · ${status} · ${open.length} active alerts (${critical} critical · ${warning} warning · ${info} info)`}
        gaugeValue={trustScore / 100}
        gaugeReadout={`${trustScore}`}
        gaugeLabel="TRUST"
        gaugeColor={color}
        segments={[
          { label: 'Critical', value: critical, color: '#ff3355' },
          { label: 'Warning',  value: warning,  color: '#ffaa00' },
          { label: 'Info',     value: info,     color: '#7cc6ff' },
        ]}
        extra={
          <button
            type="button"
            onClick={() => void load()}
            disabled={busy}
            style={{
              background: 'transparent',
              color: '#00f0ff',
              border: '1px solid #00f0ff66',
              padding: '4px 10px',
              borderRadius: 4,
              fontSize: 11,
              fontFamily: 'inherit',
              cursor: busy ? 'wait' : 'pointer',
            }}
          >
            <Clock size={12} style={{ marginRight: 4, verticalAlign: 'text-top' }} />
            refresh
          </button>
        }
      />

      <div className={styles.topRow} style={{ marginTop: 12 }}>
        <HudPanel
          title="Trust Score"
          accent={color}
          leading={<HudStatusLed color={color} />}
          meta={<ShieldCheck size={11} />}
          footer={<>// composite of severity-weighted open alerts</>}
        >
          <div className={styles.gaugeWrap}>
            <HudGauge
              value={trustScore / 100}
              readout={`${trustScore}`}
              label={status.toUpperCase()}
              color={color}
              size={200}
            />
          </div>
        </HudPanel>

        <OpenAlertsTile alerts={alerts} onResolve={onResolve} />
      </div>

      <div style={{ marginTop: 12 }}>
        <AssetChipRow alerts={alerts} />
      </div>

      <div style={{ marginTop: 12 }}>
        <AuditTrail alerts={alerts} />
      </div>
    </div>
  );
}
