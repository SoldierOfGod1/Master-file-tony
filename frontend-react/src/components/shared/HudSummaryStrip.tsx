/* ============================================================
   HudSummaryStrip — the page-header panel. Big gauge on the
   left, title + subtitle + segmented activity bar on the right.
   Drop this at the top of any HUD page.
   ============================================================ */

import type { ReactNode } from 'react';
import hudStyles from '../../theme/hud.module.css';
import HudGauge from './HudGauge';

export interface HudSegment {
  readonly label: string;
  readonly value: number;
  readonly color: string;
}

export interface HudSummaryStripProps {
  readonly title: string;
  readonly subtitle?: string;
  /** Gauge value in [0, 1] — leave undefined to hide the gauge. */
  readonly gaugeValue?: number;
  readonly gaugeReadout?: string;
  readonly gaugeLabel?: string;
  readonly gaugeColor?: string;
  /** Optional legend segments rendered as a thin multi-colour bar. */
  readonly segments?: readonly HudSegment[];
  /** Slot for extra content on the right (buttons, a second gauge, etc.). */
  readonly extra?: ReactNode;
}

export default function HudSummaryStrip({
  title,
  subtitle,
  gaugeValue,
  gaugeReadout,
  gaugeLabel = 'FLEET',
  gaugeColor = '#00f0ff',
  segments,
  extra,
}: HudSummaryStripProps) {
  const total = segments?.reduce((sum, s) => sum + s.value, 0) ?? 0;
  return (
    <div className={hudStyles.summaryStrip}>
      {gaugeValue !== undefined && (
        <HudGauge
          value={gaugeValue}
          label={gaugeLabel}
          readout={gaugeReadout ?? `${Math.round(gaugeValue * 100)}`}
          color={gaugeColor}
          size={104}
        />
      )}
      <div className={hudStyles.summaryText}>
        <div className={hudStyles.summaryTitle}>{title}</div>
        {subtitle && <div className={hudStyles.summarySub}>{subtitle}</div>}
        {segments && segments.length > 0 && (
          <div className={hudStyles.summaryBarWrap} title={segments.map((s) => `${s.label}: ${s.value}`).join(' · ')}>
            {segments.map((s) => {
              const pct = total === 0 ? 0 : (s.value / total) * 100;
              return (
                <div
                  key={s.label}
                  className={hudStyles.summaryBarSeg}
                  title={`${s.label}: ${s.value}`}
                  style={{
                    width: `${pct}%`,
                    background: s.color,
                    boxShadow: `0 0 6px ${s.color}`,
                  }}
                />
              );
            })}
          </div>
        )}
      </div>
      {extra}
    </div>
  );
}
