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
