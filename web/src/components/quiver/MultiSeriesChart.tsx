import { useEffect, useMemo, useRef } from 'react';
import type uPlotCtor from 'uplot';
import 'uplot/dist/uPlot.min.css';
import type { TimeSeriesPoint } from '../../api/timeseries';
import { buildAlignedData } from '../../utils/quiverAggregation';

type UPlotInstance = InstanceType<typeof uPlotCtor>;

export interface ChartSeries {
  id: string;
  label: string;
  color: string;
  points: TimeSeriesPoint[];
  // US-404: optional uplot dash pattern (pixel widths fed to canvas
  // setLineDash). Solid by default; non-default-branch overlays render
  // dashed so the same colour visually disambiguates branches.
  dash?: number[];
}

interface MultiSeriesChartProps {
  series: ChartSeries[];
  // Receives the selected time window in ms (start <= end) when the user
  // releases a drag-selection. If the selection is empty/cleared the
  // callback is invoked with null on both bounds.
  onRangeSelect?: (startMs: number | null, endMs: number | null) => void;
  height?: number;
}

export function MultiSeriesChart({
  series,
  onRangeSelect,
  height = 280,
}: MultiSeriesChartProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const chartRef = useRef<UPlotInstance | null>(null);
  const resizeObserverRef = useRef<ResizeObserver | null>(null);
  // Keep the latest callback reference reachable from the uPlot hook without
  // forcing a chart rebuild every render.
  const onRangeSelectRef = useRef<MultiSeriesChartProps['onRangeSelect']>(onRangeSelect);
  useEffect(() => {
    onRangeSelectRef.current = onRangeSelect;
  }, [onRangeSelect]);

  const aligned = useMemo(() => buildAlignedData(series), [series]);
  const seriesSig = useMemo(
    () => series.map((s) => `${s.id}:${s.label}:${s.color}`).join('|'),
    [series],
  );

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
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

    const { xs, ys } = aligned;
    if (xs.length === 0 || series.length === 0) {
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
        height,
        scales: { x: { time: true } },
        series: [
          {},
          ...series.map((s) => ({
            label: s.label,
            stroke: s.color,
            width: 2,
            spanGaps: true,
            points: { show: false },
            ...(s.dash && s.dash.length > 0 ? { dash: s.dash } : {}),
          })),
        ],
        axes: [
          { stroke: '#94a3b8', grid: { stroke: '#1e293b' } },
          { stroke: '#94a3b8', grid: { stroke: '#1e293b' } },
        ],
        legend: { show: true },
        // Disable x-zoom-on-drag so a drag publishes a selection range
        // instead of mutating the visible window. Aggregation is computed
        // on the released selection.
        cursor: {
          drag: { x: true, y: false, setScale: false },
        },
        hooks: {
          setSelect: [
            (u) => {
              const cb = onRangeSelectRef.current;
              if (!cb) return;
              if (!u.select || u.select.width <= 0) {
                cb(null, null);
                return;
              }
              const startSec = u.posToVal(u.select.left, 'x');
              const endSec = u.posToVal(u.select.left + u.select.width, 'x');
              if (
                !Number.isFinite(startSec) ||
                !Number.isFinite(endSec)
              ) {
                cb(null, null);
                return;
              }
              cb(
                Math.round(startSec * 1000),
                Math.round(endSec * 1000),
              );
            },
          ],
        },
      };

      chartRef.current?.destroy();
      chartRef.current = new uPlot(opts, [xs, ...ys] as uPlotCtor.AlignedData, el);

      if (typeof ResizeObserver !== 'undefined') {
        resizeObserverRef.current?.disconnect();
        const ro = new ResizeObserver(() => {
          const w = el.clientWidth || 600;
          chartRef.current?.setSize({ width: w, height });
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
  }, [aligned, series, seriesSig, height]);

  const hasData = aligned.xs.length > 0;

  return (
    <div data-testid="quiver-chart-wrap" className="space-y-2">
      <div
        ref={containerRef}
        data-testid="quiver-chart"
        className={hasData ? 'w-full' : 'hidden'}
      />
      {!hasData && (
        <div className="text-xs text-text-muted py-8 text-center">
          {series.length === 0
            ? 'Add a series to begin'
            : 'No data in the selected series'}
        </div>
      )}
    </div>
  );
}
