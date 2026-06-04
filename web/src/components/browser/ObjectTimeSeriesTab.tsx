// US-457: TimeSeries tab on the object-detail panel. Renders one chart
// per property whose dataType.type === "timeseries", auto-fetching points
// from the existing /streamPoints endpoint via the same hook that powers
// the Quiver workbench. Falls back to an empty-state when an object has
// no timeseries-typed properties.
//
// US-402 follow-up: each property carries a transform-chain builder. The
// user picks one of the five built-in ops (diff / sma / ema / resample /
// scale), supplies its params, and applies it; the tab POSTs the
// store-resolved series source to /timeseries/transform and renders the
// transformed series in place of the raw points until cleared.

import { useCallback, useEffect, useMemo, useState } from 'react';
import { MultiSeriesChart, type ChartSeries } from '../quiver/MultiSeriesChart';
import { useTimeSeriesPoints } from '../../hooks/useTimeSeries';
import {
  transformTimeSeries,
  type TimeSeriesPoint,
  type TransformOp,
  type TransformSpec,
} from '../../api/timeseries';
import { baseTypeOf } from '../../lib/geoParser';
import type { ObjectType } from '../../api/types';

const SERIES_PALETTE = [
  '#2563eb',
  '#9333ea',
  '#16a34a',
  '#dc2626',
  '#0891b2',
  '#ea580c',
  '#7c3aed',
  '#0284c7',
];

const TRANSFORM_OPS: { value: TransformOp; label: string }[] = [
  { value: 'diff', label: 'Diff (Δ)' },
  { value: 'sma', label: 'Moving avg (SMA)' },
  { value: 'ema', label: 'Exp. moving avg (EMA)' },
  { value: 'resample', label: 'Resample / downsample' },
  { value: 'scale', label: 'Scale (y = a·v + b)' },
];

// Aggregation choices for the resample/downsample op — mirrors the
// backend taxonomy (pkg/timeseries/transform.go applyResample).
const RESAMPLE_AGGS = ['avg', 'sum', 'min', 'max', 'count'] as const;

interface SeriesFetcherProps {
  ontologyApiName: string;
  objectType: string;
  primaryKey: string;
  property: string;
  onLoaded: (
    property: string,
    points: TimeSeriesPoint[],
    status: 'loading' | 'ready' | 'error',
  ) => void;
}

function SeriesFetcher({
  ontologyApiName,
  objectType,
  primaryKey,
  property,
  onLoaded,
}: SeriesFetcherProps) {
  const { data, isLoading, isError } = useTimeSeriesPoints({
    ontologyApiName,
    objectType,
    primaryKey,
    property,
  });
  const status = isLoading ? 'loading' : isError ? 'error' : 'ready';
  useEffect(() => {
    onLoaded(property, data ?? [], status);
  }, [property, data, status, onLoaded]);
  return null;
}

interface ObjectTimeSeriesTabProps {
  ontologyApiName: string;
  objectType: ObjectType;
  primaryKey: string;
}

interface PropState {
  points: TimeSeriesPoint[];
  status: 'loading' | 'ready' | 'error';
}

// Local builder draft for a single property's transform. `op` is the only
// always-present field; the param inputs are op-specific and validated by
// the backend, so the builder only enforces the minimal client-side shape.
interface TransformDraft {
  op: TransformOp;
  window: string; // sma
  alpha: string; // ema
  interval: string; // resample
  agg: string; // resample
  factor: string; // scale
  offset: string; // scale
}

function emptyDraft(): TransformDraft {
  return {
    op: 'resample',
    window: '5',
    alpha: '0.3',
    interval: '1h',
    agg: 'avg',
    factor: '1',
    offset: '0',
  };
}

// Per-property transform state: what is applied (drives rendering) plus the
// async status of the transform request itself.
interface TransformState {
  applied: TransformSpec[] | null;
  points: TimeSeriesPoint[];
  status: 'idle' | 'running' | 'ready' | 'error';
  error?: string;
}

