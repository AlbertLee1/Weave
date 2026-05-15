// VTX-023: Circular layout for Vertex canvases.
//
// Thin adapter over `graphology-layout/circular`. The upstream package
// exposes a `scale` + `center` pair where `center` is a normalized
// 0..1 offset that gets multiplied by `scale` to produce the actual
// translation — easy to misuse. We expose a saner `radius` + explicit
// `centerX` / `centerY` contract instead, and translate to the library's
// parameters internally (run at `center: 0.5` so offset = 0, then apply
// our own translation).

import Graph from 'graphology';
import circular from 'graphology-layout/circular';

import {
  applyPinnedOverrides,
  type LayoutEdgeInput,
  type LayoutNodeInput,
  type LayoutPoint,
} from './hierarchicalLayout';

export interface CircularLayoutOptions {
  nodes: LayoutNodeInput[];
  edges?: LayoutEdgeInput[];
  /**
   * Radius of the layout circle in canvas units. Default 200 px.
   * Mapped onto graphology-layout/circular's `scale` parameter.
   */
  radius?: number;
  /** X coordinate of the circle centre. Default 0. */
  centerX?: number;
  /** Y coordinate of the circle centre. Default 0. */
  centerY?: number;
  /**
   * Test-only knob: pass a single uniform translation applied to BOTH
   * x and y. Lets the unit test exercise the offset path without having
   * to spell out centerX/centerY separately.
   */
  center?: number;
  /** Deprecated alias for `radius`; preserved for symmetry with the upstream API. */
  scale?: number;
  /**
   * VTX-024: nodes whose coordinates are user-fixed. The layout still runs
   * on the full graph, but pinned ids are overwritten in the returned map
   * with the supplied coords so they stay put across re-layouts.
   */
  pinnedPositions?: Map<string, LayoutPoint>;
}

const DEFAULT_RADIUS = 200;

/**
 * Place each node on a circle of radius `radius` centred at
 * (centerX, centerY). Edges are accepted for API symmetry with the
 * other layouts but are ignored — circular placement is a function of
 * node insertion order only.
 */
export function circularLayout(
  opts: CircularLayoutOptions,
): Map<string, LayoutPoint> {
  const out = new Map<string, LayoutPoint>();
  if (opts.nodes.length === 0) return out;

  const g = new Graph();
  const ids: string[] = [];
  for (const node of opts.nodes) {
    if (!node.id || g.hasNode(node.id)) continue;
    g.addNode(node.id);
    ids.push(node.id);
  }

  const radius = opts.radius ?? opts.scale ?? DEFAULT_RADIUS;
  // `center` (test alias) translates BOTH dimensions uniformly. Otherwise
  // pull from the explicit centerX/centerY pair (default 0,0).
  const cx = opts.center ?? opts.centerX ?? 0;
  const cy = opts.center ?? opts.centerY ?? 0;

  // Run upstream with center=0.5 so its internal offset (center - 0.5) * scale
  // collapses to 0 — we apply our own translation in the loop below for
  // an unambiguous contract.
  const positions = circular(g, { scale: radius, center: 0.5 });

  for (const id of ids) {
    const p = positions[id];
    if (
      p &&
      typeof p.x === 'number' &&
      typeof p.y === 'number' &&
      Number.isFinite(p.x) &&
      Number.isFinite(p.y)
    ) {
      out.set(id, { x: p.x + cx, y: p.y + cy });
    }
  }
  applyPinnedOverrides(out, ids, opts.pinnedPositions);
  return out;
}
