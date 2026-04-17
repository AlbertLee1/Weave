// Parses geopoint / geoshape property values into shapes consumable by
// Leaflet. The backend accepts a range of formats (GeoJSON objects, lat/lng
// literals, and "lat,lon" strings), so we normalise here rather than in each
// component.

import type { DataType } from '../api/types';

export type LatLng = [number, number]; // [lat, lon]

// GeoJSON geometry / feature shapes we care about for rendering.
// We keep these loose because they originate from untyped wire data.
export type GeoJSONGeometry =
  | { type: 'Point'; coordinates: [number, number] }
  | { type: 'LineString'; coordinates: [number, number][] }
  | { type: 'Polygon'; coordinates: [number, number][][] }
  | { type: 'MultiPoint'; coordinates: [number, number][] }
  | { type: 'MultiLineString'; coordinates: [number, number][][] }
  | { type: 'MultiPolygon'; coordinates: [number, number][][][] };

export type GeoJSONFeature = {
  type: 'Feature';
  geometry: GeoJSONGeometry;
  properties?: Record<string, unknown>;
};

export function baseTypeOf(dt: DataType): string {
  if (dt.type === 'array' && dt.itemType) return dt.itemType.type;
  return dt.type;
}

function isFiniteNumber(n: unknown): n is number {
  return typeof n === 'number' && Number.isFinite(n);
}

// parseGeopoint accepts the common wire formats and returns [lat, lon] or null.
// Accepted inputs:
//   - "lat,lon" string  (e.g. "40.7128,-74.0060")
//   - {lat, lon} / {latitude, longitude}
//   - [lat, lon]
//   - GeoJSON Point: {type: "Point", coordinates: [lon, lat]}
//   - GeoJSON Feature wrapping a Point
export function parseGeopoint(value: unknown): LatLng | null {
  if (value === null || value === undefined) return null;

  if (typeof value === 'string') {
    const parts = value.split(',').map((s) => s.trim());
    if (parts.length === 2) {
      const lat = Number(parts[0]);
      const lon = Number(parts[1]);
      if (isFiniteNumber(lat) && isFiniteNumber(lon)) return [lat, lon];
    }
    // Try JSON-shaped string as a last resort.
    try {
      return parseGeopoint(JSON.parse(value));
    } catch {
      return null;
    }
  }

  if (Array.isArray(value) && value.length === 2) {
    const [a, b] = value;
    if (isFiniteNumber(a) && isFiniteNumber(b)) return [a, b];
  }

  if (typeof value !== 'object') return null;
  const o = value as Record<string, unknown>;

  // GeoJSON Feature → unwrap to its geometry.
  if (o.type === 'Feature' && o.geometry) {
    return parseGeopoint(o.geometry);
  }
  // GeoJSON Point.
  if (o.type === 'Point' && Array.isArray(o.coordinates)) {
    const [lon, lat] = o.coordinates as unknown[];
    if (isFiniteNumber(lat) && isFiniteNumber(lon)) return [lat, lon];
  }

  // Object shapes with explicit field names.
  const lat = (o.lat ?? o.latitude) as unknown;
  const lon = (o.lon ?? o.lng ?? o.longitude) as unknown;
  if (isFiniteNumber(lat) && isFiniteNumber(lon)) return [lat, lon];

  return null;
}

// parseGeoshape extracts a GeoJSON geometry object from a property value.
// Accepts:
//   - A GeoJSON geometry directly ({type: "Polygon"|"LineString"|..., coordinates})
//   - A GeoJSON Feature (returns its .geometry)
//   - A JSON string encoding either of the above
export function parseGeoshape(value: unknown): GeoJSONGeometry | null {
  if (value === null || value === undefined) return null;

  if (typeof value === 'string') {
    try {
      return parseGeoshape(JSON.parse(value));
    } catch {
      return null;
    }
  }

  if (typeof value !== 'object') return null;
  const o = value as Record<string, unknown>;

  if (o.type === 'Feature' && o.geometry) {
    return parseGeoshape(o.geometry);
  }

  const type = o.type;
  if (
    type === 'Point' ||
    type === 'LineString' ||
    type === 'Polygon' ||
    type === 'MultiPoint' ||
    type === 'MultiLineString' ||
    type === 'MultiPolygon'
  ) {
    if (!Array.isArray(o.coordinates)) return null;
    return o as unknown as GeoJSONGeometry;
  }

  return null;
}

// geoshapeBounds returns a bounding box [[south, west], [north, east]] for a
// geometry, or null if the geometry has no coordinates.
export function geoshapeBounds(
  g: GeoJSONGeometry,
): [LatLng, LatLng] | null {
  let minLat = Infinity;
  let minLon = Infinity;
  let maxLat = -Infinity;
  let maxLon = -Infinity;

  const visitPoint = (pt: unknown) => {
    if (!Array.isArray(pt) || pt.length < 2) return;
    const [lon, lat] = pt;
    if (!isFiniteNumber(lat) || !isFiniteNumber(lon)) return;
    if (lat < minLat) minLat = lat;
    if (lat > maxLat) maxLat = lat;
    if (lon < minLon) minLon = lon;
    if (lon > maxLon) maxLon = lon;
  };

  const walk = (node: unknown) => {
    if (!Array.isArray(node)) return;
    if (node.length > 0 && typeof node[0] === 'number') {
      visitPoint(node);
      return;
    }
    for (const child of node) walk(child);
  };

  walk(g.coordinates);

  if (minLat === Infinity) return null;
  return [
    [minLat, minLon],
    [maxLat, maxLon],
  ];
}
