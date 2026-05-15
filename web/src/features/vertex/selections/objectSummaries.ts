// VTX-020 + VTX-021: project a SystemGraph payload to a Map<rid,
// VertexObjectSummary> keyed by objectRid. The Selection Sidebar uses
// this to render the per-object property panel without re-walking the
// payload; VTX-021 adds the api-name metadata (ontology / objectType /
// primaryKey) so the sidebar's Properties / Series / Linked Events tabs
// can call OSS get / activity / timeseries.
//
// First-layer-wins dedupe (matches payloadToGraph + extractExtendedLabels).

import type { VertexObjectSummary } from '../../../vertex/VertexSelectionSidebar';

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

function lastDotSegment(s: string): string | undefined {
  const idx = s.lastIndexOf('.');
  if (idx === -1 || idx === s.length - 1) return undefined;
  return s.slice(idx + 1);
}

export function payloadToObjectSummaries(payload: unknown): Map<string, VertexObjectSummary> {
  const out = new Map<string, VertexObjectSummary>();
  if (!isObject(payload)) return out;
  const layers = Array.isArray(payload.layers) ? payload.layers : [];
  for (const layer of layers) {
    if (!isObject(layer)) continue;
    const ontologyApiName =
      typeof layer.ontology === 'string' && layer.ontology !== ''
        ? layer.ontology
        : undefined;
    const objectType =
      typeof layer.objectType === 'string' && layer.objectType !== ''
        ? layer.objectType
        : undefined;
    const objects = Array.isArray(layer.objects) ? layer.objects : [];
    for (const obj of objects) {
      if (!isObject(obj)) continue;
      const rid = obj.objectRid;
      if (typeof rid !== 'string' || rid === '') continue;
      if (out.has(rid)) continue;
      const properties = isObject(obj.properties) ? obj.properties : {};
      const name =
        typeof properties.name === 'string' && properties.name !== ''
          ? properties.name
          : rid;
      const explicitPk =
        typeof properties.primaryKey === 'string' && properties.primaryKey !== ''
          ? properties.primaryKey
          : typeof properties.primaryKey === 'number'
            ? String(properties.primaryKey)
            : undefined;
      const primaryKey = explicitPk ?? lastDotSegment(rid);
      out.set(rid, {
        rid,
        label: name,
        properties,
        ontologyApiName,
        objectType,
        primaryKey,
      });
    }
  }
  return out;
}
