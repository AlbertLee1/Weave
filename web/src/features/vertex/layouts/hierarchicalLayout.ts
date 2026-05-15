// VTX-022: hierarchical layout (Sugiyama / dagre) for Vertex canvases.
//
// Thin adapter over @dagrejs/dagre that takes a (nodes, edges) projection +
// a few knobs and returns a Map<nodeId, {x, y}>. The result is meant to be
// written back onto a graphology Graph via `setNodeAttribute(id, 'x'|'y', …)`
// followed by `sigma.refresh()`. This file is intentionally framework-free
// (no React, no Sigma) so the math is Vitest-friendly and the PRD's
// `graphology-layout-dagre` gap is filled in-tree without a custom npm
// package. The PRD line names that package but it doesn't exist on the
// registry; @dagrejs/dagre is the upstream library that any such adapter
// would wrap, so we wrap it here directly.

import dagre from '@dagrejs/dagre';

export interface LayoutNodeInput {
  id: string;
  /** Optional per-node box size (px). Defaults to nodeWidth/nodeHeight. */
  width?: number;
  height?: number;
}

export interface LayoutEdgeInput {
  source: string;
  target: string;
}

export interface LayoutPoint {
  x: number;
  y: number;
}

export interface HierarchicalLayoutOptions {
  nodes: LayoutNodeInput[];
  edges: LayoutEdgeInput[];
  /** Flip rankdir from TB to BT so the root sits at the bottom. */
  reverse?: boolean;
  /**
   * Pin these nodes to the top rank by stripping all of their incoming
   * edges before running dagre. Unknown ids are ignored.
   */
  rootNodes?: string[];
  /** Spacing knobs (dagre defaults are reasonable but tuneable per surface). */
  nodeSep?: number;
  rankSep?: number;
  nodeWidth?: number;
  nodeHeight?: number;
}

const DEFAULT_NODE_WIDTH = 40;
const DEFAULT_NODE_HEIGHT = 40;
const DEFAULT_NODE_SEP = 50;
const DEFAULT_RANK_SEP = 80;

export function hierarchicalLayout(
  opts: HierarchicalLayoutOptions,
): Map<string, LayoutPoint> {
  const out = new Map<string, LayoutPoint>();
  if (opts.nodes.length === 0) return out;

  const g = new dagre.graphlib.Graph();
  g.setGraph({
    rankdir: opts.reverse ? 'BT' : 'TB',
    nodesep: opts.nodeSep ?? DEFAULT_NODE_SEP,
    ranksep: opts.rankSep ?? DEFAULT_RANK_SEP,
    marginx: 20,
    marginy: 20,
  });
  g.setDefaultEdgeLabel(() => ({}));

  const ids = new Set<string>();
  for (const n of opts.nodes) {
    if (!n.id || ids.has(n.id)) continue;
    ids.add(n.id);
    g.setNode(n.id, {
      width: n.width ?? opts.nodeWidth ?? DEFAULT_NODE_WIDTH,
      height: n.height ?? opts.nodeHeight ?? DEFAULT_NODE_HEIGHT,
    });
  }

  const roots = new Set(opts.rootNodes ?? []);
  for (const e of opts.edges) {
    if (!ids.has(e.source) || !ids.has(e.target)) continue;
    if (e.source === e.target) continue;
    // Force root: strip every edge that lands ON a root node so the
    // chosen roots have no incoming dependency and dagre assigns them
    // rank 0 naturally.
    if (roots.has(e.target)) continue;
    g.setEdge(e.source, e.target);
  }

  dagre.layout(g);

  for (const id of ids) {
    const node = g.node(id) as { x?: number; y?: number } | undefined;
    if (
      node &&
      typeof node.x === 'number' &&
      typeof node.y === 'number' &&
      Number.isFinite(node.x) &&
      Number.isFinite(node.y)
    ) {
      out.set(id, { x: node.x, y: node.y });
    }
  }
  return out;
}
