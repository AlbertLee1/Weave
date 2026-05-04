import { useMemo } from 'react';
import type { ChartWidget } from './types';
import { useWidgetDataSource } from './dataSource';

// US-428: chart widget renderer. Three real chart types share one SVG
// surface — line / bar / pie — picked from the widget's chartType field.
//
// We deliberately picked SVG over uplot/echarts here. The codebase already
// standardised on uplot for streaming time-series (US-401, TimeSeriesChart)
// where canvas is justified by 10K+ point counts; dashboard widgets are
// short categorical series (8-32 buckets) where SVG is cheaper, jsdom-safe
// without canvas mocks, and renders crisp at any DPR.
//
// The wrapper keeps the legacy `data-chart-type` / `data-chart-values`
// attributes that US-328 tests assert against — the new SVG renderer is
// nested INSIDE the same surface so the existing test contract continues
// to hold.

const PALETTE = [
  '#22d3ee',
  '#f59e0b',
  '#a855f7',
  '#10b981',
  '#ef4444',
  '#3b82f6',
  '#ec4899',
  '#84cc16',
];

const VIEWBOX_W = 200;
const VIEWBOX_H = 100;
const PADDING = 4;

function pickColor(i: number): string {
  return PALETTE[i % PALETTE.length];
}

interface SeriesView {
  values: number[];
  labels: string[];
  source: 'inline' | 'live';
  loading: boolean;
  error: string | undefined;
}

function useResolvedSeries(widget: ChartWidget): SeriesView {
  const live = useWidgetDataSource(widget.dataSource);
  return useMemo(() => {
    if (widget.dataSource && widget.dataSource.kind === 'aggregation') {
      return {
        values: live.values,
        labels:
          live.labels ?? live.values.map((_, i) => String(i + 1)),
        source: 'live',
        loading: live.status === 'loading',
        error: live.status === 'error' ? live.error : undefined,
      };
    }
    return {
      values: widget.values,
      labels:
        widget.labels && widget.labels.length === widget.values.length
          ? widget.labels
          : widget.values.map((_, i) => String(i + 1)),
      source: 'inline',
      loading: false,
      error: undefined,
    };
  }, [
    widget.values,
    widget.labels,
    widget.dataSource,
    live.values,
    live.labels,
    live.status,
    live.error,
  ]);
}

export function ChartView({ widget }: { widget: ChartWidget }) {
  const { values, labels, source, loading, error } = useResolvedSeries(widget);
  return (
    <div
      data-testid="dashboard-widget-chart"
      data-chart-type={widget.chartType}
      data-chart-values={values.join(',')}
      data-chart-source={source}
      data-chart-status={
        loading ? 'loading' : error ? 'error' : 'ready'
      }
      className="w-full h-full flex flex-col"
    >
      {error && (
        <div
          data-testid="dashboard-widget-chart-error"
          className="text-[10px] text-accent-error font-mono"
        >
          {error}
        </div>
      )}
      {loading && values.length === 0 ? (
        <div className="flex-1 flex items-center justify-center text-[10px] text-text-secondary font-mono">
          Loading…
        </div>
      ) : values.length === 0 ? (
        <span className="text-text-secondary italic text-xs self-center my-auto">
          No values
        </span>
      ) : widget.chartType === 'pie' ? (
        <PieChart values={values} labels={labels} />
      ) : widget.chartType === 'line' ? (
        <LineChart values={values} labels={labels} />
      ) : (
        <BarChart values={values} labels={labels} />
      )}
    </div>
  );
}

function BarChart({
  values,
  labels,
}: {
  values: number[];
  labels: string[];
}) {
  const max = Math.max(...values, 0);
  const min = Math.min(...values, 0);
  const span = max - min || 1;
  const innerW = VIEWBOX_W - PADDING * 2;
  const innerH = VIEWBOX_H - PADDING * 2;
  const slot = innerW / values.length;
  const barW = slot * 0.7;
  const baseline = PADDING + innerH * (max / span);
  return (
    <svg
      data-testid="dashboard-widget-chart-svg"
      viewBox={`0 0 ${VIEWBOX_W} ${VIEWBOX_H}`}
      preserveAspectRatio="none"
      className="w-full h-full"
    >
      {values.map((v, i) => {
        const h = (Math.abs(v) / span) * innerH;
        const x = PADDING + i * slot + (slot - barW) / 2;
        const y = v >= 0 ? baseline - h : baseline;
        return (
          <rect
            key={i}
            data-testid="dashboard-widget-chart-bar"
            data-bar-value={v}
            data-bar-label={labels[i]}
            x={x}
            y={y}
            width={barW}
            height={Math.max(1, h)}
            fill={pickColor(i)}
            opacity={0.85}
          >
            <title>{`${labels[i]}: ${v}`}</title>
          </rect>
        );
      })}
    </svg>
  );
}

