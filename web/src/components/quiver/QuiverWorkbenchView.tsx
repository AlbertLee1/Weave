import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTimeSeriesPoints } from '../../hooks/useTimeSeries';
import type { TimeSeriesPoint } from '../../api/timeseries';
import { MultiSeriesChart, type ChartSeries } from './MultiSeriesChart';
import {
  aggregateRange,
  EMPTY_AGGREGATE,
  type RangeAggregate,
} from '../../utils/quiverAggregation';

// US-403: shared chart + aggregate panel rendering used by both the
// editable QuiverPage and the read-only QuiverViewPage. The component
// only owns transient display state (selection range, fetched points) —
// the seriesList itself is provided by the parent and is mutated only
// in editor mode.

const TIME_LOW = -8.64e15;
const TIME_HIGH = 8.64e15;

export interface SeriesSpec {
  id: string;
  ontologyApiName: string;
  objectType: string;
  primaryKey: string;
  property: string;
  label: string;
  color: string;
}

type SeriesStatus = 'loading' | 'error' | 'ready';

function SeriesFetcher({
  spec,
  onLoaded,
}: {
  spec: SeriesSpec;
  onLoaded: (id: string, points: TimeSeriesPoint[], status: SeriesStatus) => void;
}) {
  const { data, isLoading, isError } = useTimeSeriesPoints({
    ontologyApiName: spec.ontologyApiName,
    objectType: spec.objectType,
    primaryKey: spec.primaryKey,
    property: spec.property,
  });
  const status: SeriesStatus = isLoading
    ? 'loading'
    : isError
      ? 'error'
      : 'ready';
  useEffect(() => {
    onLoaded(spec.id, data ?? [], status);
  }, [spec.id, data, status, onLoaded]);
  return null;
}

function formatNumber(n: number): string {
  if (!Number.isFinite(n)) return '—';
  if (Math.abs(n) >= 1000) return n.toFixed(0);
  if (Math.abs(n) >= 1) return n.toFixed(2);
  return n.toFixed(4);
}

function formatTime(ms: number | null): string {
  if (ms === null || !Number.isFinite(ms)) return '—';
  return new Date(ms).toISOString().replace('.000', '');
}

interface QuiverWorkbenchViewProps {
  seriesList: SeriesSpec[];
  // When supplied, an "×" appears on every aggregate row and triggers
  // this callback. Omit (or pass undefined) to render the read-only
  // panel — the editor passes the remove handler, the share view does
  // not.
  onRemove?: (id: string) => void;
}

