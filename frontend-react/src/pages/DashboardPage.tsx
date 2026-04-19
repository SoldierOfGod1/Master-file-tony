/* ============================================================
   DashboardPage — Neon Hex Command Deck.
   Ported from Canva Variant A (see public/mockups/02-variant-a-hex.png).
   6 hex KPI tiles arranged 3-on-each-side of a central LIVE orb.
   ============================================================ */

import { Link } from 'react-router-dom';
import {
  Bot,
  ListChecks,
  Coins,
  DollarSign,
  ArrowUpCircle,
  AlertTriangle,
  TrendingUp,
  TrendingDown,
  Minus,
  Wrench,
  Activity,
  Terminal,
  Shield,
  type LucideIcon,
} from 'lucide-react';
import { useCommandCentre } from '../context/CommandCentreContext';
import GlassCard from '../components/shared/GlassCard';
import QualityGatePanel from './QualityGatePanel';
import type { KpiEntry } from '../types/api';
import styles from './DashboardPage.module.css';

interface KpiCardConfig {
  key: keyof NonNullable<ReturnType<typeof useCommandCentre>['state']['kpis']>;
  label: string;
  icon: LucideIcon;
  format: (v: number) => string;
}

// Tile order: first three render on the left side, last three on the right.
// Middle column is the central LIVE orb.
const KPI_CARDS: readonly KpiCardConfig[] = [
  { key: 'activeAgents', label: 'Active Agents', icon: Bot, format: (v) => String(v) },
  { key: 'tasksInFlight', label: 'Tasks In-Flight', icon: ListChecks, format: (v) => String(v) },
  { key: 'tokensToday', label: 'Tokens Today', icon: Coins, format: (v) => v >= 1000 ? `${(v / 1000).toFixed(1)}K` : String(v) },
  { key: 'costToday', label: 'Cost Today', icon: DollarSign, format: (v) => `R${v.toFixed(2)}` },
  { key: 'uptime', label: 'Uptime', icon: ArrowUpCircle, format: (v) => `${v}%` },
  { key: 'errorRate', label: 'Error Rate', icon: AlertTriangle, format: (v) => `${v}%` },
] as const;

interface SummaryCardConfig {
  to: string;
  label: string;
  icon: LucideIcon;
  count: (state: ReturnType<typeof useCommandCentre>['state']) => string;
}

const SUMMARY_CARDS: readonly SummaryCardConfig[] = [
  { to: '/agents', label: 'Agent Fleet', icon: Bot, count: (s) => `${s.agents.length} agents` },
  { to: '/tasks', label: 'Task Board', icon: ListChecks, count: (s) => `${s.tasks.length} tasks` },
  { to: '/feed', label: 'Activity Feed', icon: Activity, count: (s) => `${s.feed.length} events` },
  { to: '/tools', label: 'Tools Hub', icon: Wrench, count: (s) => `${s.tools.length} tools` },
  { to: '/health', label: 'System Health', icon: Activity, count: () => 'Live' },
  { to: '/logs', label: 'Log Terminal', icon: Terminal, count: (s) => `${s.logs.length} entries` },
  { to: '/costs', label: 'Cost Analytics', icon: DollarSign, count: (s) => s.costs ? `R${s.costs.total.toFixed(2)}` : '--' },
  { to: '/security', label: 'Security', icon: Shield, count: (s) => s.security ? `Score: ${s.security.trustScore}` : '--' },
] as const;

function TrendArrow({ trend }: { readonly trend: KpiEntry['trend'] }) {
  if (trend === 'up') {
    return (
      <span className={`${styles.hexTrend} ${styles.trendUp}`}>
        <TrendingUp size={10} /> Up
      </span>
    );
  }
  if (trend === 'down') {
    return (
      <span className={`${styles.hexTrend} ${styles.trendDown}`}>
        <TrendingDown size={10} /> Down
      </span>
    );
  }
  return (
    <span className={`${styles.hexTrend} ${styles.trendFlat}`}>
      <Minus size={10} /> Flat
    </span>
  );
}

/* One hexagonal KPI tile. Wrapped in a div so we can apply clip-path without
   interfering with the holographic frame mask used elsewhere. */
function HexTile({ cfg, entry }: {
  readonly cfg: KpiCardConfig;
  readonly entry: KpiEntry | undefined;
}) {
  return (
    <div className={styles.hex}>
      <span className={styles.hexValue}>
        {entry ? cfg.format(entry.value) : '--'}
      </span>
      <span className={styles.hexLabel2}>{cfg.label}</span>
      {entry && <TrendArrow trend={entry.trend} />}
    </div>
  );
}

export default function DashboardPage() {
  const { state } = useCommandCentre();
  const { kpis } = state;

  const left = KPI_CARDS.slice(0, 3);
  const right = KPI_CARDS.slice(3, 6);

  return (
    <div className={styles.page}>
      <h2 className={styles.pageTitle}>Command Centre Overview</h2>

      <div className={styles.hexDeck}>
        <span className={styles.hexLabel}>Dashboard</span>
        <span className={`${styles.hexLabel} ${styles.hexLabelBottom}`}>
          Mission Control · Live Telemetry
        </span>

        <div className={styles.hexRing}>
          <div className={styles.hexLeft}>
            {left.map((cfg) => (
              <HexTile key={cfg.key} cfg={cfg} entry={kpis?.[cfg.key]} />
            ))}
          </div>

          <div className={styles.hexCenter}>
            <div className={styles.orb}>
              <div className={styles.orbInner}>
                <span className={styles.orbText}>LIVE</span>
              </div>
            </div>
          </div>

          <div className={styles.hexRight}>
            {right.map((cfg) => (
              <HexTile key={cfg.key} cfg={cfg} entry={kpis?.[cfg.key]} />
            ))}
          </div>
        </div>
      </div>

      <div style={{ margin: '24px 0' }}>
        <QualityGatePanel />
      </div>

      <div className={styles.summaryGrid}>
        {SUMMARY_CARDS.map((card) => {
          const Icon = card.icon;
          return (
            <Link key={card.to} to={card.to} className={styles.summaryLink}>
              <GlassCard className={`${styles.summaryCard} holoFrame`}>
                <div className={styles.summaryCardInner}>
                  <Icon size={24} className={styles.summaryIcon} />
                  <span className={styles.summaryLabel}>{card.label}</span>
                  <span className={styles.summaryCount}>{card.count(state)}</span>
                </div>
              </GlassCard>
            </Link>
          );
        })}
      </div>
    </div>
  );
}
