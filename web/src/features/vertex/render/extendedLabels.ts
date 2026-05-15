// VTX-019: project a Vertex SystemGraph payload + node RID onto a list of
// extended-label cards the DOM overlay renders above the node.
//
// Each layer in the payload may carry an `extendedLabels[]` array (formal
// schema lands in VTX-058); each entry is one of three kinds:
//   - property:   { kind:'property',  property:<key>, label?:<text> }
//   - timeSeries: { kind:'timeSeries', property:<key>, label?:<text> }
//   - measure:    { kind:'measure',   functionRid:<rid>, label?:<text> }
//
// For VTX-019 this helper only builds the *projection* — i.e. the labels
// and (for property kind) the resolved value pulled from the object's
// properties bag. The actual mini-sparkline / function call lands in the
// kind-specific renderers (VTX-059+).
//
// The function is pure JS so it can be Vitest-tested in isolation and
// composed with `payloadToGraph` upstream.

const MISSING_VALUE = '—';

export type ExtendedLabelKind = 'property' | 'timeSeries' | 'measure';

export interface ExtendedLabel {
  /** Stable React key — `${kind}:${identifier}`. */
  key: string;
  kind: ExtendedLabelKind;
  /** Display text shown on the card. */
  label: string;
  /**
   * Resolved value for `property` kind (e.g. `92` or `JFK`); always a
   * `—` placeholder when the underlying property is missing. For
   * `timeSeries` / `measure` kinds the value lands in a follow-up story
   * (VTX-059 / VTX-064) and stays undefined here.
   */
  value?: string;
}

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

function deriveMeasureLabel(functionRid: string): string {
  // Pull the last `.`-segment so `ri.functions.main.fn.avgDelay` →
  // `avgDelay`. Falls back to the full RID when there's no dot.
  const idx = functionRid.lastIndexOf('.');
  return idx === -1 || idx === functionRid.length - 1
    ? functionRid
    : functionRid.slice(idx + 1);
}

function stringifyValue(v: unknown): string {
  if (v === null || v === undefined) return MISSING_VALUE;
  if (typeof v === 'string') return v === '' ? MISSING_VALUE : v;
  if (typeof v === 'number' || typeof v === 'boolean') return String(v);
  return MISSING_VALUE;
}

interface LayerHit {
  extendedLabels: unknown[];
  properties: Record<string, unknown>;
}

function findLayerHit(payload: unknown, objectRid: string): LayerHit | null {
  if (!isObject(payload)) return null;
  const layers = Array.isArray(payload.layers) ? payload.layers : [];
  for (const layer of layers) {
    if (!isObject(layer)) continue;
    const objects = Array.isArray(layer.objects) ? layer.objects : [];
    for (const obj of objects) {
      if (!isObject(obj)) continue;
      if (obj.objectRid !== objectRid) continue;
      const labels = Array.isArray(layer.extendedLabels)
        ? layer.extendedLabels
        : [];
      const props = isObject(obj.properties) ? obj.properties : {};
      return { extendedLabels: labels, properties: props };
    }
  }
  return null;
}

export function extractExtendedLabels(
  payload: unknown,
  objectRid: string,
): ExtendedLabel[] {
  const hit = findLayerHit(payload, objectRid);
  if (hit === null) return [];
  const out: ExtendedLabel[] = [];
  for (let i = 0; i < hit.extendedLabels.length; i++) {
    const raw = hit.extendedLabels[i];
    if (!isObject(raw)) continue;
    const kind = raw.kind;
    if (kind === 'property') {
      const property = typeof raw.property === 'string' ? raw.property : '';
      if (property === '') continue;
      const label = typeof raw.label === 'string' && raw.label !== ''
        ? raw.label
        : property;
      out.push({
        key: `property:${property}:${i}`,
        kind: 'property',
        label,
        value: stringifyValue(hit.properties[property]),
      });
    } else if (kind === 'timeSeries') {
      const property = typeof raw.property === 'string' ? raw.property : '';
      if (property === '') continue;
      const label = typeof raw.label === 'string' && raw.label !== ''
        ? raw.label
        : property;
      out.push({
        key: `timeSeries:${property}:${i}`,
        kind: 'timeSeries',
        label,
      });
    } else if (kind === 'measure') {
      const rid = typeof raw.functionRid === 'string' ? raw.functionRid : '';
      if (rid === '') continue;
      const label = typeof raw.label === 'string' && raw.label !== ''
        ? raw.label
        : deriveMeasureLabel(rid);
      out.push({
        key: `measure:${rid}:${i}`,
        kind: 'measure',
        label,
      });
    }
    // Unknown kinds are silently skipped — strict 422 validation lands
    // in VTX-058 on the backend.
  }
  return out;
}
