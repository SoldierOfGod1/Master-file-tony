/* ============================================================
   rain Dark NOC — autonomous network operations HUD.
   Composes existing tiles (alerts, DB health, incident timeline)
   plus a Grafana iframe for the operator's `--cEk8A4k` dashboard,
   plus the 41-agent reference registry from DarkNoc.md.
   Cybertron is a chat tool, not a UI; the Cybertron card just
   tells the operator how to reach it.
   ============================================================ */

import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  AlertTriangle,
  Bot,
  Cpu,
  ExternalLink,
  Flame,
  Layers,
  Network,
  RefreshCw,
  Sparkles,
} from 'lucide-react';
import HudPanel from '../components/shared/HudPanel';
import HudSummaryStrip from '../components/shared/HudSummaryStrip';
import { HudChip, HudStatusLed } from '../components/shared/HudChip';
import {
  getDarkNocConfig,
  getDarkNocOverview,
  listDarkNocFaults,
  listDarkNocRegistry,
  type DarkNocConfig,
  type DarkNocFault,
  type DarkNocOverview,
  type DarkNocRegistryAgent,
} from '../api/darknoc';
import hudStyles from '../theme/hud.module.css';

const SEVERITY_COLOR: Record<string, string> = {
  critical: '#ff3355',
  warning:  '#ffaa00',
  info:     '#7cc6ff',
};
const sevColor = (s: string): string => SEVERITY_COLOR[s.toLowerCase()] ?? '#7cc6ff';

function fmtTime(iso: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  const now = Date.now();
  const diff = Math.max(0, Math.floor((now - d.getTime()) / 1000));
  if (diff < 60) return `${diff}s ago`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  return d.toLocaleString();
}

/* ---- Disabled banner ---- */
function DisabledBanner() {
  return (
    <HudPanel
      title="rain Dark NOC · disabled"
      accent="#ffaa00"
      icon={<Network size={12} />}
      leading={<HudStatusLed color="#ffaa00" animate={false} />}
    >
      <div style={{ padding: 14, fontSize: 13, lineHeight: 1.55 }}>
        <p style={{ margin: 0 }}>
          Set <code>DARK_NOC_ENABLED=true</code> in the backend environment to
          turn this tab on. Configure two connections in Settings:
        </p>
        <ul style={{ marginTop: 8, paddingLeft: 20 }}>
          <li>
            <strong>clickhouse-prod</strong> — driver <code>clickhouse</code>,
            host + user + password + database for the rain telemetry cluster.
            Used by the fault telemetry tile and the Cybertron chat tool.
          </li>
          <li>
            <strong>grafana-prod</strong> — driver <code>grafana</code>, host =
            base URL (<code>https://grafana.rain.network</code>), password = a
            service-account token with Viewer role. Used by Cybertron to read
            panel metadata from dashboard <code>--cEk8A4k</code>.
          </li>
        </ul>
        <p style={{ marginTop: 10, opacity: 0.7, fontSize: 11 }}>
          // when both connections are configured and{' '}
          <code>DARK_NOC_ENABLED=true</code> is set, the page renders the live
          HUD on next refresh.
        </p>
      </div>
    </HudPanel>
  );
}

/* ---- Outage timeline strip ---- */
function OverviewStrip({ overview }: { readonly overview: DarkNocOverview | null }) {
  if (!overview) {
    return (
      <HudPanel
        title="Network HUD · loading"
        accent="#7cc6ff"
        leading={<HudStatusLed color="#7cc6ff" />}
      >
        <div style={{ padding: 12, opacity: 0.6, fontSize: 12 }}>
          // querying ClickHouse…
        </div>
      </HudPanel>
    );
  }
  const trust = overview.network_trust_score;
  const trustColor = trust >= 80 ? '#6ff2a0' : trust >= 50 ? '#ffaa00' : '#ff3355';
  const sourceColor = overview.source === 'clickhouse' ? '#6ff2a0' : '#ffaa00';
  return (
    <HudPanel
      title="Network HUD"
      accent={trustColor}
      icon={<Cpu size={12} />}
      leading={<HudStatusLed color={trustColor} animate={trust < 80} />}
      meta={<HudChip color={sourceColor}>{overview.source}</HudChip>}
    >
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(140px, 1fr))', gap: 10, padding: 10 }}>
        <Kpi label="Trust score" value={`${trust}/100`} color={trustColor} />
        <Kpi label="Faults · 24h" value={`${overview.faults_last_24h}`} color="#7cc6ff" />
        <Kpi label="Critical · 24h" value={`${overview.critical_faults_24h}`} color="#ff3355" />
        <Kpi label="Active slices" value={`${overview.active_slices}`} color="#00f0ff" />
        <Kpi label="SLA breaching" value={`${overview.slices_breaching_sla}`} color={overview.slices_breaching_sla > 0 ? '#ff3355' : '#6ff2a0'} />
        <Kpi label="Source latency" value={`${overview.source_latency_ms}ms`} color="#7cc6ff" />
      </div>
      {overview.note && (
        <div style={{ padding: '0 12px 10px', fontSize: 11, color: '#ffaa00' }}>
          // {overview.note}
        </div>
      )}
    </HudPanel>
  );
}

