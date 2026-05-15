// VTX-023: Auto-mode heuristic for Vertex layout selection.
//
// "Small" graphs get ForceAtlas2 (organic clusters surface naturally,
// rendering cost is acceptable); larger graphs fall back to the
// Sugiyama / dagre hierarchical pipeline where the deterministic
// rank-based output remains readable past a few hundred nodes.

export const AUTO_LAYOUT_FORCE_NODE_LIMIT = 100;

export type AutoLayoutKind = 'force' | 'hierarchical';

export function pickAutoLayoutKind(nodeCount: number): AutoLayoutKind {
  return nodeCount < AUTO_LAYOUT_FORCE_NODE_LIMIT ? 'force' : 'hierarchical';
}
