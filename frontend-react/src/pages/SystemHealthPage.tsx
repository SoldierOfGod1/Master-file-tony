/* ============================================================
   SystemHealthPage — one HUD panel per metric, each with a
   big segmented gauge.
   ============================================================ */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { Cpu, Database, Wifi, HeartPulse, Flame, AlertTriangle } from 'lucide-react';
import { useCommandCentre } from '../context/CommandCentreContext';
import HudGauge from '../components/shared/HudGauge';
import HudPanel from '../components/shared/HudPanel';
import HudSummaryStrip from '../components/shared/HudSummaryStrip';
import { HudChip, HudStatusLed } from '../components/shared/HudChip';
import {
  listDatabaseHealth, listPlatformIncidents,
  type DatabaseHealth, type Incident,
} from '../api/platforms';
import hudStyles from '../theme/hud.module.css';
import styles from './SystemHealthPage.module.css';
import type { LucideIcon } from 'lucide-react';

/* Thresholds → LED + accent colour. Red once over 80 %, amber over 60 %. */
function colourFor(value: number): { accent: string; led: string } {
  if (value > 80) return { accent: '#ff3355', led: '#ff3355' };
  if (value > 60) return { accent: '#ffaa00', led: '#ffaa00' };
  return { accent: '#6ff2a0', led: '#6ff2a0' };
}

interface MetricConfig {
  readonly key: 'cpu' | 'memory' | 'network';
  readonly title: string;
  readonly icon: LucideIcon;
  readonly unit: string;
  /** If true, lower is better — inverts threshold colouring. */
  readonly treatAsAbsolute?: boolean;
}

const METRICS: readonly MetricConfig[] = [
  { key: 'cpu',     title: 'CPU',         icon: Cpu,      unit: '%' },
  { key: 'memory',  title: 'Memory',      icon: Database, unit: '%' },
  { key: 'network', title: 'Network I/O', icon: Wifi,     unit: 'Mbps', treatAsAbsolute: true },
] as const;

