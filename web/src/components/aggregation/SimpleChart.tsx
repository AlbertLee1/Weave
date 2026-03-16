import type { AggregationBucket } from '../../api/aggregation';

interface SimpleChartProps {
  data: AggregationBucket[];
  metricKey: string;
}

export function SimpleChart({ data, metricKey }: SimpleChartProps) {
  if (data.length === 0) {
    return (
      <div className="text-xs text-text-secondary py-4">No data to chart.</div>
    );
  }

  const values = data.map((b) => b.metrics[metricKey] ?? 0);
  const maxValue = Math.max(...values, 1);

  const chartWidth = 600;
  const chartHeight = 300;
  const padding = { top: 20, right: 20, bottom: 60, left: 60 };
  const plotWidth = chartWidth - padding.left - padding.right;
  const plotHeight = chartHeight - padding.top - padding.bottom;

  const barWidth = Math.max(4, Math.min(40, plotWidth / data.length - 4));
  const barGap = (plotWidth - barWidth * data.length) / (data.length + 1);

  function getLabel(bucket: AggregationBucket): string {
    if (!bucket.group) return '';
    const vals = Object.values(bucket.group);
    return vals.map((v) => String(v)).join(', ');
  }

  return (
    <svg
      viewBox={`0 0 ${chartWidth} ${chartHeight}`}
      className="w-full max-w-[600px] h-auto"
    >
      {/* Y axis */}
      <line
        x1={padding.left}
        y1={padding.top}
        x2={padding.left}
        y2={padding.top + plotHeight}
        stroke="currentColor"
        className="text-border"
        strokeWidth="1"
      />

      {/* X axis */}
      <line
        x1={padding.left}
        y1={padding.top + plotHeight}
        x2={padding.left + plotWidth}
        y2={padding.top + plotHeight}
        stroke="currentColor"
        className="text-border"
        strokeWidth="1"
      />

      {/* Y axis labels */}
      {[0, 0.25, 0.5, 0.75, 1].map((frac) => {
        const y = padding.top + plotHeight * (1 - frac);
        const label = Math.round(maxValue * frac);
        return (
          <g key={frac}>
            <line
              x1={padding.left}
              y1={y}
              x2={padding.left + plotWidth}
              y2={y}
              stroke="currentColor"
              className="text-border"
              strokeWidth="0.5"
              strokeDasharray="4,4"
            />
            <text
              x={padding.left - 8}
              y={y + 3}
              textAnchor="end"
              className="fill-text-secondary"
              fontSize="10"
              fontFamily="monospace"
            >
              {label}
            </text>
          </g>
        );
      })}

      {/* Bars */}
      {data.map((bucket, i) => {
        const value = values[i];
        const barHeight = (value / maxValue) * plotHeight;
        const x = padding.left + barGap + i * (barWidth + barGap);
        const y = padding.top + plotHeight - barHeight;
        const label = getLabel(bucket);

        return (
          <g key={i}>
            <rect
              data-bar
              x={x}
              y={y}
              width={barWidth}
              height={barHeight}
              className="fill-accent-cyan"
              rx="2"
            />
            <text
              x={x + barWidth / 2}
              y={padding.top + plotHeight + 14}
              textAnchor="middle"
              className="fill-text-secondary"
              fontSize="9"
              fontFamily="monospace"
              transform={`rotate(-45, ${x + barWidth / 2}, ${padding.top + plotHeight + 14})`}
            >
              {label.length > 12 ? label.slice(0, 12) + '...' : label}
            </text>
          </g>
        );
      })}

      {/* Metric key label */}
      <text
        x={padding.left - 40}
        y={padding.top + plotHeight / 2}
        textAnchor="middle"
        className="fill-text-secondary"
        fontSize="10"
        fontFamily="monospace"
        transform={`rotate(-90, ${padding.left - 40}, ${padding.top + plotHeight / 2})`}
      >
        {metricKey}
      </text>
    </svg>
  );
}
