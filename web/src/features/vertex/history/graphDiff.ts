export interface GraphNodeV2 {
  id: string;
  objectType: string;
  style?: Record<string, unknown>;
}

export interface GraphEdgeV2 {
  src: string;
  dst: string;
}

export interface GraphPayloadV2 {
  name: string;
  nodes: GraphNodeV2[];
  edges: GraphEdgeV2[];
}

export interface NodeChange {
  id: string;
  before: GraphNodeV2;
  after: GraphNodeV2;
}

export interface GraphDiff {
  nodesAdded: GraphNodeV2[];
  nodesRemoved: GraphNodeV2[];
  nodesChanged: NodeChange[];
  edgesAdded: GraphEdgeV2[];
  edgesRemoved: GraphEdgeV2[];
}

function edgeKey(e: GraphEdgeV2): string {
  return `${e.src}→${e.dst}`;
}

function nodeStylesEqual(a: GraphNodeV2, b: GraphNodeV2): boolean {
  return (
    a.objectType === b.objectType &&
    JSON.stringify(a.style ?? {}) === JSON.stringify(b.style ?? {})
  );
}

export function diffGraphPayloads(
  before: GraphPayloadV2,
  after: GraphPayloadV2,
): GraphDiff {
  const beforeNodes = new Map(before.nodes.map((n) => [n.id, n]));
  const afterNodes = new Map(after.nodes.map((n) => [n.id, n]));

  const nodesAdded: GraphNodeV2[] = [];
  const nodesRemoved: GraphNodeV2[] = [];
  const nodesChanged: NodeChange[] = [];

  for (const [id, n] of afterNodes) {
    const b = beforeNodes.get(id);
    if (!b) {
      nodesAdded.push(n);
    } else if (!nodeStylesEqual(b, n)) {
      nodesChanged.push({ id, before: b, after: n });
    }
  }
  for (const [id, n] of beforeNodes) {
    if (!afterNodes.has(id)) nodesRemoved.push(n);
  }

  const beforeEdges = new Map(before.edges.map((e) => [edgeKey(e), e]));
  const afterEdges = new Map(after.edges.map((e) => [edgeKey(e), e]));

  const edgesAdded: GraphEdgeV2[] = [];
  const edgesRemoved: GraphEdgeV2[] = [];

  for (const [k, e] of afterEdges) if (!beforeEdges.has(k)) edgesAdded.push(e);
  for (const [k, e] of beforeEdges) if (!afterEdges.has(k)) edgesRemoved.push(e);

  return { nodesAdded, nodesRemoved, nodesChanged, edgesAdded, edgesRemoved };
}
