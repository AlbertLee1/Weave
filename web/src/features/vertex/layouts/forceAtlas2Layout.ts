// VTX-023: ForceAtlas2 layout for Vertex canvases.
//
// Thin adapter over `graphology-layout-forceatlas2`. Mirrors the
// hierarchicalLayout API: pure `(nodes, edges, opts) → Map<id, {x,y}>`
// so the math is Vitest-friendly and the React/Sigma plumbing stays in
// a 30-line LayoutApplier shell.

import Graph from 'graphology';
import forceAtlas2 from 'graphology-layout-forceatlas2';

import type { LayoutEdgeInput, LayoutNodeInput, LayoutPoint } from './hierarchicalLayout';

export interface ForceAtlas2LayoutOptions {
  nodes: LayoutNodeInput[];
  edges: LayoutEdgeInput[];
  /** Number of iterations to run. Defaults to 150 — enough for ~100 node graphs. */
  iterations?: number;
  /** Override gravity (default 1). */
  gravity?: number;
  /** Override scalingRatio (default 10). */
  scalingRatio?: number;
}

const DEFAULT_ITERATIONS = 150;
const DEFAULT_GRAVITY = 1;
const DEFAULT_SCALING_RATIO = 10;

/**
 * Layout `nodes` + `edges` via the ForceAtlas2 algorithm. Returns a
 * deterministic position map (the algorithm itself is deterministic
 * given identical inputs + iterations).
 */
export function forceAtlas2Layout(
  opts: ForceAtlas2LayoutOptions,
): Map<string, LayoutPoint> {
  const out = new Map<string, LayoutPoint>();
  if (opts.nodes.length === 0) return out;

  const g = new Graph();
  const ids: string[] = [];
  // Spread nodes around a small circle so ForceAtlas2 has a non-degenerate
  // starting configuration; the algorithm requires distinct initial
  // positions and will diverge if every node starts at the origin.
  const n = opts.nodes.length;
  let kept = 0;
  for (const node of opts.nodes) {
    if (!node.id || g.hasNode(node.id)) continue;
    const angle = (2 * Math.PI * kept) / Math.max(1, n);
    g.addNode(node.id, {
      x: Math.cos(angle) * 100,
      y: Math.sin(angle) * 100,
    });
    ids.push(node.id);
    kept++;
  }

  for (const edge of opts.edges) {
    if (!g.hasNode(edge.source) || !g.hasNode(edge.target)) continue;
    if (edge.source === edge.target) continue;
    if (g.hasEdge(edge.source, edge.target)) continue;
    g.addEdge(edge.source, edge.target);
  }

  const positions = forceAtlas2(g, {
    iterations: opts.iterations ?? DEFAULT_ITERATIONS,
    settings: {
      gravity: opts.gravity ?? DEFAULT_GRAVITY,
      scalingRatio: opts.scalingRatio ?? DEFAULT_SCALING_RATIO,
      strongGravityMode: false,
      barnesHutOptimize: false,
    },
  });

  for (const id of ids) {
    const p = positions[id];
    if (
      p &&
      typeof p.x === 'number' &&
      typeof p.y === 'number' &&
      Number.isFinite(p.x) &&
      Number.isFinite(p.y)
    ) {
      out.set(id, { x: p.x, y: p.y });
    } else {
      // Algorithm produced a non-finite value (NaN/Infinity) — fall back to
      // the deterministic seed position so the node still has a usable
      // coordinate downstream.
      const seedIndex = ids.indexOf(id);
      const angle = (2 * Math.PI * seedIndex) / Math.max(1, ids.length);
      out.set(id, { x: Math.cos(angle) * 100, y: Math.sin(angle) * 100 });
    }
  }
  return out;
}