// buildSpec converts the builder draft into the wire TransformSpec the
// backend expects. Numeric params are coerced to numbers (the backend
// rejects strings); resample keeps interval as a duration string. Returns
// null when a required numeric param is missing/non-finite so the caller
// can surface a validation message instead of POSTing a malformed chain.
function buildSpec(draft: TransformDraft): TransformSpec | null {
  switch (draft.op) {
    case 'diff':
      return { op: 'diff' };
    case 'sma': {
      const window = Number(draft.window);
      if (!Number.isFinite(window) || window <= 0) return null;
      return { op: 'sma', params: { window } };
    }
    case 'ema': {
      const alpha = Number(draft.alpha);
      if (!Number.isFinite(alpha) || alpha <= 0 || alpha > 1) return null;
      return { op: 'ema', params: { alpha } };
    }
    case 'resample': {
      const interval = draft.interval.trim();
      if (interval.length === 0) return null;
      return { op: 'resample', params: { interval, agg: draft.agg } };
    }
    case 'scale': {
      const factor = Number(draft.factor);
      if (!Number.isFinite(factor)) return null;
      const params: Record<string, number> = { factor };
      if (draft.offset.trim().length > 0) {
        const offset = Number(draft.offset);
        if (!Number.isFinite(offset)) return null;
        params.offset = offset;
      }
      return { op: 'scale', params };
    }
    default:
      return null;
  }
}

