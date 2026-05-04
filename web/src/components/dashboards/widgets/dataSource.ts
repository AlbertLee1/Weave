import { useEffect, useMemo, useState } from 'react';
import { aggregate } from '../../../api/aggregation';
import type { AggregationMetric, GroupByClause } from '../../../api/types';
import type { WidgetDataSource } from './types';

// US-428: live aggregation binding for dashboard widgets. Returns a
// `{ values, labels, status }` triple so chart / stat callers can swap the
// inline literal for the live result without per-widget custom plumbing.
//
// status is exposed as a string (rather than separate booleans) so the
// caller can render a dedicated "loading" / "error" overlay slot inside the
// widget surface; tests can assert on the status attribute directly.

export interface WidgetDataResult {
  values: number[];
  labels?: string[];
  status: 'idle' | 'loading' | 'ready' | 'error';
  error?: string;
}

const IDLE: WidgetDataResult = { values: [], status: 'idle' };
const MISSING_BINDING: WidgetDataResult = {
  values: [],
  status: 'error',
  error: 'Missing ontology or objectType',
};

type AggregationSource = Extract<WidgetDataSource, { kind: 'aggregation' }>;

function metricName(ds: AggregationSource): string {
  return `${ds.metric}${ds.property ? '_' + ds.property : ''}`;
}

export function buildAggregationRequest(
  ds: AggregationSource,
): { aggregation: AggregationMetric[]; groupBy?: GroupByClause[] } {
  const m: AggregationMetric = {
    type: ds.metric,
    name: metricName(ds),
  };
  if (ds.metric !== 'count' && ds.property) {
    m.field = ds.property;
  }
  const req: { aggregation: AggregationMetric[]; groupBy?: GroupByClause[] } = {
    aggregation: [m],
  };
  if (ds.groupBy) {
    req.groupBy = [{ field: ds.groupBy, type: 'exact' }];
  }
  return req;
}

// Resolves the aggregation source's runtime status WITHOUT setting state in
// the effect for the trivial inline / unbound paths — those return IDLE /
// MISSING_BINDING references directly so the hook only flips state in
// response to async work.
function resolveSource(
  source: WidgetDataSource | undefined,
):
  | { kind: 'idle' }
  | { kind: 'missing' }
  | { kind: 'fetch'; source: AggregationSource } {
  if (!source || source.kind !== 'aggregation') return { kind: 'idle' };
  if (!source.ontology || !source.objectType) return { kind: 'missing' };
  return { kind: 'fetch', source };
}

export function useWidgetDataSource(
  source: WidgetDataSource | undefined,
): WidgetDataResult {
  const resolved = useMemo(() => resolveSource(source), [source]);
  // Stringify the source so the effect re-runs on shape changes without a
  // useMemo dance — a typical dashboard has < 12 widgets so the cost is
  // negligible and the dependency stability is foolproof.
  const key = resolved.kind === 'fetch' ? JSON.stringify(resolved.source) : '';
  const [fetched, setFetched] = useState<WidgetDataResult>(() =>
    key === '' ? IDLE : { values: [], status: 'loading' },
  );
  // Render-phase setState (progress.txt OfflineIndicator pattern, US-315 prior
  // art): when the upstream key flips, reset to "loading" synchronously so
  // the next render reflects the in-flight fetch without a cascading effect.
  const [prevKey, setPrevKey] = useState(key);
  if (key !== prevKey) {
    setPrevKey(key);
    setFetched(key === '' ? IDLE : { values: [], status: 'loading' });
  }

  useEffect(() => {
    if (resolved.kind !== 'fetch') return;
    let cancelled = false;
    const req = buildAggregationRequest(resolved.source);
    aggregate(resolved.source.ontology, resolved.source.objectType, req)
      .then((resp) => {
        if (cancelled) return;
        const name = metricName(resolved.source);
        const values: number[] = [];
        const labels: string[] = [];
        for (const row of resp.data) {
          const v = row.metrics[name];
          if (typeof v === 'number' && Number.isFinite(v)) {
            values.push(v);
          } else {
            values.push(0);
          }
          if (row.group) {
            const labelKeys = Object.keys(row.group);
            const labelKey = resolved.source.groupBy ?? labelKeys[0];
            const lv = row.group[labelKey];
            labels.push(lv === null || lv === undefined ? '' : String(lv));
          }
        }
        const result: WidgetDataResult = { values, status: 'ready' };
        if (labels.length === values.length && labels.length > 0) {
          result.labels = labels;
        }
        setFetched(result);
      })
      .catch((e: unknown) => {
        if (cancelled) return;
        setFetched({
          values: [],
          status: 'error',
          error: e instanceof Error ? e.message : String(e),
        });
      });
    return () => {
      cancelled = true;
    };
  }, [key, resolved]);

  if (resolved.kind === 'idle') return IDLE;
  if (resolved.kind === 'missing') return MISSING_BINDING;
  return fetched;
}
