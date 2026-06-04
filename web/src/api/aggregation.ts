import { request } from './client';
import type { AggregationRequest } from './types';

export interface AggregationResponse {
  data: AggregationBucket[];
  accuracy?: string;
  excludedItems?: number;
}

export interface AggregationBucket {
  group?: Record<string, unknown>;
  metrics: Record<string, number>;
}

export interface WireMetricValue {
  name: string;
  value: unknown;
}

export interface WireAggregationRow {
  group?: Record<string, unknown>;
  metrics: WireMetricValue[] | Record<string, unknown>;
}

export interface WireAggregationResponse {
  data?: WireAggregationRow[];
  accuracy?: string;
  excludedItems?: number;
}

export function normalizeMetrics(
  metrics: WireMetricValue[] | Record<string, unknown> | undefined,
): Record<string, number> {
  if (!metrics) return {};
  if (Array.isArray(metrics)) {
    const out: Record<string, number> = {};
    for (const m of metrics) {
      if (!m || typeof m.name !== 'string') continue;
      const n = typeof m.value === 'number' ? m.value : Number(m.value ?? 0);
      out[m.name] = Number.isFinite(n) ? n : 0;
    }
    return out;
  }
  const out: Record<string, number> = {};
  for (const [k, v] of Object.entries(metrics)) {
    const n = typeof v === 'number' ? v : Number(v ?? 0);
    out[k] = Number.isFinite(n) ? n : 0;
  }
  return out;
}

// normalizeAggregationResponse converts the on-the-wire response (metrics as a
// `[{name,value}]` array OR a `{name: value}` record) into the normalized
// `AggregationResponse` the UI renders. Shared by the HTTP `aggregate` path and
// the live `subscribeAggregation` WebSocket snapshots so both surfaces agree on
// the result shape.
export function normalizeAggregationResponse(
  wire: WireAggregationResponse | undefined,
): AggregationResponse {
  return {
    accuracy: wire?.accuracy,
    excludedItems: wire?.excludedItems,
    data: (wire?.data ?? []).map((row) => ({
      group: row.group,
      metrics: normalizeMetrics(row.metrics),
    })),
  };
}

export async function aggregate(
  ontologyApiName: string,
  objectType: string,
  aggRequest: AggregationRequest,
): Promise<AggregationResponse> {
  const wire = await request<WireAggregationResponse>(
    'POST',
    `/api/v2/ontologies/${ontologyApiName}/objects/${objectType}/aggregate`,
    aggRequest,
  );
  return normalizeAggregationResponse(wire);
}
