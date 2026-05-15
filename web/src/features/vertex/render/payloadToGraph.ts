// VTX-018: project a Vertex SystemGraph payload onto a renderable
// {nodes, edges} pair the Sigma.js / Graphology layer consumes directly.
//
// The function is intentionally pure so the renderer's heavy lifting stays
// out of jsdom — it can be benchmarked, snapshot-tested, and replayed in
// isolation without booting WebGL. Layout, selection, sidebars, etc. land
// in follow-up stories; this story is just the node/edge projection.
//
// Layout: when payload.positions[<objectRid>] is set we use those
// coordinates verbatim; otherwise nodes fall onto a deterministic
// circle so first paint has SOMETHING to draw before VTX-022's dagre /
// ForceAtlas2 pass swaps them out. The circle radius scales with node
// count so 50 vs 5000 nodes both land in roughly the same on-screen box.

import { pickEdgeArrowStyle } from '../links/edgeArrowStyle';

export interface VertexNode {
  id: string;
  label: string;
  x: number;
  y: number;
  size: number;
  color: string;
}

export interface VertexEdge {
  key: string;
  source: string;
  target: string;
  type: 'arrow' | 'line';
  /** When true, the renderer should overlay a reverse arrowhead. */
  bothArrows?: boolean;
}

export interface VertexPayloadGraph {
  nodes: VertexNode[];
  edges: VertexEdge[];
}

const DEFAULT_NODE_SIZE = 8;
const DEFAULT_NODE_COLOR = '#6B7280';
const BASE_RADIUS = 100;

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

function readPosition(
  positions: Record<string, unknown> | undefined,
  rid: string,
): { x: number; y: number } | null {
  if (!positions) return null;
  const p = positions[rid];
  if (!isObject(p)) return null;
  const x = p.x;
  const y = p.y;
  if (typeof x !== 'number' || typeof y !== 'number') return null;
  if (!Number.isFinite(x) || !Number.isFinite(y)) return null;
  return { x, y };
}

export function payloadToGraph(payload: unknown): VertexPayloadGraph {
  if (!isObject(payload)) return { nodes: [], edges: [] };
  const layersIn = Array.isArray(payload.layers) ? payload.layers : [];
  const edgesIn = Array.isArray(payload.edges) ? payload.edges : [];
  const positions = isObject(payload.positions)
    ? (payload.positions as Record<string, unknown>)
    : undefined;

  // Pass 1: count total objects so the fallback circle layout can spread
  // them evenly. We also dedupe by objectRid: the same physical entity may
  // appear in multiple layers (e.g. "Airport JFK" surfaced once via filter
  // and once via search-around) and we want one canvas node either way.
  let total = 0;
  for (const layer of layersIn) {
    if (!isObject(layer)) continue;
    const objects = Array.isArray(layer.objects) ? layer.objects : [];
    total += objects.length;
  }
  const radius = BASE_RADIUS * Math.sqrt(Math.max(total, 1));

  const nodes: VertexNode[] = [];
  const seen = new Set<string>();
  let idx = 0;
  for (const layer of layersIn) {
    if (!isObject(layer)) continue;
    const objects = Array.isArray(layer.objects) ? layer.objects : [];
    for (const obj of objects) {
      if (!isObject(obj)) continue;
      const rid = obj.objectRid;
      if (typeof rid !== 'string' || rid === '' || seen.has(rid)) continue;
      seen.add(rid);
      const props = isObject(obj.properties) ? obj.properties : {};
      const name = typeof props.name === 'string' && props.name !== ''
        ? props.name
        : rid;
      const pos = readPosition(positions, rid);
      let x: number;
      let y: number;
      if (pos !== null) {
        x = pos.x;
        y = pos.y;
      } else {
        const angle = (idx / Math.max(total, 1)) * 2 * Math.PI;
        x = radius * Math.cos(angle);
        y = radius * Math.sin(angle);
      }
      nodes.push({
        id: rid,
        label: name,
        x,
        y,
        size: DEFAULT_NODE_SIZE,
        color: DEFAULT_NODE_COLOR,
      });
      idx++;
    }
  }

  const edges: VertexEdge[] = [];
  for (let i = 0; i < edgesIn.length; i++) {
    const e = edgesIn[i];
    if (!isObject(e)) continue;
    const source = e.source;
    const target = e.target;
    if (typeof source !== 'string' || typeof target !== 'string') continue;
    if (!seen.has(source) || !seen.has(target)) continue;
    const tags = Array.isArray(e.typeClasses) ? (e.typeClasses as string[]) : undefined;
    const style = pickEdgeArrowStyle(tags);
    const type: 'arrow' | 'line' = style === 'none' ? 'line' : 'arrow';
    const id = typeof e.id === 'string' && e.id !== ''
      ? e.id
      : `${source}__${target}__${i}`;
    const edge: VertexEdge = { key: id, source, target, type };
    if (style === 'both') edge.bothArrows = true;
    edges.push(edge);
  }

  return { nodes, edges };
}