export function ObjectTimeSeriesTab({
  ontologyApiName,
  objectType,
  primaryKey,
}: ObjectTimeSeriesTabProps) {
  const tsProperties = useMemo(() => {
    if (!objectType.properties) return [] as string[];
    return Object.entries(objectType.properties)
      .filter(([, prop]) => baseTypeOf(prop.dataType) === 'timeseries')
      .map(([name]) => name)
      .sort();
  }, [objectType.properties]);

  const [byProperty, setByProperty] = useState<Record<string, PropState>>({});
  const [drafts, setDrafts] = useState<Record<string, TransformDraft>>({});
  const [transforms, setTransforms] = useState<Record<string, TransformState>>(
    {},
  );

  const handleLoaded = useCallback(
    (property: string, points: TimeSeriesPoint[], status: PropState['status']) => {
      setByProperty((prev) => {
        const cur = prev[property];
        if (cur && cur.status === status && cur.points === points) return prev;
        return { ...prev, [property]: { points, status } };
      });
    },
    [],
  );

  // Reset cached series + transform state when the targeted object changes
  // so a stale chart/transform from a previous selection does not leak.
  useEffect(() => {
    setByProperty({});
    setDrafts({});
    setTransforms({});
  }, [primaryKey, objectType.apiName]);

  const draftFor = useCallback(
    (property: string): TransformDraft => drafts[property] ?? emptyDraft(),
    [drafts],
  );

  const updateDraft = useCallback(
    (property: string, patch: Partial<TransformDraft>) => {
      setDrafts((prev) => ({
        ...prev,
        [property]: { ...(prev[property] ?? emptyDraft()), ...patch },
      }));
    },
    [],
  );

  const applyTransform = useCallback(
    async (property: string) => {
      const draft = drafts[property] ?? emptyDraft();
      const spec = buildSpec(draft);
      if (!spec) {
        setTransforms((prev) => ({
          ...prev,
          [property]: {
            applied: null,
            points: [],
            status: 'error',
            error: 'Invalid transform parameters.',
          },
        }));
        return;
      }
      const chain = [spec];
      setTransforms((prev) => ({
        ...prev,
        [property]: {
          applied: prev[property]?.applied ?? null,
          points: prev[property]?.points ?? [],
          status: 'running',
        },
      }));
      try {
        const res = await transformTimeSeries(ontologyApiName, {
          source: {
            objectType: objectType.apiName,
            primaryKey,
            property,
          },
          transforms: chain,
        });
        setTransforms((prev) => ({
          ...prev,
          [property]: {
            applied: chain,
            points: res.points ?? [],
            status: 'ready',
          },
        }));
      } catch (err) {
        setTransforms((prev) => ({
          ...prev,
          [property]: {
            applied: null,
            points: [],
            status: 'error',
            error: err instanceof Error ? err.message : 'Transform failed.',
          },
        }));
      }
    },
    [drafts, ontologyApiName, objectType.apiName, primaryKey],
  );

  const clearTransform = useCallback((property: string) => {
    setTransforms((prev) => {
      if (!prev[property]) return prev;
      const next = { ...prev };
      delete next[property];
      return next;
    });
  }, []);

  if (tsProperties.length === 0) {
    return (
      <div
        data-testid="ts-tab-empty"
        className="text-xs text-text-secondary px-1 py-4"
      >
        This object type has no timeseries properties.
      </div>
    );
  }

  return (
    <div className="space-y-4" data-testid="ts-tab">
      {tsProperties.map((property) => (
        <SeriesFetcher
          key={`fetch-${property}`}
          ontologyApiName={ontologyApiName}
          objectType={objectType.apiName}
          primaryKey={primaryKey}
          property={property}
          onLoaded={handleLoaded}
        />
      ))}
      {tsProperties.map((property, idx) => {
        const state = byProperty[property];
        const xform = transforms[property];
        const draft = draftFor(property);
        const color = SERIES_PALETTE[idx % SERIES_PALETTE.length];
        const showTransformed = xform?.status === 'ready' && !!xform.applied;
        const renderPoints = showTransformed
          ? xform.points
          : state?.points ?? [];
        const series: ChartSeries[] =
          showTransformed || (state && state.status === 'ready')
            ? [
                {
                  id: property,
                  label: showTransformed ? `${property} (transformed)` : property,
                  color,
                  points: renderPoints,
                },
              ]
            : [];
        return (
          <section
            key={property}
            data-testid={`ts-tab-property-${property}`}
            className="rounded-md border border-border p-3"
          >
            <header className="flex items-center justify-between mb-2">
              <h3 className="text-xs font-mono font-medium text-text-primary">
                {property}
              </h3>
              <span
                data-testid={`ts-tab-status-${property}`}
                className="text-[10px] uppercase tracking-wide text-text-secondary"
              >
                {state ? state.status : 'loading'}
              </span>
            </header>

            {/* Transform builder — only meaningful once raw points exist. */}
            {state?.status === 'ready' && (
              <div
                data-testid={`ts-transform-builder-${property}`}
                className="mb-3 flex flex-wrap items-end gap-2 rounded border border-border/60 bg-surface-secondary/40 p-2"
              >
                <label className="flex flex-col gap-0.5">
                  <span className="text-[10px] uppercase tracking-wide text-text-secondary">
                    Transform
                  </span>
                  <select
                    data-testid={`ts-transform-op-${property}`}
                    className="text-xs border border-border rounded px-1.5 py-1 bg-surface-primary"
                    value={draft.op}
                    onChange={(e) =>
                      updateDraft(property, {
                        op: e.target.value as TransformOp,
                      })
                    }
                  >
                    {TRANSFORM_OPS.map((o) => (
                      <option key={o.value} value={o.value}>
                        {o.label}
                      </option>
                    ))}
                  </select>
                </label>

                {draft.op === 'sma' && (
                  <label className="flex flex-col gap-0.5">
                    <span className="text-[10px] uppercase tracking-wide text-text-secondary">
                      Window
                    </span>
                    <input
                      data-testid={`ts-transform-window-${property}`}
                      type="number"
                      min={1}
                      className="w-20 text-xs border border-border rounded px-1.5 py-1 bg-surface-primary"
                      value={draft.window}
                      onChange={(e) =>
                        updateDraft(property, { window: e.target.value })
                      }
                    />
                  </label>
                )}

                {draft.op === 'ema' && (
                  <label className="flex flex-col gap-0.5">
                    <span className="text-[10px] uppercase tracking-wide text-text-secondary">
                      Alpha (0–1]
                    </span>
                    <input
                      data-testid={`ts-transform-alpha-${property}`}
                      type="number"
                      step="0.05"
                      min={0}
                      max={1}
                      className="w-20 text-xs border border-border rounded px-1.5 py-1 bg-surface-primary"
                      value={draft.alpha}
                      onChange={(e) =>
                        updateDraft(property, { alpha: e.target.value })
                      }
                    />
                  </label>
                )}

                {draft.op === 'resample' && (
                  <>
                    <label className="flex flex-col gap-0.5">
                      <span className="text-[10px] uppercase tracking-wide text-text-secondary">
                        Interval
                      </span>
                      <input
                        data-testid={`ts-transform-interval-${property}`}
                        type="text"
                        placeholder="1h"
                        className="w-20 text-xs border border-border rounded px-1.5 py-1 bg-surface-primary"
                        value={draft.interval}
                        onChange={(e) =>
                          updateDraft(property, { interval: e.target.value })
                        }
                      />
                    </label>
                    <label className="flex flex-col gap-0.5">
                      <span className="text-[10px] uppercase tracking-wide text-text-secondary">
                        Aggregation
                      </span>
                      <select
                        data-testid={`ts-transform-agg-${property}`}
                        className="text-xs border border-border rounded px-1.5 py-1 bg-surface-primary"
                        value={draft.agg}
                        onChange={(e) =>
                          updateDraft(property, { agg: e.target.value })
                        }
                      >
                        {RESAMPLE_AGGS.map((a) => (
                          <option key={a} value={a}>
                            {a}
                          </option>
                        ))}
                      </select>
                    </label>
                  </>
                )}

                {draft.op === 'scale' && (
                  <>
                    <label className="flex flex-col gap-0.5">
                      <span className="text-[10px] uppercase tracking-wide text-text-secondary">
                        Factor
                      </span>
                      <input
                        data-testid={`ts-transform-factor-${property}`}
                        type="number"
                        step="any"
                        className="w-20 text-xs border border-border rounded px-1.5 py-1 bg-surface-primary"
                        value={draft.factor}
                        onChange={(e) =>
                          updateDraft(property, { factor: e.target.value })
                        }
                      />
                    </label>
                    <label className="flex flex-col gap-0.5">
                      <span className="text-[10px] uppercase tracking-wide text-text-secondary">
                        Offset
                      </span>
                      <input
                        data-testid={`ts-transform-offset-${property}`}
                        type="number"
                        step="any"
                        className="w-20 text-xs border border-border rounded px-1.5 py-1 bg-surface-primary"
                        value={draft.offset}
                        onChange={(e) =>
                          updateDraft(property, { offset: e.target.value })
                        }
                      />
                    </label>
                  </>
                )}

                <button
                  type="button"
                  data-testid={`ts-transform-apply-${property}`}
                  className="text-xs px-2 py-1 rounded bg-accent text-white hover:opacity-90 disabled:opacity-50"
                  disabled={xform?.status === 'running'}
                  onClick={() => {
                    void applyTransform(property);
                  }}
                >
                  {xform?.status === 'running' ? 'Applying…' : 'Apply'}
                </button>

                {showTransformed && (
                  <button
                    type="button"
                    data-testid={`ts-transform-clear-${property}`}
                    className="text-xs px-2 py-1 rounded border border-border text-text-secondary hover:bg-surface-secondary"
                    onClick={() => clearTransform(property)}
                  >
                    Clear
                  </button>
                )}
              </div>
            )}

            {showTransformed && (
              <div
                data-testid={`ts-transform-active-${property}`}
                className="mb-2 text-[10px] uppercase tracking-wide font-medium"
                style={{ color }}
              >
                Showing transformed series ({xform.applied?.[0]?.op})
              </div>
            )}

            {xform?.status === 'error' && (
              <div
                data-testid={`ts-transform-error-${property}`}
                className="mb-2 text-xs text-rose-700"
              >
                {xform.error ?? 'Transform failed.'}
              </div>
            )}

            {!showTransformed &&
              state?.status === 'ready' &&
              state.points.length === 0 && (
                <div
                  data-testid={`ts-tab-nopoints-${property}`}
                  className="text-xs text-text-secondary py-6 text-center"
                >
                  No points recorded for this series yet.
                </div>
              )}
            {state?.status === 'error' && (
              <div
                data-testid={`ts-tab-error-${property}`}
                className="text-xs text-rose-700 py-6 text-center"
              >
                Failed to load timeseries data.
              </div>
            )}
            {series.length > 0 && renderPoints.length > 0 && (
              <MultiSeriesChart series={series} height={200} />
            )}
          </section>
        );
      })}
    </div>
  );
}
