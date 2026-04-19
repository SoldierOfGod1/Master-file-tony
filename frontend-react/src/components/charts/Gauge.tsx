interface GaugeProps {
  readonly value: number;
  readonly max: number;
  readonly color: string;
  readonly label: string;
  readonly size?: number;
}

export default function Gauge({
  value,
  max,
  color,
  label,
  size = 100,
}: GaugeProps) {
  const clampedValue = Math.min(Math.max(value, 0), max);
  const percentage = max > 0 ? Math.round((clampedValue / max) * 100) : 0;

  const center = size / 2;
  const strokeWidth = size * 0.08;
  const radius = (size - strokeWidth * 2) / 2;

  // 270-degree arc: starts at 135 degrees (bottom-left), sweeps 270 degrees
  const startAngle = 135;
  const totalAngle = 270;
  const filledAngle = (percentage / 100) * totalAngle;

  const toRad = (deg: number) => (deg * Math.PI) / 180;

  const arcPath = (angleDeg: number) => {
    const endAngleRad = toRad(startAngle + angleDeg);
    const startRad = toRad(startAngle);
    const x1 = center + radius * Math.cos(startRad);
    const y1 = center + radius * Math.sin(startRad);
    const x2 = center + radius * Math.cos(endAngleRad);
    const y2 = center + radius * Math.sin(endAngleRad);
    const largeArc = angleDeg > 180 ? 1 : 0;
    return `M ${x1} ${y1} A ${radius} ${radius} 0 ${largeArc} 1 ${x2} ${y2}`;
  };

  const filterId = `gauge-glow-${label.replace(/\s+/g, '-')}`;

  return (
    <svg
      width={size}
      height={size}
      viewBox={`0 0 ${size} ${size}`}
      style={{ overflow: 'visible' }}
    >
      <defs>
        <filter id={filterId} x="-50%" y="-50%" width="200%" height="200%">
          <feDropShadow
            dx="0"
            dy="0"
            stdDeviation="3"
            floodColor={color}
            floodOpacity="0.6"
          />
        </filter>
      </defs>

      {/* Background arc */}
      <path
        d={arcPath(totalAngle)}
        fill="none"
        stroke="rgba(255, 255, 255, 0.06)"
        strokeWidth={strokeWidth}
        strokeLinecap="round"
      />

      {/* Colored arc */}
      {percentage > 0 && (
        <path
          d={arcPath(filledAngle)}
          fill="none"
          stroke={color}
          strokeWidth={strokeWidth}
          strokeLinecap="round"
          filter={`url(#${filterId})`}
          style={{
            transition: 'all 0.6s cubic-bezier(0.16, 1, 0.3, 1)',
          }}
        />
      )}

      {/* Center value text */}
      <text
        x={center}
        y={center - size * 0.02}
        textAnchor="middle"
        dominantBaseline="central"
        fill={color}
        fontFamily="'Orbitron', sans-serif"
        fontSize={size * 0.22}
        fontWeight="700"
      >
        {percentage}%
      </text>

      {/* Bottom label */}
      <text
        x={center}
        y={size - size * 0.08}
        textAnchor="middle"
        fill="rgba(255, 255, 255, 0.5)"
        fontFamily="'Poppins', sans-serif"
        fontSize={size * 0.1}
        fontWeight="500"
      >
        {label}
      </text>
    </svg>
  );
}
