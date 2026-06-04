import { request } from './client';

// GeoJSON Point as emitted by the geotemporal store on the wire
// (pkg/geotemporal/store.go). `coordinates` is [longitude, latitude] per the
// GeoJSON spec. Position is typed permissively because the backend stores it
// opaquely (interface{}); production data is a Point but callers must not crash
// on other shapes.
export interface GeoJSONPoint {
  type: 'Point';
  coordinates: [number, number];
}

// A single (time, position) reading on a geotemporal series. `time` is RFC3339
// (ISO 8601); `position` is a GeoJSON Point on the wire but kept as `unknown`
// so non-Point shapes degrade gracefully instead of throwing.
export interface GeotemporalValue {
  time: string;
  position: unknown;
}

export interface GeotemporalSeriesParams {
  ontologyApiName: string;
  objectType: string;
  primaryKey: string;
  property: string;
}

function basePath(params: GeotemporalSeriesParams): string {
  const { ontologyApiName, objectType, primaryKey, property } = params;
  return (
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}` +
    `/objects/${encodeURIComponent(objectType)}` +
    `/${encodeURIComponent(primaryKey)}` +
    `/geotemporalSeries/${encodeURIComponent(property)}`
  );
}

// fetchGeotemporalLatestValue reads the most recent (time, position) value for
// a geotemporal series. The backend returns 404 (mapped to ApiRequestError)
// when the series is empty, so callers should handle the rejected promise.
export function fetchGeotemporalLatestValue(
  params: GeotemporalSeriesParams,
): Promise<GeotemporalValue> {
  return request<GeotemporalValue>('GET', `${basePath(params)}/latestValue`);
}

// fetchGeotemporalHistoricValues streams the full ordered (time ascending)
// series for a geotemporal property. An empty/unknown series returns an empty
// array (the backend never 404s this endpoint), so callers distinguish
// "no data" via the array length.
export function fetchGeotemporalHistoricValues(
  params: GeotemporalSeriesParams,
): Promise<GeotemporalValue[]> {
  return request<GeotemporalValue[]>(
    'POST',
    `${basePath(params)}/streamHistoricValues`,
    {},
  );
}

// describePosition renders a geotemporal position for plain-text display
// without pulling in a map canvas. GeoJSON Point → "lat, lng" (coordinates are
// [lng, lat]); anything else falls back to a compact JSON string.
export function describePosition(position: unknown): string {
  if (
    position &&
    typeof position === 'object' &&
    'coordinates' in position &&
    Array.isArray((position as { coordinates: unknown }).coordinates)
  ) {
    const coords = (position as { coordinates: unknown[] }).coordinates;
    const lng = coords[0];
    const lat = coords[1];
    if (typeof lng === 'number' && typeof lat === 'number') {
      return `${lat}, ${lng}`;
    }
  }
  if (position === null || position === undefined) return '-';
  if (typeof position === 'string') return position;
  return JSON.stringify(position);
}