function LineChart({
  values,
  labels,
}: {
  values: number[];
  labels: string[];
}) {
  const max = Math.max(...values);
  const min = Math.min(...values);
  const span = max - min || 1;
  const innerW = VIEWBOX_W - PADDING * 2;
  const innerH = VIEWBOX_H - PADDING * 2;
  const stepX = values.length > 1 ? innerW / (values.length - 1) : 0;
  const points = values.map((v, i) => {
    const x = PADDING + i * stepX;
    const y = PADDING + innerH - ((v - min) / span) * innerH;
    return { x, y, v, label: labels[i] };
  });
  const path = points
    .map((p, i) => `${i === 0 ? 'M' : 'L'} ${p.x.toFixed(2)} ${p.y.toFixed(2)}`)
    .join(' ');
  return (
    <svg
      data-testid="dashboard-widget-chart-svg"
      viewBox={`0 0 ${VIEWBOX_W} ${VIEWBOX_H}`}
      preserveAspectRatio="none"
      className="w-full h-full"
    >
      <path
        data-testid="dashboard-widget-chart-line"
        d={path}
        stroke={pickColor(0)}
        strokeWidth={1.5}
        fill="none"
      />
      {points.map((p, i) => (
        <circle
          key={i}
          data-testid="dashboard-widget-chart-point"
          data-point-value={p.v}
          data-point-label={p.label}
          cx={p.x}
          cy={p.y}
          r={2}
          fill={pickColor(0)}
        >
          <title>{`${p.label}: ${p.v}`}</title>
        </circle>
      ))}
    </svg>
  );
}

function PieChart({
  values,
  labels,
}: {
  values: number[];
  labels: string[];
}) {
  const positive = values.map((v) => Math.max(0, v));
  const total = positive.reduce((a, b) => a + b, 0);
  const cx = VIEWBOX_W / 2;
  const cy = VIEWBOX_H / 2;
  const r = Math.min(VIEWBOX_W, VIEWBOX_H) / 2 - PADDING;

  if (total === 0) {
    return (
      <svg
        data-testid="dashboard-widget-chart-svg"
        viewBox={`0 0 ${VIEWBOX_W} ${VIEWBOX_H}`}
        className="w-full h-full"
      >
        <circle cx={cx} cy={cy} r={r} fill="none" stroke="#475569" />
      </svg>
    );
  }

  // Special case: a single non-zero slice degenerates a 2-arc path into a
  // zero-area sliver because start == end. Render as a full disc instead.
  const nonZero = positive.filter((v) => v > 0).length;

  // Pre-compute cumulative sums so the JSX map below stays a pure render.
  const offsets: number[] = [];
  let acc = 0;
  for (const v of positive) {
    offsets.push(acc);
    acc += v;
  }

  return (
    <svg
      data-testid="dashboard-widget-chart-svg"
      viewBox={`0 0 ${VIEWBOX_W} ${VIEWBOX_H}`}
      className="w-full h-full"
    >
      {positive.map((v, i) => {
        if (v <= 0) return null;
        if (nonZero === 1) {
          return (
            <circle
              key={i}
              data-testid="dashboard-widget-chart-slice"
              data-slice-value={v}
              data-slice-label={labels[i]}
              cx={cx}
              cy={cy}
              r={r}
              fill={pickColor(i)}
            >
              <title>{`${labels[i]}: ${v}`}</title>
            </circle>
          );
        }
        const startAngle = (offsets[i] / total) * Math.PI * 2;
        const endAngle = ((offsets[i] + v) / total) * Math.PI * 2;
        const x1 = cx + r * Math.sin(startAngle);
        const y1 = cy - r * Math.cos(startAngle);
        const x2 = cx + r * Math.sin(endAngle);
        const y2 = cy - r * Math.cos(endAngle);
        const large = endAngle - startAngle > Math.PI ? 1 : 0;
        const d = [
          `M ${cx} ${cy}`,
          `L ${x1.toFixed(2)} ${y1.toFixed(2)}`,
          `A ${r} ${r} 0 ${large} 1 ${x2.toFixed(2)} ${y2.toFixed(2)}`,
          'Z',
        ].join(' ');
        return (
          <path
            key={i}
            data-testid="dashboard-widget-chart-slice"
            data-slice-value={v}
            data-slice-label={labels[i]}
            d={d}
            fill={pickColor(i)}
          >
            <title>{`${labels[i]}: ${v}`}</title>
          </path>
        );
      })}
    </svg>
  );
}
