/* ============================================================
   Agent Office — grid of monitor-panel cards, one per zone.
   Built from shared HUD primitives so every other page can
   reuse the same aesthetic.
   ============================================================ */

import { useMemo } from 'react';
import { Building2, Radar, Signal, Activity } from 'lucide-react';
import GlassCard from '../components/shared/GlassCard';
import HudGauge from '../components/shared/HudGauge';
import HudPanel from '../components/shared/HudPanel';
import { HudChip, HudStatusLed } from '../components/shared/HudChip';
import HudSummaryStrip from '../components/shared/HudSummaryStrip';
import { useCommandCentre } from '../context/CommandCentreContext';
import type { AgentOfficeState, OfficeZone } from '../types/api';
import hudStyles from '../theme/hud.module.css';
import styles from './OfficePage.module.css';

interface Palette {
  readonly primary: string;
  readonly glow: string;
  readonly label: string;
}

const ACTIVITY: Record<string, Palette> = {
  idle:      { primary: '#7cc6ff', glow: 'rgba(0, 240, 255, 0.65)', label: 'Idle' },
  coding:    { primary: '#6ff2a0', glow: 'rgba(0, 255, 136, 0.7)',  label: 'Coding' },
  reviewing: { primary: '#ffc566', glow: 'rgba(255, 170, 0, 0.7)',  label: 'Reviewing' },
  testing:   { primary: '#ff7de0', glow: 'rgba(255, 0, 229, 0.7)',  label: 'Testing' },
};

const paletteFor = (a: string): Palette =>
  ACTIVITY[a.toLowerCase()] ?? ACTIVITY.idle;

const fmtZone = (name: string): string => name.replace(/-/g, ' ').toUpperCase();

const short = (s: string | undefined, max: number = 42): string =>
  !s ? '' : s.length <= max ? s : s.slice(0, max - 1) + '…';

function AgentRow({ agent }: { readonly agent: AgentOfficeState }) {
  const pal = paletteFor(agent.activity);
  return (
    <div className={hudStyles.row}>
      <div className={hudStyles.rowDot} style={{ background: pal.primary, boxShadow: `0 0 6px ${pal.glow}` }} />
      <span className={hudStyles.rowName}>{agent.name ?? agent.id}</span>
      <HudChip color={pal.primary}>{agent.activity}</HudChip>
    </div>
  );
}

function RoomPanel({ zone, agents }: {
  readonly zone: OfficeZone;
  readonly agents: readonly AgentOfficeState[];
}) {
  const total = agents.length;
  const active = agents.filter((a) => a.activity.toLowerCase() !== 'idle').length;
  const ratio = total === 0 ? 0 : active / total;
  const color = zone.color || '#00f0ff';

  const activityCounts = agents.reduce<Record<string, number>>((acc, a) => {
    const k = a.activity.toLowerCase();
    acc[k] = (acc[k] ?? 0) + 1;
    return acc;
  }, {});
  const dominant = Object.entries(activityCounts).sort((a, b) => b[1] - a[1])[0]?.[0];
  const gaugeColor = dominant ? paletteFor(dominant).primary : color;

  const tickerLines = agents
    .filter((a) => a.lastAction)
    .slice(0, 3)
    .map((a) => `${a.name ?? a.id}: ${short(a.lastAction, 34)}`);

  const footer = (
    <div className={styles.footerTicker}>
      <Activity size={10} className={styles.tickerIcon} />
      <div className={styles.ticker}>
        {tickerLines.length === 0 ? (
          <span className={styles.tickerIdle}>// standing by</span>
        ) : (
          tickerLines.map((l, i) => <span key={i} className={styles.tickerLine}>{l}</span>)
        )}
      </div>
    </div>
  );

  return (
    <HudPanel
      title={fmtZone(zone.name)}
      accent={color}
      leading={<HudStatusLed color={active > 0 ? '#6ff2a0' : '#7cc6ff'} />}
      meta={<><Signal size={10} /> {total}</>}
      footer={footer}
    >
      <div className={styles.panelBody}>
        <HudGauge
          value={ratio}
          label="ACTIVE"
          readout={total === 0 ? '--' : `${Math.round(ratio * 100)}`}
          color={gaugeColor}
        />
        <div className={styles.roster}>
          {total === 0 ? (
            <div className={styles.rosterEmpty}>No agents stationed.</div>
          ) : (
            agents.map((a) => <AgentRow key={a.id} agent={a} />)
          )}
        </div>
      </div>
    </HudPanel>
  );
}

export default function OfficePage() {
  const { state } = useCommandCentre();
  const officeData = state.office;
  const zones = officeData?.zones ?? [];
  const agents = officeData?.agents ?? [];

  const agentsByZone = useMemo(() => {
    const m = new Map<string, AgentOfficeState[]>();
    for (const a of agents) {
      const arr = m.get(a.zone) ?? [];
      arr.push(a);
      m.set(a.zone, arr);
    }
    return m;
  }, [agents]);

  const activeCount = useMemo(
    () => agents.filter((a) => a.activity.toLowerCase() !== 'idle').length,
    [agents],
  );
  const ratio = agents.length === 0 ? 0 : activeCount / agents.length;

  return (
    <div className={hudStyles.page}>
      <div className={styles.header}>
        <div className={styles.titleRow}>
          <Building2 size={24} className={styles.titleIcon} />
          <h1 className={hudStyles.pageTitle}>Agent Office</h1>
        </div>
      </div>

      {!officeData ? (
        <GlassCard>
          <div className={styles.empty}>
            <Radar size={48} className={styles.emptyIcon} />
            <span className={styles.emptyText}>Waiting for office data...</span>
          </div>
        </GlassCard>
      ) : (
        <>
          <HudSummaryStrip
            title="Mission Control · Office Grid"
            subtitle={`${zones.length} rooms · ${agents.length} agents · ${activeCount} active`}
            gaugeValue={ratio}
            gaugeReadout={`${activeCount}/${agents.length}`}
            segments={Object.entries(ACTIVITY).map(([key, p]) => ({
              label: p.label,
              value: agents.filter((a) => a.activity.toLowerCase() === key).length,
              color: p.primary,
            }))}
          />

          <div className={hudStyles.gridWide}>
            {zones.map((z) => (
              <RoomPanel
                key={z.id}
                zone={z}
                agents={agentsByZone.get(z.id) ?? []}
              />
            ))}
          </div>
        </>
      )}
    </div>
  );
}