function Kpi({ label, value, color }: { readonly label: string; readonly value: string; readonly color: string }) {
  return (
    <div style={{ padding: 8, border: `1px solid ${color}33`, borderRadius: 4, background: 'rgba(0,0,0,0.2)' }}>
      <div style={{ fontSize: 9, opacity: 0.6, letterSpacing: '0.06em', textTransform: 'uppercase' }}>{label}</div>
      <div style={{ fontSize: 22, color, marginTop: 2, fontFamily: 'var(--font-mono, monospace)' }}>{value}</div>
    </div>
  );
}

/* ---- Faults list (last 50) ---- */
function FaultsTile({ faults }: { readonly faults: DarkNocFault[] }) {
  return (
    <HudPanel
      title={`Faults · ${faults.length}`}
      accent="#ff3355"
      icon={<Flame size={12} />}
      leading={<HudStatusLed color={faults.length > 0 ? '#ff3355' : '#6ff2a0'} animate={faults.length > 0} />}
    >
      {faults.length === 0 && (
        <div style={{ padding: 14, fontSize: 12, opacity: 0.7 }}>
          // no faults in the last 24h — quiet network
        </div>
      )}
      <div style={{ display: 'grid', gap: 4, padding: 4, maxHeight: 360, overflowY: 'auto' }}>
        {faults.map((f) => {
          const c = sevColor(f.severity);
          return (
            <div
              key={f.id}
              style={{
                padding: '6px 8px',
                borderLeft: `2px solid ${c}`,
                fontFamily: 'var(--font-mono, monospace)',
                fontSize: 11,
              }}
            >
              <div style={{ display: 'flex', gap: 6, alignItems: 'baseline' }}>
                <HudChip color={c}>{f.severity.toUpperCase()}</HudChip>
                <span style={{ color: c }}>{f.source}</span>
                {f.technology && <HudChip color="#00f0ff">{f.technology}</HudChip>}
                {f.region && <span style={{ opacity: 0.7 }}>{f.region}</span>}
                <span style={{ marginLeft: 'auto', opacity: 0.6, fontSize: 10 }}>
                  {fmtTime(f.occurred_at)}
                </span>
              </div>
              <div style={{ marginTop: 2 }}>{f.title}</div>
              {f.detail && (
                <div style={{ marginTop: 1, fontSize: 10, opacity: 0.75 }}>{f.detail}</div>
              )}
            </div>
          );
        })}
      </div>
    </HudPanel>
  );
}

/* ---- Cybertron card ---- */
function CybertronCard() {
  return (
    <HudPanel
      title="Cybertron"
      subtitle="ask in chat — read-only. Gated by RAIN_SUPPORT_L2"
      accent="#c488ff"
      icon={<Bot size={12} />}
      leading={<HudStatusLed color="#c488ff" />}
    >
      <div style={{ padding: 12, fontSize: 12, lineHeight: 1.55 }}>
        <p style={{ margin: 0 }}>
          Cybertron is the Dark NOC orchestrator agent. Reach it from the
          {' '}<a href="/chat" style={{ color: '#c488ff' }}>Chat</a> tab — it
          knows three tools:
        </p>
        <ul style={{ marginTop: 8, paddingLeft: 20, opacity: 0.85 }}>
          <li><code>darknoc_overview</code> — current KPI bundle</li>
          <li><code>darknoc_faults</code> — last 50 faults</li>
          <li><code>darknoc_registry</code> — 41-agent reference roadmap</li>
        </ul>
        <p style={{ marginTop: 10, fontSize: 11, opacity: 0.6 }}>
          // try: "what's wrong with the 5G network?" or "show me critical
          faults in the last hour"
        </p>
      </div>
    </HudPanel>
  );
}

