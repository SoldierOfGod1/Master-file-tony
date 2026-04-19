interface BarChartProps {
  readonly data: readonly number[];
  readonly width?: number;
  readonly height?: number;
  readonly color?: string;
}

const DAY_LABELS = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'] as const;

export default function BarChart({
  data,
  width = 200,
  height = 80,
  color = '#0077C8',
}: BarChartProps) {
  const labelHeight = 16;
  const chartHeight = height - labelHeight;
  const maxValue = Math.max(...data, 1);
  const barCount = Math.min(data.length, 7);
  const barGap = 4;
  const barWidth = (width - barGap * (barCount - 1)) / barCount;
  const cornerRadius = Math.min(barWidth * 0.25, 4);

  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      style={{ overflow: 'visible' }}
    >
      {data.slice(0, 7).map((value, i) => {
        const barH = Math.max((value / maxValue) * chartHeight, 2);
        const x = i * (barWidth + barGap);
        const y = chartHeight - barH;

        return (
          <g key={i}>
            {/* Bar with rounded top corners using clipPath */}
            <rect
              x={x}
              y={y}
              width={barWidth}
              height={barH}
              rx={cornerRadius}
              ry={cornerRadius}
              fill={color}
              opacity={0.7}
              style={{
                transition: 'height 0.4s cubic-bezier(0.16, 1, 0.3, 1), y 0.4s cubic-bezier(0.16, 1, 0.3, 1)',
              }}
            />

            {/* Day label */}
            <text
              x={x + barWidth / 2}
              y={height - 2}
              textAnchor="middle"
              fill="rgba(255, 255, 255, 0.4)"
              fontFamily="'JetBrains Mono', monospace"
              fontSize={9}
              fontWeight="500"
            >
              {DAY_LABELS[i] ?? ''}
            </text>
          </g>
        );
      })}
    </svg>
  );
}
