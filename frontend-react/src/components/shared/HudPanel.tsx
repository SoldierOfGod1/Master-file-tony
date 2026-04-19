/* ============================================================
   HudPanel — the reusable monitor-panel card.
   Matches the dark-glass / neon-edge / header-strip pattern
   from the Canva Component Library. All HUD pages build their
   layout by composing these.
   ============================================================ */

import type { ReactNode } from 'react';
import hudStyles from '../../theme/hud.module.css';

export interface HudPanelProps {
  /** Uppercase Orbitron title in the header strip. */
  readonly title: string;
  /** Optional node rendered on the left of the title — typically a <StatusLed>. */
  readonly leading?: ReactNode;
  /** Optional node rendered on the right of the title — typically a <Chip> with a count. */
  readonly meta?: ReactNode;
  /** Body content. */
  readonly children: ReactNode;
  /** Optional footer content (ticker / status line). */
  readonly footer?: ReactNode;
  /** Accent colour for the radial wash + top gradient. Defaults to cyan. */
  readonly accent?: string;
  /** Extra class on the root for page-specific tweaks. */
  readonly className?: string;
  /** Called when the user clicks the panel body — for navigation etc. */
  readonly onClick?: () => void;
}

export default function HudPanel({
  title,
  leading,
  meta,
  children,
  footer,
  accent = '#00f0ff',
  className = '',
  onClick,
}: HudPanelProps) {
  const rootClass = [hudStyles.panel, className, onClick ? hudStyles.panelClickable : '']
    .filter(Boolean)
    .join(' ');
  return (
    <div
      className={rootClass}
      style={{ ['--panel-accent' as string]: accent }}
      onClick={onClick}
    >
      <div className={hudStyles.panelHeader}>
        {leading}
        <span className={hudStyles.panelTitle}>{title}</span>
        {meta && <span className={hudStyles.panelMeta}>{meta}</span>}
      </div>
      <div className={hudStyles.panelBody}>{children}</div>
      {footer && <div className={hudStyles.panelFooter}>{footer}</div>}
    </div>
  );
}
