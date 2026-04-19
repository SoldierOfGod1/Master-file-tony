/* ============================================================
   SystemHealthPage — one HUD panel per metric, each with a
   big segmented gauge.
   ============================================================ */

import { useMemo } from 'react';
import { Cpu, Database, Wifi, HeartPulse } from 'lucide-react';
import { useCommandCentre } from '../context/CommandCentreContext';
import HudGauge from '../components/shared/HudGauge';
import HudPanel from '../components/shared/HudPanel';
import HudSummaryStrip from '../components/shared/HudSummaryStrip';
import { HudStatusLed } from '../components/shared/HudChip';
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
                  readout={health ? `${value}${m.unit}` : '--'}
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
