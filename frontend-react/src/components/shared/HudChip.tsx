/* ============================================================
   HudChip + HudStatusLed — small atoms used inside HudPanels.
   ============================================================ */

import type { ReactNode } from 'react';
import hudStyles from '../../theme/hud.module.css';

export interface HudChipProps {
  readonly children: ReactNode;
  /** Accent colour of the chip outline + text. */
  readonly color?: string;
  readonly className?: string;
}

/* Small pill with a translucent tint of the accent colour. */
export function HudChip({ children, color = '#00f0ff', className = '' }: HudChipProps) {
  return (
    <span
      className={`${hudStyles.chip} ${className}`}
      style={{
        color,
        background: `${color}22`,
        borderColor: `${color}66`,
      }}
    >
      {children}
    </span>
  );
}

export interface HudStatusLedProps {
  /** Colour of the dot (defaults to cyan). */
  readonly color?: string;
  /** When true the LED pulses; false renders a solid dot. */
  readonly animate?: boolean;
  readonly className?: string;
}

/* A glowing dot. The pulse animation lives in hud.module.css. */
export function HudStatusLed({
  color = '#7cc6ff',
  animate = true,
  className = '',
}: HudStatusLedProps) {
  return (
    <span
      className={`${hudStyles.led} ${animate ? hudStyles.ledAnimate : ''} ${className}`}
      style={{ background: color, boxShadow: `0 0 8px ${color}` }}
    />
  );
}
