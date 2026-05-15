// VTX-020: project a SystemGraph payload to a Map<rid, VertexObjectSummary>
// keyed by objectRid. The Selection Sidebar uses this to render the
// per-object property panel without re-walking the payload.
//
// First-layer-wins dedupe (matches payloadToGraph + extractExtendedLabels).

import type { VertexObjectSummary } from '../../../vertex/VertexSelectionSidebar';

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

export function payloadToObjectSummaries(payload: unknown): Map<string, VertexObjectSummary> {
  const out = new Map<string, VertexObjectSummary>();
  if (!isObject(payload)) return out;
  const layers = Array.isArray(payload.layers) ? payload.layers : [];
  for (const layer of layers) {
    if (!isObject(layer)) continue;
    const objects = Array.isArray(layer.objects) ? layer.objects : [];
    for (const obj of objects) {
      if (!isObject(obj)) continue;
      const rid = obj.objectRid;
      if (typeof rid !== 'string' || rid === '') continue;
      if (out.has(rid)) continue;
      const properties = isObject(obj.properties) ? obj.properties : {};
      const name = typeof properties.name === 'string' && properties.name !== '' ? properties.name : rid;
      out.set(rid, { rid, label: name, properties });
    }
  }
  return out;
}
