export interface GraphEdge {
  src: string;
  dst: string;
}

export interface GraphSnapshot {
  nodes: Set<string>;
  edges: GraphEdge[];
}

export interface ExpansionCheck {
  neighborCount: number;
  maxNodes: number;
}

export type ExpansionResult =
  | { ok: true }
  | { ok: false; message: string };

export function checkExpansionLimits(check: ExpansionCheck): ExpansionResult {
  if (check.neighborCount > check.maxNodes) {
    return {
      ok: false,
      message: 'Too many neighbors. Use filter or limit depth.',
    };
  }
  return { ok: true };
}

export function collapseUniqueNeighbors(
  graph: GraphSnapshot,
  hubId: string,
): Set<string> {
  const removed = new Set<string>();
  if (!graph.nodes.has(hubId)) return removed;

  // neighbors = nodes directly connected to hub (either direction)
  const neighbors = new Set<string>();
  for (const e of graph.edges) {
    if (e.src === hubId) neighbors.add(e.dst);
    if (e.dst === hubId) neighbors.add(e.src);
  }

  for (const n of neighbors) {
    let touchesOther = false;
    for (const e of graph.edges) {
      if (e.src === n && e.dst !== hubId) {
        touchesOther = true;
        break;
      }
      if (e.dst === n && e.src !== hubId) {
        touchesOther = true;
        break;
      }
    }
    if (!touchesOther) removed.add(n);
  }
  return removed;
}
