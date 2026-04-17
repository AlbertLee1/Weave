import { useEffect, useMemo, useRef, useState } from 'react';
import type uPlotCtor from 'uplot';
import 'uplot/dist/uPlot.min.css';
import {
  filterPointsByRange,
  useTimeSeriesPoints,
  type TimeSeriesRange,
} from '../../hooks/useTimeSeries';
import type { TimeSeriesPoint } from '../../api/timeseries';

type UPlotInstance = InstanceType<typeof uPlotCtor>;

interface TimeSeriesChartProps {
  ontologyApiName: string;
  objectType: string;
  primaryKey: string;
  property: string;
  label?: string;
}

const RANGE_OPTIONS: TimeSeriesRange[] = ['1h', '24h', '7d', '30d'];

function toNumericValue(v: unknown): number | null {
  if (typeof v === 'number' && Number.isFinite(v)) return v;
  if (typeof v === 'string') {
    const n = Number(v);
    return Number.isFinite(n) ? n : null;
  }
  return null;
}

function toAlignedData(points: TimeSeriesPoint[]): [number[], (number | null)[]] {
  const xs: number[] = [];
  const ys: (number | null)[] = [];
  for (const p of points) {
    const ts = Date.parse(p.time);
    if (!Number.isFinite(ts)) continue;
    xs.push(Math.floor(ts / 1000));
    ys.push(toNumericValue(p.value));
  }
  return [xs, ys];
}

export function TimeSeriesChart({
  ontologyApiName,
  objectType,
  primaryKey,
  property,
  label,
}: TimeSeriesChartProps) {
  const [range, setRange] = useState<TimeSeriesRange>('24h');
  const { data, isLoading, isError, error } = useTimeSeriesPoints({
    ontologyApiName,
    objectType,
    primaryKey,
    property,
  });

  const filtered = useMemo(
    () => (data ? filterPointsByRange(data, range) : []),
    [data, range],
  );

  const chartRef = useRef<UPlotInstance | null>(null);
  const containerRef = useRef<HTMLDivElement | null>(null);
  const resizeObserverRef = useRef<ResizeObserver | null>(null);

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    // matchMedia is required by uplot at load time; skip in environments
    // without it (jsdom) so canvas-free test runs still exercise the
    // surrounding UI logic.
    if (
      typeof window === 'undefined' ||
      typeof window.matchMedia !== 'function'
    ) {
      return;
    }
    const canvasCtx =
      typeof document !== 'undefined' &&
      typeof document.createElement('canvas').getContext === 'function'
        ? document.createElement('canvas').getContext('2d')
        : null;
    if (!canvasCtx) return;

    const [xs, ys] = toAlignedData(filtered);
    if (xs.length === 0) {
      chartRef.current?.destroy();
      chartRef.current = null;
      return;
    }

    let cancelled = false;
    (async () => {
      const { default: uPlot } = await import('uplot');
      if (cancelled) return;
      const width = el.clientWidth || 600;
      const opts: uPlotCtor.Options = {
        width,
        height: 220,
        scales: { x: { time: true } },
        series: [
          {},
          {
            label: label ?? property,
            stroke: '#22d3ee',
            width: 2,
            points: { show: xs.length <= 50 },
          },
        ],
        axes: [
          { stroke: '#94a3b8', grid: { stroke: '#1e293b' } },
          { stroke: '#94a3b8', grid: { stroke: '#1e293b' } },
        ],
        legend: { show: false },
      };

      chartRef.current?.destroy();
      chartRef.current = new uPlot(opts, [xs, ys], el);

      if (typeof ResizeObserver !== 'undefined') {
        resizeObserverRef.current?.disconnect();
        const ro = new ResizeObserver(() => {
          const w = el.clientWidth || 600;
          chartRef.current?.setSize({ width: w, height: 220 });
        });
        ro.observe(el);
        resizeObserverRef.current = ro;
      }
    })();

    return () => {
      cancelled = true;
      resizeObserverRef.current?.disconnect();
      resizeObserverRef.current = null;
      chartRef.current?.destroy();
      chartRef.current = null;
    };
  }, [filtered, label, property]);

  const hasData = filtered.length > 0;

  return (
    <div className="space-y-2" data-testid={`timeseries-${property}`}>
      <div className="flex items-center gap-2 flex-wrap">
        <span className="text-xs font-sans text-text-secondary">
          {label ?? property}
        </span>
        <div className="ml-auto flex gap-1">
          {RANGE_OPTIONS.map((r) => (
            <button
              key={r}
              type="button"
              onClick={() => setRange(r)}
              aria-pressed={range === r}
              className={[
                'px-2 py-0.5 text-xs rounded border transition-colors',
                range === r
                  ? 'border-accent-cyan/60 text-accent-cyan bg-accent-cyan/10'
                  : 'border-border text-text-secondary hover:border-accent-cyan/40 hover:text-text-primary',
              ].join(' ')}
            >
              {r}
            </button>
          ))}
        </div>
      </div>
      {isLoading && (
        <div className="text-xs text-text-secondary py-8 text-center">
          Loading…
        </div>
      )}
      {isError && (
        <div className="text-xs text-accent-error py-4 text-center">
          Failed to load time series
          {error instanceof Error ? `: ${error.message}` : ''}
        </div>
      )}
      {!isLoading && !isError && !hasData && (
        <div className="text-xs text-text-muted py-8 text-center">
          No data in the selected range
        </div>
      )}
      <div
        ref={containerRef}
        data-testid={`timeseries-chart-${property}`}
        className={hasData ? 'w-full' : 'hidden'}
      />
    </div>
  );
}