export function QuiverWorkbenchView({
  seriesList,
  onRemove,
}: QuiverWorkbenchViewProps) {
  const [pointsById, setPointsById] = useState<Record<string, TimeSeriesPoint[]>>({});
  const [statusById, setStatusById] = useState<Record<string, SeriesStatus>>({});
  const [selection, setSelection] = useState<{ start: number | null; end: number | null }>({
    start: null,
    end: null,
  });

  const handleLoaded = useCallback(
    (id: string, points: TimeSeriesPoint[], status: SeriesStatus) => {
      setPointsById((prev) => {
        if (prev[id] === points) return prev;
        return { ...prev, [id]: points };
      });
      setStatusById((prev) => {
        if (prev[id] === status) return prev;
        return { ...prev, [id]: status };
      });
    },
    [],
  );

  const handleRangeSelect = useCallback(
    (start: number | null, end: number | null) => {
      setSelection({ start, end });
    },
    [],
  );

  function handleClearSelection() {
    setSelection({ start: null, end: null });
  }

  const chartSeries = useMemo<ChartSeries[]>(
    () =>
      seriesList.map((s) => ({
        id: s.id,
        label: s.label,
        color: s.color,
        points: pointsById[s.id] ?? [],
      })),
    [seriesList, pointsById],
  );

  const aggregates = useMemo<Record<string, RangeAggregate>>(() => {
    const out: Record<string, RangeAggregate> = {};
    const start = selection.start ?? TIME_LOW;
    const end = selection.end ?? TIME_HIGH;
    for (const s of seriesList) {
      const pts = pointsById[s.id] ?? [];
      out[s.id] = aggregateRange(pts, start, end);
    }
    return out;
  }, [seriesList, pointsById, selection]);

  const hasInfiniteRange = selection.start === null || selection.end === null;
  const editable = onRemove !== undefined;

  return (
    <>
      {seriesList.map((s) => (
        <SeriesFetcher key={s.id} spec={s} onLoaded={handleLoaded} />
      ))}

      <div
        className="border border-border rounded p-4 bg-bg-tertiary"
        data-testid="quiver-chart-panel"
      >
        <MultiSeriesChart
          series={chartSeries}
          onRangeSelect={handleRangeSelect}
        />
      </div>

      <div
        className="border border-border rounded bg-bg-tertiary"
        data-testid="quiver-aggregate-panel"
      >
        <div className="flex items-center justify-between px-4 py-2 border-b border-border">
          <h3 className="text-xs font-medium text-text-primary">
            {hasInfiniteRange ? 'Series totals' : 'Selection aggregate'}
          </h3>
          <div className="flex items-center gap-3 text-xs font-mono text-text-secondary">
            <span data-testid="quiver-selection-start">
              {hasInfiniteRange ? 'all' : formatTime(selection.start)}
            </span>
            <span>→</span>
            <span data-testid="quiver-selection-end">
              {hasInfiniteRange ? 'all' : formatTime(selection.end)}
            </span>
            {!hasInfiniteRange && (
              <button
                type="button"
                onClick={handleClearSelection}
                className="px-2 py-0.5 border border-border rounded text-text-secondary hover:text-text-primary"
                data-testid="quiver-clear-selection"
              >
                clear
              </button>
            )}
          </div>
        </div>
        <table className="w-full text-xs">
          <thead className="bg-bg-primary text-text-secondary uppercase tracking-wider">
            <tr>
              <th className="px-3 py-2 text-left font-sans">Series</th>
              <th className="px-3 py-2 text-right font-sans">Count</th>
              <th className="px-3 py-2 text-right font-sans">Sum</th>
              <th className="px-3 py-2 text-right font-sans">Avg</th>
              <th className="px-3 py-2 text-right font-sans">Min</th>
              <th className="px-3 py-2 text-right font-sans">Max</th>
              {editable && <th className="px-3 py-2"></th>}
            </tr>
          </thead>
          <tbody>
            {seriesList.map((s) => {
              const agg = aggregates[s.id] ?? EMPTY_AGGREGATE;
              const status = statusById[s.id];
              return (
                <tr
                  key={s.id}
                  data-testid={`quiver-row-${s.id}`}
                  className="border-t border-border"
                >
                  <td className="px-3 py-2">
                    <div className="flex items-center gap-2">
                      <span
                        aria-hidden
                        className="inline-block w-3 h-3 rounded-sm flex-shrink-0"
                        style={{ background: s.color }}
                        data-testid={`quiver-color-${s.id}`}
                      />
                      <div className="flex flex-col">
                        <span className="text-text-primary">{s.label}</span>
                        <span className="font-mono text-[10px] text-text-muted">
                          {s.objectType}/{s.primaryKey}.{s.property}
                        </span>
                      </div>
                      {status === 'loading' && (
                        <span className="text-text-muted">…</span>
                      )}
                      {status === 'error' && (
                        <span className="text-accent-error">err</span>
                      )}
                    </div>
                  </td>
                  <td
                    className="px-3 py-2 text-right font-mono text-text-primary"
                    data-testid={`quiver-count-${s.id}`}
                  >
                    {agg.count}
                  </td>
                  <td
                    className="px-3 py-2 text-right font-mono text-text-primary"
                    data-testid={`quiver-sum-${s.id}`}
                  >
                    {agg.count === 0 ? '—' : formatNumber(agg.sum)}
                  </td>
                  <td
                    className="px-3 py-2 text-right font-mono text-text-primary"
                    data-testid={`quiver-avg-${s.id}`}
                  >
                    {agg.count === 0 ? '—' : formatNumber(agg.avg)}
                  </td>
                  <td className="px-3 py-2 text-right font-mono text-text-primary">
                    {agg.count === 0 ? '—' : formatNumber(agg.min)}
                  </td>
                  <td
                    className="px-3 py-2 text-right font-mono text-text-primary"
                    data-testid={`quiver-max-${s.id}`}
                  >
                    {agg.count === 0 ? '—' : formatNumber(agg.max)}
                  </td>
                  {editable && (
                    <td className="px-3 py-2 text-right">
                      <button
                        type="button"
                        onClick={() => onRemove?.(s.id)}
                        data-testid={`quiver-remove-${s.id}`}
                        className="text-text-muted hover:text-accent-error"
                        aria-label="Remove series"
                      >
                        ×
                      </button>
                    </td>
                  )}
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </>
  );
}