export default function SystemHealthPage() {
  const { state } = useCommandCentre();
  const health = state.health;

  // Surface the three monitoring streams that run in the background
  // but used to have no UI here. Polls are cheap (cached snapshots
  // in the backend), refreshed every 30s alongside the CPU/memory
  // panel.
  const [dbs, setDbs] = useState<DatabaseHealth[]>([]);
  const [incidents, setIncidents] = useState<Incident[]>([]);
  const refresh = useCallback(async () => {
    const [d, i] = await Promise.all([
      listDatabaseHealth(),
      listPlatformIncidents(10),
    ]);
    setDbs(d);
    setIncidents(i);
  }, []);
  useEffect(() => {
    void refresh();
    const t = window.setInterval(() => { void refresh(); }, 30_000);
    return () => window.clearInterval(t);
  }, [refresh]);
  const openIncidents = incidents.filter((i) => i.state !== 'resolved');
  const p1Open = openIncidents.filter((i) => i.severity === 'p1').length;

  const fleetHealth = useMemo(() => {
    if (!health) return { readout: '--', ratio: 0 };
    // Composite score: avg of (100 - cpu) and (100 - memory), higher = healthier.
    const score = Math.max(0, Math.min(100, 100 - (health.cpu + health.memory) / 2));
    return { readout: `${Math.round(score)}`, ratio: score / 100 };
  }, [health]);

  return (
    <div className={hudStyles.page}>
      <HudSummaryStrip
        title="System Health · Live Telemetry"
        subtitle="WebSocket-driven · refresh rate 1s"
        gaugeValue={fleetHealth.ratio}
        gaugeReadout={fleetHealth.readout}
        gaugeLabel="HEALTH"
        gaugeColor={fleetHealth.ratio > 0.5 ? '#6ff2a0' : '#ffaa00'}
      />

      {/* DB health + Incidents — the two streams the backend was
          already running but never surfaced on this page. Keeps
          the heavy monitoring console on /service while letting
          /health answer "is the monitoring itself alive?". */}
      <div style={{
        display: 'grid', gap: 12, marginBottom: 12,
        // auto-fit with a 420px minimum: two columns on desktop,
        // collapses to a single column below ~900px so the DB +
        // Incident panels keep their readable width on laptops.
        gridTemplateColumns: 'repeat(auto-fit, minmax(min(420px, 100%), 1fr))',
      }}>
        <HudPanel
          title={`Database Health · ${dbs.length}`}
          accent={dbs.some((d) => !d.reachable) ? '#ff7b7b' : '#6ff2a0'}
          icon={<Database size={12} />}
          leading={<HudStatusLed color={dbs.some((d) => !d.reachable) ? '#ff7b7b' : '#6ff2a0'} />}
          meta={<HudChip color="#7cc6ff">{dbs.filter((d) => d.reachable).length}/{dbs.length} up</HudChip>}
        >
          {dbs.length === 0 ? (
            <div style={{ padding: 10, fontSize: 11, opacity: 0.65 }}>
              // no DB connections configured yet
            </div>
          ) : (
            <div style={{ display: 'grid', gap: 4, padding: 4 }}>
              {dbs.map((d) => {
                const c = d.reachable ? '#6ff2a0' : '#ff3355';
                return (
                  <div key={d.id} style={{
                    display: 'grid', gridTemplateColumns: '1fr auto auto',
                    gap: 6, padding: '4px 8px', alignItems: 'baseline',
                    borderLeft: `2px solid ${c}55`,
                    fontFamily: 'var(--font-mono, monospace)', fontSize: 11,
                  }}>
                    <span>
                      <span style={{ color: c }}>{d.label}</span>
                      {d.is_axiom && <span style={{ fontSize: 9, opacity: 0.7, marginLeft: 5 }}>P1</span>}
                    </span>
                    <span style={{ fontSize: 10, opacity: 0.75 }}>
                      {d.reachable ? `${d.query_ms}ms` : 'down'}
                    </span>
                    <HudChip color={c}>{d.reachable ? 'UP' : 'DOWN'}</HudChip>
                  </div>
                );
              })}
            </div>
          )}
        </HudPanel>

        <HudPanel
          title={`Open Incidents · ${openIncidents.length}`}
          accent={p1Open > 0 ? '#ff3355' : openIncidents.length > 0 ? '#ffaa00' : '#6ff2a0'}
          icon={<Flame size={12} />}
          leading={<HudStatusLed
            color={p1Open > 0 ? '#ff3355' : openIncidents.length > 0 ? '#ffaa00' : '#6ff2a0'}
            animate={p1Open > 0} />}
          meta={
            p1Open > 0
              ? <HudChip color="#ff3355">{p1Open} P1</HudChip>
              : openIncidents.length === 0
                ? <HudChip color="#6ff2a0">all clear</HudChip>
                : null
          }
        >
          {openIncidents.length === 0 ? (
            <div style={{ padding: 10, fontSize: 11, opacity: 0.65 }}>
              // no open incidents
            </div>
          ) : (
            <div style={{ display: 'grid', gap: 4, padding: 4 }}>
              {openIncidents.slice(0, 6).map((i) => {
                const c =
                  i.severity === 'p1' ? '#ff3355' :
                  i.severity === 'critical' ? '#ff7b7b' :
                  i.severity === 'warning' ? '#ffaa00' :
                  '#7cc6ff';
                return (
                  <div key={i.id} style={{
                    padding: '5px 8px', borderLeft: `2px solid ${c}`,
                    fontFamily: 'var(--font-mono, monospace)', fontSize: 11,
                  }}>
                    <div style={{ display: 'flex', gap: 5, alignItems: 'baseline' }}>
                      <HudChip color={c}>{i.severity.toUpperCase()}</HudChip>
                      <span style={{ opacity: 0.9 }}>{i.service_id}</span>
                      <span style={{ fontSize: 9, opacity: 0.6, marginLeft: 'auto' }}>{i.state}</span>
                    </div>
                    <div style={{ fontSize: 10, opacity: 0.8, marginTop: 2 }}>{i.title}</div>
                  </div>
                );
              })}
              {openIncidents.length > 6 && (
                <div style={{ fontSize: 9, opacity: 0.6, padding: '2px 8px' }}>
                  + {openIncidents.length - 6} more · open <a href="/service" style={{ color: '#00f0ff' }}>rain Service</a>
                </div>
              )}
            </div>
          )}
        </HudPanel>
      </div>

      <div className={hudStyles.grid}>
        {METRICS.map((m) => {
          const value = health?.[m.key] ?? 0;
          const pct = m.treatAsAbsolute ? Math.min(value / 100, 1) : value / 100;
          const { accent, led } = m.treatAsAbsolute
            ? { accent: '#0077c8', led: '#7cc6ff' }
            : colourFor(value);
          const Icon = m.icon;
          return (
            <HudPanel
              key={m.key}
              title={m.title}
              accent={accent}
              leading={<HudStatusLed color={led} />}
              meta={<><HeartPulse size={10} /> live</>}
              footer={<div className={styles.footerLine}><Icon size={12} /> {m.title} {health ? 'nominal' : 'awaiting data'}</div>}
            >
              <div className={styles.body}>
                <HudGauge
                  value={pct}
                  label={m.title.toUpperCase()}
                  readout={health ? `${Number(value).toFixed(2)}${m.unit}` : '--'}
                  color={accent}
                  size={180}
                />
              </div>
            </HudPanel>
          );
        })}
      </div>
    </div>
  );
}
