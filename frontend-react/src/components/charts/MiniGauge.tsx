interface MiniGaugeProps {
  readonly value: number;
  readonly max: number;
  readonly color: string;
  readonly size?: number;
}

export default function MiniGauge({
  value,
  max,
  color,
  size = 50,
}: MiniGaugeProps) {
  const clampedValue = Math.min(Math.max(value, 0), max);
  const percentage = max > 0 ? clampedValue / max : 0;

  const center = size / 2;
  const strokeWidth = size * 0.1;
  const radius = (size - strokeWidth * 2) / 2;
  const circumference = 2 * Math.PI * radius;
  const filled = circumference * percentage;
  const gap = circumference - filled;

  return (
    <svg
      width={size}
      height={size}
      viewBox={`0 0 ${size} ${size}`}
      style={{ overflow: 'visible' }}
    >
      {/* Background ring */}
      <circle
        cx={center}
        cy={center}
        r={radius}
        fill="none"
        stroke="rgba(255, 255, 255, 0.06)"
        strokeWidth={strokeWidth}
      />

      {/* Colored ring */}
      {percentage > 0 && (
        <circle
          cx={center}
          cy={center}
          r={radius}
          fill="none"
          stroke={color}
          strokeWidth={strokeWidth}
          strokeLinecap="round"
          strokeDasharray={`${filled} ${gap}`}
          strokeDashoffset={circumference * 0.25}
          style={{
            transition: 'stroke-dasharray 0.6s cubic-bezier(0.16, 1, 0.3, 1)',
            filter: `drop-shadow(0 0 3px ${color})`,
          }}
        />
      )}

      {/* Center number */}
      <text
        x={center}
        y={center}
        textAnchor="middle"
        dominantBaseline="central"
        fill={color}
        fontFamily="'Orbitron', sans-serif"
        fontSize={size * 0.24}
        fontWeight="700"
      >
        {clampedValue}
      </text>
    </svg>
  );
}
