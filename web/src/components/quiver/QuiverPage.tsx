import { useCallback, useEffect, useMemo, useState } from 'react';
import { useParams } from 'react-router';
import { useTimeSeriesPoints } from '../../hooks/useTimeSeries';
import type { TimeSeriesPoint } from '../../api/timeseries';
import { EmptyState } from '../common/EmptyState';
import { MultiSeriesChart, type ChartSeries } from './MultiSeriesChart';
import {
  aggregateRange,
  EMPTY_AGGREGATE,
  pickColor,
  type RangeAggregate,
} from '../../utils/quiverAggregation';

// Wide-enough timestamp bounds that aggregateRange's "in window" predicate
// admits every parseable point. Chosen to stay finite (so the helper's
// finiteness guard does not trip) and to span the JS Date range.
const TIME_LOW = -8.64e15;
const TIME_HIGH = 8.64e15;

interface SeriesSpec {
  id: string;
  ontologyApiName: string;
  objectType: string;
  primaryKey: string;
  property: string;
  label: string;
  color: string;
}

// Sub-component pattern (one per spec) so React Hooks rules stay satisfied
// while the parent stitches multiple useTimeSeriesPoints results together.
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

export function QuiverPage() {
  const { ontology } = useParams<{ ontology: string }>();
  const ontologyApiName = ontology ?? '';
  const [seriesList, setSeriesList] = useState<SeriesSpec[]>([]);
  const [pointsById, setPointsById] = useState<Record<string, TimeSeriesPoint[]>>({});
  const [statusById, setStatusById] = useState<Record<string, SeriesStatus>>({});
  const [selection, setSelection] = useState<{ start: number | null; end: number | null }>({
    start: null,
    end: null,
  });

  // Picker form state
  const [draftObjectType, setDraftObjectType] = useState('');
  const [draftPrimaryKey, setDraftPrimaryKey] = useState('');
  const [draftProperty, setDraftProperty] = useState('');
  const [draftLabel, setDraftLabel] = useState('');

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

  function handleAdd(e: React.FormEvent) {
    e.preventDefault();
    const objectType = draftObjectType.trim();
    const primaryKey = draftPrimaryKey.trim();
    const property = draftProperty.trim();
    if (!objectType || !primaryKey || !property) return;
    const id = `${objectType}|${primaryKey}|${property}|${Date.now()}`;
    const label = draftLabel.trim() || `${objectType}/${primaryKey}.${property}`;
    setSeriesList((prev) => [
      ...prev,
      {
        id,
        ontologyApiName,
        objectType,
        primaryKey,
        property,
        label,
        color: pickColor(prev.length),
      },
    ]);
    setDraftObjectType('');
    setDraftPrimaryKey('');
    setDraftProperty('');
    setDraftLabel('');
  }

  function handleRemove(id: string) {
    setSeriesList((prev) => prev.filter((s) => s.id !== id));
    setPointsById((prev) => {
      if (!(id in prev)) return prev;
      const next = { ...prev };
      delete next[id];
      return next;
    });
    setStatusById((prev) => {
      if (!(id in prev)) return prev;
      const next = { ...prev };
      delete next[id];
      return next;
    });
  }

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

  // Per-series aggregate over the active selection window. When no selection
  // is active, fall through to "all data" aggregation so the panel always has
  // something useful to show — a common Foundry/Quiver UX expectation.
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

  if (!ontologyApiName) {
    return (
      <div className="flex items-center justify-center h-full">
        <EmptyState
          title="Missing Ontology"
          description="Pick an ontology from the sidebar to use Quiver."
        />
      </div>
    );
  }

  const hasInfiniteRange = selection.start === null || selection.end === null;

  return (
    <div className="flex flex-col h-full overflow-hidden" data-testid="quiver-page">
      {seriesList.map((s) => (
        <SeriesFetcher key={s.id} spec={s} onLoaded={handleLoaded} />
      ))}

      <div className="border-b border-border bg-bg-primary p-4 flex flex-col gap-4">
        <div>
          <h2 className="text-sm font-medium text-text-primary">Quiver Workbench</h2>
          <div className="text-xs font-mono text-text-secondary mt-0.5">
            {ontologyApiName}
          </div>
        </div>

        <form
          onSubmit={handleAdd}
          className="grid grid-cols-1 md:grid-cols-5 gap-2"
          data-testid="quiver-add-form"
        >
          <input
            type="text"
            placeholder="Object type"
            value={draftObjectType}
            onChange={(e) => setDraftObjectType(e.target.value)}
            data-testid="quiver-input-objectType"
            className="px-2 py-1.5 text-sm bg-bg-tertiary border border-border rounded text-text-primary"
          />
          <input
            type="text"
            placeholder="Primary key"
            value={draftPrimaryKey}
            onChange={(e) => setDraftPrimaryKey(e.target.value)}
            data-testid="quiver-input-primaryKey"
            className="px-2 py-1.5 text-sm bg-bg-tertiary border border-border rounded text-text-primary"
          />
          <input
            type="text"
            placeholder="Property"
            value={draftProperty}
            onChange={(e) => setDraftProperty(e.target.value)}
            data-testid="quiver-input-property"
            className="px-2 py-1.5 text-sm bg-bg-tertiary border border-border rounded text-text-primary"
          />
          <input
            type="text"
            placeholder="Label (optional)"
            value={draftLabel}
            onChange={(e) => setDraftLabel(e.target.value)}
            data-testid="quiver-input-label"
            className="px-2 py-1.5 text-sm bg-bg-tertiary border border-border rounded text-text-primary"
          />
          <button
            type="submit"
            disabled={
              !draftObjectType.trim() ||
              !draftPrimaryKey.trim() ||
              !draftProperty.trim()
            }
            data-testid="quiver-add-button"
            className="bg-accent-cyan text-bg-primary px-4 py-1.5 rounded text-sm font-medium hover:bg-accent-cyan/80 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            Add series
          </button>
        </form>
      </div>

      <div className="flex-1 overflow-y-auto p-4 space-y-4">
        {seriesList.length === 0 ? (
          <EmptyState
            title="No series yet"
            description="Add a series above to plot a time-series overlay."
          />
        ) : (
          <>
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
                    <th className="px-3 py-2"></th>
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
                        <td className="px-3 py-2 text-right font-mono text-text-primary" data-testid={`quiver-count-${s.id}`}>
                          {agg.count}
                        </td>
                        <td className="px-3 py-2 text-right font-mono text-text-primary" data-testid={`quiver-sum-${s.id}`}>
                          {agg.count === 0 ? '—' : formatNumber(agg.sum)}
                        </td>
                        <td className="px-3 py-2 text-right font-mono text-text-primary" data-testid={`quiver-avg-${s.id}`}>
                          {agg.count === 0 ? '—' : formatNumber(agg.avg)}
                        </td>
                        <td className="px-3 py-2 text-right font-mono text-text-primary">
                          {agg.count === 0 ? '—' : formatNumber(agg.min)}
                        </td>
                        <td className="px-3 py-2 text-right font-mono text-text-primary" data-testid={`quiver-max-${s.id}`}>
                          {agg.count === 0 ? '—' : formatNumber(agg.max)}
                        </td>
                        <td className="px-3 py-2 text-right">
                          <button
                            type="button"
                            onClick={() => handleRemove(s.id)}
                            data-testid={`quiver-remove-${s.id}`}
                            className="text-text-muted hover:text-accent-error"
                            aria-label="Remove series"
                          >
                            ×
                          </button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
