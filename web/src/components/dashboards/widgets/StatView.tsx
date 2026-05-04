import { useMemo } from 'react';
import type { StatWidget } from './types';
import { useWidgetDataSource } from './dataSource';

// US-428: stat card + trend sparkline. The number / label / trend block is
// preserved from US-328 (data-testid="dashboard-widget-stat" + the
// data-stat-trend attribute) — the new functionality is the sparkline
// strip below it, rendered as a tiny SVG line so it stays jsdom-safe.

const SPARK_W = 120;
const SPARK_H = 32;
const SPARK_PAD = 2;

interface ResolvedStat {
  value: string;
  sparkline: number[];
  source: 'inline' | 'live';
  loading: boolean;
  error?: string;
}

function useResolvedStat(widget: StatWidget): ResolvedStat {
  const live = useWidgetDataSource(widget.dataSource);
  return useMemo(() => {
    if (widget.dataSource && widget.dataSource.kind === 'aggregation') {
      const last =
        live.values.length > 0 ? live.values[live.values.length - 1] : 0;
      return {
        value: live.values.length > 0 ? formatStatValue(last) : widget.value,
        sparkline: live.values,
        source: 'live',
        loading: live.status === 'loading',
        error: live.status === 'error' ? live.error : undefined,
      };
    }
    return {
      value: widget.value,
      sparkline: widget.sparkline ?? [],
      source: 'inline',
      loading: false,
      error: undefined,
    };
  }, [widget.value, widget.sparkline, widget.dataSource, live]);
}

function formatStatValue(n: number): string {
  if (!Number.isFinite(n)) return '0';
  if (Number.isInteger(n)) return n.toString();
  // 4 sig figs is plenty for a header number; trim trailing zeros.
  return Number(n.toFixed(4)).toString();
}

export function StatView({ widget }: { widget: StatWidget }) {
  const { value, sparkline, source, loading, error } = useResolvedStat(widget);
  const trendSymbol =
    widget.trend === 'up' ? '▲' : widget.trend === 'down' ? '▼' : '—';
  const trendClass =
    widget.trend === 'up'
      ? 'text-accent-success'
      : widget.trend === 'down'
        ? 'text-accent-error'
        : 'text-text-secondary';
  const sparkColor =
    widget.trend === 'up'
      ? '#10b981'
      : widget.trend === 'down'
        ? '#ef4444'
        : '#94a3b8';

  return (
    <div
      data-testid="dashboard-widget-stat"
      data-stat-trend={widget.trend}
      data-stat-source={source}
      data-stat-status={loading ? 'loading' : error ? 'error' : 'ready'}
      className="w-full h-full flex flex-col items-center justify-center text-center"
    >
      <div className="text-2xl font-semibold text-text-primary">{value}</div>
      <div className="text-[10px] uppercase tracking-wider text-text-secondary mt-1">
        {widget.label}
      </div>
      <div className={`text-xs mt-1 ${trendClass}`}>{trendSymbol}</div>
      {sparkline.length >= 2 && <Sparkline values={sparkline} stroke={sparkColor} />}
      {error && (
        <div
          data-testid="dashboard-widget-stat-error"
          className="text-[10px] text-accent-error font-mono mt-1"
        >
          {error}
        </div>
      )}
    </div>
  );
}

function Sparkline({
  values,
  stroke,
}: {
  values: number[];
  stroke: string;
}) {
  const max = Math.max(...values);
  const min = Math.min(...values);
  const span = max - min || 1;
  const innerW = SPARK_W - SPARK_PAD * 2;
  const innerH = SPARK_H - SPARK_PAD * 2;
  const stepX = values.length > 1 ? innerW / (values.length - 1) : 0;
  const path = values
    .map((v, i) => {
      const x = SPARK_PAD + i * stepX;
      const y = SPARK_PAD + innerH - ((v - min) / span) * innerH;
      return `${i === 0 ? 'M' : 'L'} ${x.toFixed(2)} ${y.toFixed(2)}`;
    })
    .join(' ');
  return (
    <svg
      data-testid="dashboard-widget-stat-sparkline"
      data-spark-values={values.join(',')}
      viewBox={`0 0 ${SPARK_W} ${SPARK_H}`}
      preserveAspectRatio="none"
      className="mt-2 w-full max-w-[160px] h-6"
    >
      <path d={path} stroke={stroke} strokeWidth={1.5} fill="none" />
    </svg>
  );
}
