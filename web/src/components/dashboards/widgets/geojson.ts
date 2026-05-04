// Parses the GeoJSON value the user persisted on a map widget. Accepts
// either an object literal (the typical post-save shape) or a raw string
// (e.g. mid-edit JSON paste) and reports whether the value is renderable.

export interface ParsedGeoJSON {
  ok: boolean;
  raw: unknown;
}

export function parseGeoJSON(input: unknown): ParsedGeoJSON {
  if (input === undefined || input === null || input === '') {
    return { ok: false, raw: null };
  }
  if (typeof input === 'string') {
    try {
      const parsed = JSON.parse(input);
      return { ok: true, raw: parsed };
    } catch {
      return { ok: false, raw: null };
    }
  }
  if (typeof input === 'object') {
    return { ok: true, raw: input };
  }
  return { ok: false, raw: null };
}
