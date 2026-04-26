/* ============================================================
   CostAnalyticsPage — HUD breakdown with charts inside panels.
   ============================================================ */

import { Banknote, TrendingDown, TrendingUp } from 'lucide-react';
import { useCommandCentre } from '../context/CommandCentreContext';
import DonutChart from '../components/charts/DonutChart';
import BarChart from '../components/charts/BarChart';
import HudPanel from '../components/shared/HudPanel';
import HudSummaryStrip from '../components/shared/HudSummaryStrip';
import { HudChip, HudStatusLed } from '../components/shared/HudChip';
import hudStyles from '../theme/hud.module.css';
import styles from './CostAnalyticsPage.module.css';
import BudgetsTile from './BudgetsTile';

function formatRand(value: number): string {
  if (value >= 1_000_000) return `R${(value / 1_000_000).toFixed(2)}M`;
  if (value >= 1_000) return `R${(value / 1_000).toFixed(2)}K`;
  return `R${value.toFixed(2)}`;
}

export default function CostAnalyticsPage() {
  const { state } = useCommandCentre();
  const { costs } = state;

  const segments = costs?.models.map((m) => ({
    name: m.name,
    value: m.value,
    color: m.color,
  })) ?? [];

  const dailyData = costs?.daily ?? [];
  const total = costs?.total ?? 0;

  // Daily trend direction (last vs prev) — informs an up/down chip.
  // `daily` is a plain number[] on CostData, so compare numbers directly.
  const trend = dailyData.length >= 2
    ? dailyData[dailyData.length - 1] - dailyData[dailyData.length - 2]
    : 0;
  const trendUp = trend > 0;

  // Summary bar shows spend by model.
  const segmentData = segments.map((s) => ({
    label: s.name,
    value: s.value,
    color: s.color,
  }));

  return (
    <div className={hudStyles.page}>
      <HudSummaryStrip
        title="Cost Analytics"
        subtitle={`${formatRand(total)} total · ${segments.length} models tracked`}
        gaugeValue={total === 0 ? 0 : Math.min(total / 10000, 1)}
        gaugeReadout={formatRand(total)}
        gaugeLabel="SPEND"
        gaugeColor="#ff7de0"
        segments={segmentData}
        extra={
          <div className={styles.trendIcon}>
            {trendUp
              ? <TrendingUp size={22} style={{ color: '#ff7de0' }} />
              : <TrendingDown size={22} style={{ color: '#6ff2a0' }} />}
          </div>
        }
      />

      <div className={styles.topRow}>
        <HudPanel
          title="Model Breakdown"
          accent="#ff7de0"
          leading={<HudStatusLed color="#ff7de0" />}
          meta={<Banknote size={10} />}
        >
          <div className={styles.chartWrap}>
            <DonutChart segments={segments} size={180} />
          </div>
        </HudPanel>

        <HudPanel
          title="Daily Cost · Last 7 Days"
          accent="#0077c8"
          leading={<HudStatusLed color="#7cc6ff" />}
          meta={<HudChip color={trendUp ? '#ff7de0' : '#6ff2a0'}>{trendUp ? '▲ UP' : '▼ DOWN'}</HudChip>}
        >
          <div className={styles.chartWrap}>
            <BarChart data={dailyData} width={300} height={140} color="#00f0ff" />
          </div>
        </HudPanel>
      </div>

      <HudPanel
        title="Model Details"
        accent="#00f0ff"
        leading={<HudStatusLed color="#6ff2a0" />}
        meta={<>{segments.length} models</>}
      >
        <div className={styles.legend}>
          {segments.map((seg) => (
            <div key={seg.name} className={styles.legendItem}>
              <span
                className={styles.legendDot}
                style={{ background: seg.color, boxShadow: `0 0 6px ${seg.color}` }}
              />
              <span className={styles.legendName}>{seg.name}</span>
              <span className={styles.legendValue}>{formatRand(seg.value)}</span>
            </div>
          ))}
          {segments.length === 0 && (
            <span className={styles.emptyText}>// no cost data available</span>
          )}
        </div>
      </HudPanel>

      {/* Phase B3 + D-series follow-up — per-user weekly budget tile.
          Distinct from the model-cost breakdown above: this one
          attributes spend to the human + shows tripwire state. */}
      <BudgetsTile />
    </div>
  );
}
