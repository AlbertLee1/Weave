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
}

export function streamTimeSeriesPoints(
  params: StreamPointsParams,
): Promise<TimeSeriesPoint[]> {
  const { ontologyApiName, objectType, primaryKey, property } = params;
  const path =
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}` +
    `/objects/${encodeURIComponent(objectType)}` +
    `/${encodeURIComponent(primaryKey)}` +
    `/timeseries/${encodeURIComponent(property)}/streamPoints`;
  return request<TimeSeriesPoint[]>('POST', path, {});
}