/* ---- Grafana iframe tile ---- */
function GrafanaTile({ dashUID }: { readonly dashUID: string }) {
  if (!dashUID) {
    return null;
  }
  // d-solo + kiosk renders just the panel chrome-free. The full
  // dashboard URL works too but loads the Grafana sidebar inside the
  // iframe; the d-solo route is cleaner.
  const src = `https://grafana.rain.network/d/${encodeURIComponent(dashUID)}/isoc-state-of-the-network?orgId=1&from=now-7d&to=now&kiosk&theme=dark&refresh=30s`;
  return (
    <HudPanel
      title="Grafana · isoc state of the network"
      subtitle="loaded via your browser's Azure AD session — backend never sees credentials"
      accent="#00f0ff"
      icon={<Network size={12} />}
      leading={<HudStatusLed color="#00f0ff" />}
      meta={
        <a
          href={`https://grafana.rain.network/d/${encodeURIComponent(dashUID)}/isoc-state-of-the-network`}
          target="_blank"
          rel="noreferrer"
          style={{ color: '#00f0ff', fontSize: 10, display: 'inline-flex', alignItems: 'center', gap: 3 }}
        >
          open <ExternalLink size={10} />
        </a>
      }
    >
      <iframe
        title="isoc state of the network"
        src={src}
        style={{
          width: '100%',
          height: 720,
          border: 'none',
          background: 'rgba(0,0,0,0.3)',
        }}
      />
    </HudPanel>
  );
}

