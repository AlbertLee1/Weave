import { describe, it, expect } from 'vitest';
import {
  payloadToGraph,
  type VertexEdge,
  type VertexNode,
  type VertexPayloadGraph,
} from './payloadToGraph';

const airportLayer = (count: number) => ({
  id: 'layer-airports',
  objectTypeRid: 'ri.ontology.main.object-type.airport',
  objectType: 'Airport',
  ontologyRid: 'ri.ontology.main.ontology.vtx',
  objects: Array.from({ length: count }, (_, i) => ({
    objectRid: `ri.ontology.main.object.airport.${i}`,
    properties: {
      name: `Airport ${i}`,
      code: `AP${i}`,
    },
  })),
});

const edgeBetween = (
  i: number,
  j: number,
  typeClasses?: string[],
): Record<string, unknown> => ({
  id: `edge-${i}-${j}`,
  linkTypeRid: 'ri.ontology.main.link-type.flight',
  source: `ri.ontology.main.object.airport.${i}`,
  target: `ri.ontology.main.object.airport.${j}`,
  ...(typeClasses ? { typeClasses } : {}),
});

describe('VTX-018 payloadToGraph', () => {
  it('Given_1Layer50Airports_When_payloadToGraph_Then_returns50NodesEachWithNameSubtitle', () => {
    const payload = {
      layers: [airportLayer(50)],
      edges: [],
    };
    const out: VertexPayloadGraph = payloadToGraph(payload);
    expect(out.nodes).toHaveLength(50);
    for (let i = 0; i < 50; i++) {
      const node: VertexNode = out.nodes[i];
      expect(node.id).toBe(`ri.ontology.main.object.airport.${i}`);
      expect(node.label).toBe(`Airport ${i}`);
      expect(typeof node.x).toBe('number');
      expect(typeof node.y).toBe('number');
      expect(Number.isFinite(node.x)).toBe(true);
      expect(Number.isFinite(node.y)).toBe(true);
      expect(node.size).toBeGreaterThan(0);
      expect(typeof node.color).toBe('string');
    }
  });

  it('Given_payloadObjectMissingNameProperty_When_payloadToGraph_Then_labelFallsBackToObjectRid', () => {
    const payload = {
      layers: [
        {
          objects: [
            { objectRid: 'ri.ontology.main.object.airport.JFK', properties: {} },
          ],
        },
      ],
      edges: [],
    };
    const out = payloadToGraph(payload);
    expect(out.nodes).toHaveLength(1);
    expect(out.nodes[0].label).toBe('ri.ontology.main.object.airport.JFK');
  });

  it('Given_positionsKeyedByObjectRid_When_payloadToGraph_Then_nodesUsePersistedXY', () => {
    const payload = {
      layers: [
        {
          objects: [
            { objectRid: 'ri.A', properties: { name: 'A' } },
            { objectRid: 'ri.B', properties: { name: 'B' } },
          ],
        },
      ],
      edges: [],
      positions: {
        'ri.A': { x: 12, y: -34, pinned: true },
        'ri.B': { x: 99, y: 100 },
      },
    };
    const out = payloadToGraph(payload);
    const byId = new Map(out.nodes.map((n) => [n.id, n]));
    expect(byId.get('ri.A')!.x).toBe(12);
    expect(byId.get('ri.A')!.y).toBe(-34);
    expect(byId.get('ri.B')!.x).toBe(99);
    expect(byId.get('ri.B')!.y).toBe(100);
  });

  it('Given_50Edges_When_payloadToGraph_Then_returns50EdgesAllWithArrowStyleByDefault', () => {
    const layer = airportLayer(50);
    const edges: Record<string, unknown>[] = [];
    for (let i = 0; i < 50; i++) {
      edges.push(edgeBetween(i, (i + 1) % 50));
    }
    const out = payloadToGraph({ layers: [layer], edges });
    expect(out.edges).toHaveLength(50);
    for (const e of out.edges) {
      const edge: VertexEdge = e;
      expect(edge.type).toBe('arrow');
      expect(edge.source).toMatch(/^ri\.ontology\.main\.object\.airport\./);
      expect(edge.target).toMatch(/^ri\.ontology\.main\.object\.airport\./);
    }
  });

  it('Given_EdgeTaggedUndirectional_When_payloadToGraph_Then_emitsLineEdge', () => {
    const layer = airportLayer(2);
    const edges = [edgeBetween(0, 1, ['vertex:link_undirectional'])];
    const out = payloadToGraph({ layers: [layer], edges });
    expect(out.edges).toHaveLength(1);
    expect(out.edges[0].type).toBe('line');
  });

  it('Given_EdgeTaggedBidirectional_When_payloadToGraph_Then_emitsArrowEdgeWithBothFlag', () => {
    const layer = airportLayer(2);
    const edges = [edgeBetween(0, 1, ['vertex:link_bidirectional'])];
    const out = payloadToGraph({ layers: [layer], edges });
    expect(out.edges).toHaveLength(1);
    expect(out.edges[0].type).toBe('arrow');
    // bidirectional edges request a reverse arrowhead so the renderer can
    // pick a double-arrow programmable type when one is registered.
    expect(out.edges[0].bothArrows).toBe(true);
  });

  it('Given_PayloadEdgesCarryLinkTypeRid_When_payloadToGraph_Then_preservesLinkTypeRidOnEachEdge', () => {
    // VTX-025: the merge reducer groups same-direction edges by their
    // (source, target, linkTypeRid) triple, so the projection layer has
    // to keep linkTypeRid around — pickEdgeArrowStyle only needs the tag
    // list, but downstream consumers need the RID too.
    const layer = airportLayer(3);
    const edges = [
      edgeBetween(0, 1),
      edgeBetween(0, 1),
      edgeBetween(0, 2),
    ];
    const out = payloadToGraph({ layers: [layer], edges });
    expect(out.edges).toHaveLength(3);
    for (const e of out.edges) {
      expect(e.linkTypeRid).toBe('ri.ontology.main.link-type.flight');
    }
  });

  it('Given_EdgeWithUnknownEndpoint_When_payloadToGraph_Then_dropsTheDanglingEdge', () => {
    const layer = airportLayer(1);
    const edges = [
      // 0 → 99 — 99 does not exist
      edgeBetween(0, 99),
    ];
    const out = payloadToGraph({ layers: [layer], edges });
    expect(out.edges).toHaveLength(0);
  });

  it('Given_DuplicateObjectRidAcrossLayers_When_payloadToGraph_Then_emitsSingleNode', () => {
    const dup = {
      objectRid: 'ri.x',
      properties: { name: 'X' },
    };
    const payload = {
      layers: [
        { objectType: 'A', objects: [dup] },
        { objectType: 'B', objects: [dup] },
      ],
      edges: [],
    };
    const out = payloadToGraph(payload);
    expect(out.nodes).toHaveLength(1);
  });

  it('Given_OpaqueOrEmptyPayload_When_payloadToGraph_Then_returnsEmptyGraphWithoutThrowing', () => {
    expect(payloadToGraph(null).nodes).toEqual([]);
    expect(payloadToGraph(undefined).edges).toEqual([]);
    expect(payloadToGraph({}).nodes).toEqual([]);
    expect(payloadToGraph({ layers: 'not-an-array' }).nodes).toEqual([]);
  });

  it('Given_5000Nodes_When_payloadToGraph_Then_completesUnderThePerfBudget', () => {
    const big = airportLayer(5000);
    const start = performance.now();
    const out = payloadToGraph({ layers: [big], edges: [] });
    const elapsedMs = performance.now() - start;
    expect(out.nodes).toHaveLength(5000);
    // The BDD demands 5000-node first paint ≤ 2 s. The pure-JS projection
    // must leave the entire 2 s budget to Sigma's WebGL pass, so we hold the
    // helper to a far tighter 500 ms ceiling — well within dev-machine
    // headroom and 4× tighter than the spec gate.
    expect(elapsedMs).toBeLessThan(500);
  });
});
