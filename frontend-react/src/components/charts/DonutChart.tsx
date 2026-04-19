interface DonutSegment {
  readonly name: string;
  readonly value: number;
  readonly color: string;
}

interface DonutChartProps {
  readonly segments: readonly DonutSegment[];
  readonly size?: number;
}

function formatCurrency(value: number): string {
  if (value >= 1_000_000) return `R${(value / 1_000_000).toFixed(1)}M`;
  if (value >= 1_000) return `R${(value / 1_000).toFixed(1)}K`;
  return `R${value}`;
}

export default function DonutChart({ segments, size = 120 }: DonutChartProps) {
  const center = size / 2;
  const strokeWidth = size * 0.14;
  const radius = (size - strokeWidth * 2) / 2;
  const circumference = 2 * Math.PI * radius;

  const total = segments.reduce((sum, seg) => sum + seg.value, 0);

  if (total === 0) {
    return (
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`}>
        <circle
          cx={center}
          cy={center}
          r={radius}
          fill="none"
          stroke="rgba(255, 255, 255, 0.06)"
          strokeWidth={strokeWidth}
        />
        <text
          x={center}
          y={center}
          textAnchor="middle"
          dominantBaseline="central"
          fill="rgba(255, 255, 255, 0.3)"
          fontFamily="'Orbitron', sans-serif"
          fontSize={size * 0.16}
          fontWeight="700"
        >
          R0
        </text>
      </svg>
    );
  }

  // Build segments with cumulative offsets
  let cumulativeOffset = 0;
  const segmentPaths = segments
    .filter((seg) => seg.value > 0)
    .map((seg) => {
      const fraction = seg.value / total;
      const segmentLength = circumference * fraction;
      const gapSize = segments.length > 1 ? 2 : 0;
      const adjustedLength = Math.max(segmentLength - gapSize, 0);

      const offset = circumference * 0.25 - cumulativeOffset;
      cumulativeOffset += segmentLength;

      return {
        ...seg,
        dashArray: `${adjustedLength} ${circumference - adjustedLength}`,
        dashOffset: offset,
      };
    });

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
        stroke="rgba(255, 255, 255, 0.04)"
        strokeWidth={strokeWidth}
      />

      {/* Segments */}
      {segmentPaths.map((seg) => (
        <circle
          key={seg.name}
          cx={center}
          cy={center}
          r={radius}
          fill="none"
          stroke={seg.color}
          strokeWidth={strokeWidth}
          strokeLinecap="round"
          strokeDasharray={seg.dashArray}
          strokeDashoffset={seg.dashOffset}
          style={{
            transition: 'stroke-dasharray 0.6s cubic-bezier(0.16, 1, 0.3, 1)',
            filter: `drop-shadow(0 0 4px ${seg.color})`,
          }}
        />
      ))}

      {/* Center total */}
      <text
        x={center}
        y={center}
        textAnchor="middle"
        dominantBaseline="central"
        fill="rgba(255, 255, 255, 0.9)"
        fontFamily="'Orbitron', sans-serif"
        fontSize={size * 0.14}
        fontWeight="700"
      >
        {formatCurrency(total)}
      </text>
    </svg>
  );
}
