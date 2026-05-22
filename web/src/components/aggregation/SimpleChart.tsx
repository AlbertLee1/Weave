import type { AggregationBucket } from '../../api/aggregation';

export type SimpleChartType = 'bar' | 'line' | 'pie';

interface SimpleChartProps {
  data: AggregationBucket[];
  metricKey: string;
  chartType?: SimpleChartType;
}

const PIE_COLORS = ['#22d3ee', '#a78bfa', '#34d399', '#f59e0b', '#f472b6', '#60a5fa'];

function getLabel(bucket: AggregationBucket): string {
  if (!bucket.group) return '';
  const vals = Object.values(bucket.group);
  return vals.map((v) => String(v)).join(', ');
}

function polarToCartesian(
  centerX: number,
  centerY: number,
  radius: number,
  angleInDegrees: number,
) {
  const angleInRadians = ((angleInDegrees - 90) * Math.PI) / 180.0;
  return {
    x: centerX + radius * Math.cos(angleInRadians),
    y: centerY + radius * Math.sin(angleInRadians),
  };
}

function describeSlice(
  centerX: number,
  centerY: number,
  radius: number,
  startAngle: number,
  endAngle: number,
) {
  const start = polarToCartesian(centerX, centerY, radius, endAngle);
  const end = polarToCartesian(centerX, centerY, radius, startAngle);
  const largeArcFlag = endAngle - startAngle <= 180 ? '0' : '1';

  return [
    `M ${centerX} ${centerY}`,
    `L ${start.x} ${start.y}`,
    `A ${radius} ${radius} 0 ${largeArcFlag} 0 ${end.x} ${end.y}`,
    'Z',
  ].join(' ');
}

export function SimpleChart({ data, metricKey, chartType = 'bar' }: SimpleChartProps) {
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

  const points = data.map((bucket, i) => {
    const x = data.length === 1
      ? padding.left + plotWidth / 2
      : padding.left + (plotWidth / (data.length - 1)) * i;
    const y = padding.top + plotHeight - ((values[i] ?? 0) / maxValue) * plotHeight;
    return { x, y, label: getLabel(bucket), value: values[i] ?? 0 };
  });

  function renderAxes() {
    return (
      <>
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
      </>
    );
  }

  function renderMetricLabel() {
    return (
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
    );
  }

  function renderBarChart() {
    return (
      <>
        {renderAxes()}

        {data.map((bucket, i) => {
          const value = values[i] ?? 0;
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

        {renderMetricLabel()}
      </>
    );
  }

  function renderLineChart() {
    return (
      <>
        {renderAxes()}

        <polyline
          data-line
          points={points.map((point) => `${point.x},${point.y}`).join(' ')}
          fill="none"
          stroke="currentColor"
          className="text-accent-cyan"
          strokeWidth="3"
          strokeLinejoin="round"
          strokeLinecap="round"
        />
        {points.map((point, i) => (
          <g key={i}>
            <circle
              data-line-point
              cx={point.x}
              cy={point.y}
              r="4"
              className="fill-bg-primary stroke-accent-cyan"
              strokeWidth="2"
            />
            <text
              x={point.x}
              y={padding.top + plotHeight + 18}
              textAnchor="middle"
              className="fill-text-secondary"
              fontSize="9"
              fontFamily="monospace"
              transform={`rotate(-45, ${point.x}, ${padding.top + plotHeight + 18})`}
            >
              {point.label.length > 12 ? point.label.slice(0, 12) + '...' : point.label}
            </text>
          </g>
        ))}

        {renderMetricLabel()}
      </>
    );
  }

  function renderPieChart() {
    const positiveValues = values.map((value) => Math.max(0, value));
    const positiveTotal = positiveValues.reduce((sum, value) => sum + value, 0);
    const sliceValues = positiveTotal > 0 ? positiveValues : data.map(() => 1);
    const total = positiveTotal > 0 ? positiveTotal : data.length;
    const fullSliceIndex = sliceValues.filter((value) => value > 0).length === 1
      ? sliceValues.findIndex((value) => value > 0)
      : -1;
    let startAngle = 0;
    const centerX = padding.left + 150;
    const centerY = padding.top + 120;
    const radius = 90;

    return (
      <g>
        {sliceValues.map((value, i) => {
          const sweep = (value / total) * 360;
          const endAngle = startAngle + sweep;
          const path = describeSlice(centerX, centerY, radius, startAngle, endAngle);
          startAngle = endAngle;
          const label = getLabel(data[i]!);
          const isFullSlice = i === fullSliceIndex;

          return (
            <g key={i}>
              {isFullSlice ? (
                <circle
                  data-pie-full-slice
                  cx={centerX}
                  cy={centerY}
                  r={radius}
                  fill={PIE_COLORS[i % PIE_COLORS.length]}
                  stroke="currentColor"
                  className="text-bg-tertiary"
                  strokeWidth="2"
                />
              ) : (
                <path
                  data-pie-slice
                  d={path}
                  fill={PIE_COLORS[i % PIE_COLORS.length]}
                  stroke="currentColor"
                  className="text-bg-tertiary"
                  strokeWidth="2"
                />
              )}
              <rect
                x={360}
                y={padding.top + i * 22}
                width={10}
                height={10}
                fill={PIE_COLORS[i % PIE_COLORS.length]}
                rx="2"
              />
              <text
                x={378}
                y={padding.top + 9 + i * 22}
                className="fill-text-secondary"
                fontSize="10"
                fontFamily="monospace"
              >
                {label.length > 20 ? label.slice(0, 20) + '...' : label}
              </text>
            </g>
          );
        })}
        <text
          x={centerX}
          y={centerY}
          textAnchor="middle"
          className="fill-text-secondary"
          fontSize="10"
          fontFamily="monospace"
        >
          {metricKey}
        </text>
      </g>
    );
  }

  return (
    <svg
      data-testid={`aggregation-chart-${chartType}`}
      viewBox={`0 0 ${chartWidth} ${chartHeight}`}
      className="w-full max-w-[600px] h-auto"
    >
      {chartType === 'pie' ? renderPieChart() : null}
      {chartType === 'line' ? renderLineChart() : null}
      {chartType === 'bar' ? renderBarChart() : null}
    </svg>
  );
}
