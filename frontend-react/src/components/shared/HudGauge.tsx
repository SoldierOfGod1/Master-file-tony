/* ============================================================
   HudGauge — segmented circular gauge used across HUD pages.
   Outer ring of 36 tick marks + inner arc + central Orbitron
   readout. Visual spec from the Canva Component Library slide.
   ============================================================ */

import hudStyles from '../../theme/hud.module.css';

export interface HudGaugeProps {
  /** Value in [0, 1]. Anything outside that range is clamped. */
  readonly value: number;
  /** Small caption rendered under the central readout. */
  readonly label: string;
  /** Big number / text shown in the centre. */
  readonly readout: string;
  /** Accent colour — arc, ticks, readout glow. Defaults to cyan. */
  readonly color?: string;
  /** Pixel size; the SVG is always square. */
  readonly size?: number;
  /** Number of tick marks around the outer ring. 36 feels "tactical". */
  readonly tickCount?: number;
}

export default function HudGauge({
  value,
  label,
  readout,
  color = '#00f0ff',
  size = 128,
  tickCount = 36,
}: HudGaugeProps) {
  const v = Math.max(0, Math.min(1, value));
  const r = size / 2 - 14;
  const cx = size / 2;
  const cy = size / 2;
  const circumference = 2 * Math.PI * r;
  const dash = circumference * v;

  const ticks = Array.from({ length: tickCount }, (_, i) => {
    const angle = (i / tickCount) * Math.PI * 2 - Math.PI / 2;
    const x1 = cx + Math.cos(angle) * (r + 6);
    const y1 = cy + Math.sin(angle) * (r + 6);
    const x2 = cx + Math.cos(angle) * (r + 10);
    const y2 = cy + Math.sin(angle) * (r + 10);
    const lit = i / tickCount <= v;
    return { x1, y1, x2, y2, lit };
  });

  return (
    <svg width={size} height={size} className={hudStyles.gauge}>
      {ticks.map((t, i) => (
        <line
          key={i}
          x1={t.x1}
          y1={t.y1}
          x2={t.x2}
          y2={t.y2}
          stroke={t.lit ? color : 'rgba(255,255,255,0.15)'}
          strokeWidth={1.5}
          strokeLinecap="round"
        />
      ))}
      <circle
        cx={cx}
        cy={cy}
        r={r}
        fill="none"
        stroke="rgba(255,255,255,0.07)"
        strokeWidth={5}
      />
      <circle
        cx={cx}
        cy={cy}
        r={r}
        fill="none"
        stroke={color}
        strokeWidth={5}
        strokeLinecap="round"
        strokeDasharray={`${dash} ${circumference - dash}`}
        strokeDashoffset={circumference / 4}
        transform={`rotate(-90 ${cx} ${cy})`}
        style={{ filter: `drop-shadow(0 0 4px ${color})` }}
      />
      <circle
        cx={cx}
        cy={cy}
        r={r - 12}
        fill="none"
        stroke={color}
        strokeOpacity={0.3}
        strokeWidth={1}
        strokeDasharray="1 3"
      />
      <text
        x={cx}
        y={cy - 2}
        textAnchor="middle"
        fontFamily="Orbitron, sans-serif"
        fontSize={Math.round(size * 0.16)}
        fontWeight={700}
        fill={color}
        style={{ filter: `drop-shadow(0 0 4px ${color})` }}
      >
        {readout}
      </text>
      <text
        x={cx}
        y={cy + Math.round(size * 0.11)}
        textAnchor="middle"
        fontFamily="JetBrains Mono, monospace"
        fontSize={8}
        fill="rgba(255,255,255,0.55)"
        letterSpacing="1"
      >
        {label}
      </text>
    </svg>
  );
}
