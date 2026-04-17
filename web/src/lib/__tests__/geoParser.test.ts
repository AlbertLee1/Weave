import { describe, it, expect } from 'vitest';
import {
  baseTypeOf,
  geoshapeBounds,
  parseGeopoint,
  parseGeoshape,
} from '../geoParser';

describe('parseGeopoint', () => {
  it('parses "lat,lon" string', () => {
    expect(parseGeopoint('40.7128,-74.006')).toEqual([40.7128, -74.006]);
  });

  it('parses whitespace-separated lat/lon', () => {
    expect(parseGeopoint('40.7128, -74.006')).toEqual([40.7128, -74.006]);
  });

  it('parses {lat, lon} object', () => {
    expect(parseGeopoint({ lat: 1, lon: 2 })).toEqual([1, 2]);
  });

  it('parses {latitude, longitude} object', () => {
    expect(parseGeopoint({ latitude: 1, longitude: 2 })).toEqual([1, 2]);
  });

  it('parses GeoJSON Point (lon/lat coordinate order)', () => {
    expect(
      parseGeopoint({ type: 'Point', coordinates: [-74.006, 40.7128] }),
    ).toEqual([40.7128, -74.006]);
  });

  it('parses GeoJSON Feature wrapping a Point', () => {
    const feat = {
      type: 'Feature',
      geometry: { type: 'Point', coordinates: [10, 20] },
    };
    expect(parseGeopoint(feat)).toEqual([20, 10]);
  });

  it('parses a JSON-string representation of a Point', () => {
    expect(
      parseGeopoint('{"type":"Point","coordinates":[-1,2]}'),
    ).toEqual([2, -1]);
  });

  it('returns null for null/undefined/invalid', () => {
    expect(parseGeopoint(null)).toBeNull();
    expect(parseGeopoint(undefined)).toBeNull();
    expect(parseGeopoint('not a coord')).toBeNull();
    expect(parseGeopoint({ foo: 'bar' })).toBeNull();
    expect(parseGeopoint([NaN, 1])).toBeNull();
  });
});

describe('parseGeoshape', () => {
  it('passes GeoJSON Polygon through', () => {
    const poly = {
      type: 'Polygon',
      coordinates: [
        [
          [0, 0],
          [1, 0],
          [1, 1],
          [0, 1],
          [0, 0],
        ],
      ],
    };
    expect(parseGeoshape(poly)).toEqual(poly);
  });

  it('unwraps Feature to its geometry', () => {
    const feat = {
      type: 'Feature',
      geometry: {
        type: 'LineString',
        coordinates: [
          [0, 0],
          [1, 1],
        ],
      },
    };
    expect(parseGeoshape(feat)).toEqual(feat.geometry);
  });

  it('parses JSON-string geometry', () => {
    const raw = '{"type":"Point","coordinates":[1,2]}';
    const parsed = parseGeoshape(raw);
    expect(parsed?.type).toBe('Point');
  });

  it('returns null for non-geometry input', () => {
    expect(parseGeoshape(null)).toBeNull();
    expect(parseGeoshape({ type: 'Banana', coordinates: [] })).toBeNull();
    expect(parseGeoshape({ type: 'Polygon' })).toBeNull();
  });
});

describe('geoshapeBounds', () => {
  it('computes bounds for a Polygon', () => {
    const poly = {
      type: 'Polygon' as const,
      coordinates: [
        [
          [0, 10],
          [5, 10],
          [5, 15],
          [0, 15],
          [0, 10],
        ] as [number, number][],
      ],
    };
    expect(geoshapeBounds(poly)).toEqual([
      [10, 0],
      [15, 5],
    ]);
  });

  it('computes bounds for a LineString', () => {
    const line = {
      type: 'LineString' as const,
      coordinates: [
        [-1, -2],
        [3, 4],
      ] as [number, number][],
    };
    expect(geoshapeBounds(line)).toEqual([
      [-2, -1],
      [4, 3],
    ]);
  });

  it('returns null for empty coordinates', () => {
    expect(
      geoshapeBounds({ type: 'Polygon', coordinates: [] }),
    ).toBeNull();
  });
});

describe('baseTypeOf', () => {
  it('returns item type for arrays', () => {
    expect(
      baseTypeOf({ type: 'array', itemType: { type: 'geopoint' } }),
    ).toBe('geopoint');
  });
  it('returns type otherwise', () => {
    expect(baseTypeOf({ type: 'geoshape' })).toBe('geoshape');
  });
});
