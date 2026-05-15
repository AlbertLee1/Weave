// VTX-027: merge user-added objects into a base payload projection.
//
// The "+ Add objects" dialog hands the page a list of {rid, label} pairs
// the user picked from OSS search. The page-level state stores them
// outside the underlying graph payload (they're transient until the next
// Save) and this helper folds them into the projection that GraphLoader /
// VertexNodeOverlay / SelectionSidebar consume — so a freshly added node
// looks identical to a payload-resident one for downstream consumers.
//
// Pure: no React, no Sigma. Skips ids already present in base; dedupes
// within the added list (first occurrence wins). Added nodes get
// deterministic positions on a ring offset from the origin so 5 brand-new
// nodes don't pile up at (0, 0) before the user runs a layout pass.

import type { VertexNode, VertexPayloadGraph } from './payloadToGraph';

const DEFAULT_NODE_SIZE = 8;
const DEFAULT_NODE_COLOR = '#6B7280';
const ADDED_RING_RADIUS = 250;

export interface AddedObject {
  rid: string;
  label: string;
}

export function mergeAddedNodes(
  base: VertexPayloadGraph,
  added: ReadonlyArray<AddedObject>,
): VertexPayloadGraph {
  if (added.length === 0) return base;
  const seen = new Set(base.nodes.map((n) => n.id));
  const nextNodes: VertexNode[] = base.nodes.slice();
  // Local dedupe inside the `added` list — first occurrence wins.
  const queued: AddedObject[] = [];
  for (const obj of added) {
    if (!obj || typeof obj.rid !== 'string' || obj.rid === '') continue;
    if (seen.has(obj.rid)) continue;
    seen.add(obj.rid);
    queued.push(obj);
  }
  const total = Math.max(queued.length, 1);
  for (let i = 0; i < queued.length; i++) {
    const obj = queued[i];
    const angle = (i / total) * 2 * Math.PI;
    nextNodes.push({
      id: obj.rid,
      label: obj.label !== '' ? obj.label : obj.rid,
      x: ADDED_RING_RADIUS * Math.cos(angle),
      y: ADDED_RING_RADIUS * Math.sin(angle),
      size: DEFAULT_NODE_SIZE,
      color: DEFAULT_NODE_COLOR,
    });
  }
  return { nodes: nextNodes, edges: base.edges };
}
