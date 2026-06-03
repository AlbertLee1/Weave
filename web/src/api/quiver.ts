import { request } from './client';

// QuiverDashboardConfig is the front-end-owned envelope persisted on
// the backend as opaque JSONB. The series array carries whatever the
// workbench's series-spec shape is at the time of save; new keys
// (transform chain, axis bounds, default selection) can be added
// without a backend change.
export interface QuiverSeriesConfig {
  id: string;
  objectType: string;
  primaryKey: string;
  property: string;
  label: string;
  color: string;
  // US-404: optional ontology branch this series resolves on. Empty/absent
  // means the series tracks the page's active branch (default 'main').
  branch?: string;
}

export interface QuiverDashboardConfig {
  ontologyApiName: string;
  series: QuiverSeriesConfig[];
}

export interface QuiverDashboard {
  rid: string;
  name: string;
  owner: string;
  config: QuiverDashboardConfig;
  createdAt: string;
  updatedAt: string;
}

export interface ListQuiverDashboardsResponse {
  dashboards: QuiverDashboard[];
}

export function listQuiverDashboards(): Promise<ListQuiverDashboardsResponse> {
  return request<ListQuiverDashboardsResponse>('GET', '/api/v2/quiver/dashboards');
}

export function getQuiverDashboard(rid: string): Promise<QuiverDashboard> {
  return request<QuiverDashboard>(
    'GET',
    `/api/v2/quiver/dashboards/${encodeURIComponent(rid)}`,
  );
}

// viewQuiverDashboard hits the read-only share surface — any
// authenticated caller who knows the RID can read the row.
export function viewQuiverDashboard(rid: string): Promise<QuiverDashboard> {
  return request<QuiverDashboard>(
    'GET',
    `/api/v2/quiver/dashboards/${encodeURIComponent(rid)}/view`,
  );
}

export interface SaveQuiverDashboardInput {
  rid?: string;
  name: string;
  config: QuiverDashboardConfig;
}

export function saveQuiverDashboard(
  input: SaveQuiverDashboardInput,
): Promise<QuiverDashboard> {
  return request<QuiverDashboard>('POST', '/api/v2/quiver/save', input);
}

export function deleteQuiverDashboard(rid: string): Promise<void> {
  return request<void>(
    'DELETE',
    `/api/v2/quiver/dashboards/${encodeURIComponent(rid)}`,
  );
}

// US-483: batch sparkline fetch. One POST returns the points for every
// series in the saved dashboard's config, replacing the per-series
// useTimeSeriesPoints fan-out the SPA used to do on dashboard load.

export interface QuiverSparklinePoint {
  time: string;
  value: unknown;
}

export interface QuiverSparklineSeries {
  id: string;
  label: string;
  color: string;
  objectType: string;
  primaryKey: string;
  property: string;
  branch?: string;
  points: QuiverSparklinePoint[];
}

export interface QuiverSparklinesResponse {
  rid: string;
  series: QuiverSparklineSeries[];
}

export interface BatchSparklinesInput {
  // seriesIds restricts the fan-out to a named subset (in dashboard
  // order, not request order). Empty / undefined returns every series.
  seriesIds?: string[];
}

export function batchQuiverSparklines(
  rid: string,
  input: BatchSparklinesInput = {},
): Promise<QuiverSparklinesResponse> {
  return request<QuiverSparklinesResponse>(
    'POST',
    `/api/v2/quiver/dashboards/${encodeURIComponent(rid)}/sparklines`,
    input,
  );
}

// US-482: windowed + bucketed read surface for a saved dashboard. Where
// /sparklines returns raw points, /data clips each series to [from, to]
// and reduces every `step` bucket to its mean — driven by the TopBar
// time-range + step picker so the chart shows a fixed-resolution window
// rather than the full unbounded series.

export interface QuiverDataPoint {
  time: string;
  value: unknown;
}

export interface QuiverDataSeries {
  id: string;
  label: string;
  color: string;
  objectType: string;
  primaryKey: string;
  property: string;
  branch?: string;
  points: QuiverDataPoint[];
}

export interface QuiverDataResponse {
  rid: string;
  // From / To echo the resolved window the server actually scanned, so
  // the SPA can detect drift between the picker selection and the data
  // window the server clipped to.
  from: string;
  to: string;
  step: string;
  series: QuiverDataSeries[];
}

export interface GetQuiverDataParams {
  // from / to accept an RFC3339 timestamp or a unix-millis string; an
  // empty side means "all time" there. step is a required Go duration
  // string (e.g. "5m", "1h") — the bucket width.
  from?: string;
  to?: string;
  step: string;
}

export function getQuiverData(
  rid: string,
  params: GetQuiverDataParams,
): Promise<QuiverDataResponse> {
  const query = new URLSearchParams();
  query.set('step', params.step);
  if (params.from && params.from.trim().length > 0) {
    query.set('from', params.from.trim());
  }
  if (params.to && params.to.trim().length > 0) {
    query.set('to', params.to.trim());
  }
  return request<QuiverDataResponse>(
    'GET',
    `/api/v2/quiver/dashboards/${encodeURIComponent(rid)}/data?${query.toString()}`,
  );
}