/* ---- Reference registry (collapsed by default) ---- */
function ReferenceRegistry({ agents }: { readonly agents: DarkNocRegistryAgent[] }) {
  const [open, setOpen] = useState(false);
  const byDomain = useMemo(() => {
    const m = new Map<string, DarkNocRegistryAgent[]>();
    for (const a of agents) {
      const arr = m.get(a.domain) ?? [];
      arr.push(a);
      m.set(a.domain, arr);
    }
    return Array.from(m.entries()).sort((a, b) => b[1].length - a[1].length);
  }, [agents]);

  return (
    <HudPanel
      title={`Reference registry · ${agents.length} agents`}
      subtitle="Capgemini Open Registry — ROADMAP REFERENCE, not live at rain"
      accent="#7cc6ff"
      icon={<Layers size={12} />}
      leading={<HudStatusLed color="#7cc6ff" animate={false} />}
      meta={
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          style={{
            background: 'transparent',
            border: '1px solid #7cc6ff66',
            color: '#7cc6ff',
            padding: '2px 8px',
            fontSize: 10,
            cursor: 'pointer',
            borderRadius: 2,
            fontFamily: 'inherit',
          }}
        >
          {open ? 'hide' : 'show'}
        </button>
      }
    >
      {!open ? (
        <div style={{ padding: 12, opacity: 0.6, fontSize: 11 }}>
          // {agents.length} agents grouped by {byDomain.length} domains. Click "show" to expand.
        </div>
      ) : (
        <div style={{ padding: 8, display: 'grid', gap: 12 }}>
          {byDomain.map(([domain, list]) => (
            <div key={domain}>
              <div style={{ fontSize: 10, opacity: 0.6, letterSpacing: '0.06em', textTransform: 'uppercase', marginBottom: 4 }}>
                {domain} · {list.length}
              </div>
              <div style={{ display: 'grid', gap: 4 }}>
                {list.map((a) => (
                  <div
                    key={`${a.domain}::${a.name}`}
                    style={{
                      padding: '6px 8px',
                      borderLeft: '2px solid #7cc6ff44',
                      fontSize: 11,
                    }}
                  >
                    <div style={{ display: 'flex', gap: 6, alignItems: 'baseline' }}>
                      <strong>{a.name}</strong>
                      {a.category && <HudChip color="#00f0ff">{a.category}</HudChip>}
                      {a.use_case && <HudChip color="#6ff2a0">{a.use_case}</HudChip>}
                      {a.protocol && <span style={{ opacity: 0.6, fontSize: 10 }}>{a.protocol}</span>}
                    </div>
                    {a.summary && (
                      <div style={{ marginTop: 2, opacity: 0.8, fontSize: 11 }}>{a.summary}</div>
                    )}
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      )}
    </HudPanel>
  );
}

/* ---- Page root ---- */
export default function DarkNocPage() {
  const [config, setConfig] = useState<DarkNocConfig | null>(null);
  const [overview, setOverview] = useState<DarkNocOverview | null>(null);
  const [faults, setFaults] = useState<DarkNocFault[]>([]);
  const [registry, setRegistry] = useState<DarkNocRegistryAgent[]>([]);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    setBusy(true);
    try {
      const cfg = await getDarkNocConfig();
      setConfig(cfg);
      // Always load registry — it's static reference and harmless.
      setRegistry(await listDarkNocRegistry());
      if (cfg.enabled) {
        const [o, f] = await Promise.all([getDarkNocOverview(), listDarkNocFaults()]);
        setOverview(o);
        setFaults(f);
      } else {
        setOverview(null);
        setFaults([]);
      }
    } finally {
      setBusy(false);
    }
  }, []);

  useEffect(() => {
    void load();
    // Refresh KPIs every 30s — matches ClickHouse cache TTL on the
    // backend, so we don't pay double for repeated fetches.
    const t = window.setInterval(() => { void load(); }, 30_000);
    return () => window.clearInterval(t);
  }, [load]);

  const enabled = config?.enabled ?? false;
  const dashUID = config?.grafana_dashboard_uid ?? '';
  const trust = overview?.network_trust_score ?? 0;
  const trustColor = trust >= 80 ? '#6ff2a0' : trust >= 50 ? '#ffaa00' : '#ff3355';

  return (
    <div className={hudStyles.page}>
      <HudSummaryStrip
        title="rain Dark NOC · autonomous network operations"
        subtitle={
          enabled
            ? `Cybertron orchestrator · trust score ${trust}/100 · ${faults.length} faults · ${registry.length} reference agents`
            : 'set DARK_NOC_ENABLED=true on the backend to enable'
        }
        gaugeValue={enabled ? trust / 100 : 0}
        gaugeReadout={enabled ? `${trust}` : '—'}
        gaugeLabel={enabled ? 'TRUST' : 'OFF'}
        gaugeColor={enabled ? trustColor : '#7cc6ff'}
        extra={
          <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
            <button
              type="button"
              onClick={() => void load()}
              disabled={busy}
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 4,
                padding: '4px 10px',
                background: 'transparent',
                color: '#00f0ff',
                border: '1px solid #00f0ff66',
                borderRadius: 4,
                cursor: busy ? 'wait' : 'pointer',
                fontSize: 11,
                fontFamily: 'inherit',
              }}
            >
              <RefreshCw size={12} />
              Refresh
            </button>
            <Sparkles size={20} style={{ color: '#c488ff' }} />
          </div>
        }
      />

      <div style={{ display: 'flex', flexDirection: 'column', gap: 12, marginTop: 12 }}>
        {!enabled && <DisabledBanner />}

        {enabled && (
          <>
            <OverviewStrip overview={overview} />

            <div
              style={{
                display: 'grid',
                gap: 12,
                gridTemplateColumns: 'repeat(auto-fit, minmax(min(420px, 100%), 1fr))',
              }}
            >
              <FaultsTile faults={faults} />
              <CybertronCard />
            </div>

            <GrafanaTile dashUID={dashUID} />
          </>
        )}

        {/* Registry is always shown — even when disabled it's useful
            as a research surface. */}
        {registry.length > 0 && <ReferenceRegistry agents={registry} />}

        {!enabled && registry.length === 0 && (
          <HudPanel
            title="Reference registry"
            accent="#7cc6ff"
            leading={<HudStatusLed color="#7cc6ff" animate={false} />}
            icon={<AlertTriangle size={12} />}
          >
            <div style={{ padding: 12, fontSize: 12, opacity: 0.7 }}>
              // could not load DarkNoc.md — registry is empty. Place the file at
              <code> ~/Downloads/DarkNoc.md</code> and refresh.
            </div>
          </HudPanel>
        )}
      </div>
    </div>
  );
}
