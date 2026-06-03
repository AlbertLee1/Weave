import { request } from './client';

export interface TimeSeriesPoint {
  time: string;
  value: unknown;
}

export interface StreamPointsParams {
  ontologyApiName: string;
  objectType: string;
  primaryKey: string;
  property: string;
  // US-404: optional explicit branch override. When set, appended to the
  // path as ?branch=<branch>; the global withActiveBranch injector
  // preserves explicit query params so this overrides the store's
  // per-ontology active branch.
  branch?: string;
}

export function streamTimeSeriesPoints(
  params: StreamPointsParams,
): Promise<TimeSeriesPoint[]> {
  const { ontologyApiName, objectType, primaryKey, property, branch } = params;
  let path =
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}` +
    `/objects/${encodeURIComponent(objectType)}` +
    `/${encodeURIComponent(primaryKey)}` +
    `/timeseries/${encodeURIComponent(property)}/streamPoints`;
  if (branch && branch.trim().length > 0) {
    path += `?branch=${encodeURIComponent(branch.trim())}`;
  }
  return request<TimeSeriesPoint[]>('POST', path, {});
}

// US-402 — chain transform endpoint. The five built-in ops mirror the
// backend taxonomy (pkg/timeseries/transform.go):
//   - diff      : first-difference (no params)
//   - sma       : simple moving average, params.window (int)
//   - ema       : exponential moving average, params.alpha ∈ (0,1]
//   - resample  : bucketize by params.interval (duration string),
//                 params.agg ∈ avg|sum|min|max|count|first|last
//   - scale     : y = factor*v + offset, params.factor / params.offset
export type TransformOp = 'diff' | 'sma' | 'ema' | 'resample' | 'scale';

export interface TransformSpec {
  op: TransformOp;
  // params is a free-form bag keyed per-op; the backend validates it.
  params?: Record<string, unknown>;
}

export interface TransformSource {
  objectType: string;
  primaryKey: string;
  property: string;
}

export interface TransformTimeSeriesInput {
  // Exactly one of source / points must be supplied. source resolves a
  // persisted series via the store; points carries an inline series.
  source?: TransformSource;
  points?: TimeSeriesPoint[];
  transforms: TransformSpec[];
}

export interface TransformTimeSeriesResponse {
  points: TimeSeriesPoint[];
}

export function transformTimeSeries(
  ontologyApiName: string,
  input: TransformTimeSeriesInput,
): Promise<TransformTimeSeriesResponse> {
  const path =
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}` +
    `/timeseries/transform`;
  return request<TransformTimeSeriesResponse>('POST', path, input);
}
