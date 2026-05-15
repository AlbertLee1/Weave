import { describe, it, expect } from 'vitest';

import { mergeAddedNodes, type AddedObject } from './mergeAddedNodes';
import type { VertexPayloadGraph } from './payloadToGraph';

const baseProjection: VertexPayloadGraph = {
  nodes: [
    {
      id: 'ri.airport.JFK',
      label: 'JFK',
      x: 0,
      y: 0,
      size: 8,
      color: '#6B7280',
    },
  ],
  edges: [],
};

describe('mergeAddedNodes (VTX-027)', () => {
  it('Given_emptyAddedList_When_merge_Then_returnsBaseProjectionUnchanged', () => {
    const out = mergeAddedNodes(baseProjection, []);
    expect(out.nodes).toHaveLength(1);
    expect(out.nodes[0].id).toBe('ri.airport.JFK');
    expect(out.edges).toEqual([]);
  });

  it('Given_fiveNewObjects_When_merge_Then_appendsFiveNodesWithFinitePositions', () => {
    const added: AddedObject[] = Array.from({ length: 5 }, (_, i) => ({
      rid: `ri.airport.NEW${i}`,
      label: `NEW${i}`,
    }));
    const out = mergeAddedNodes(baseProjection, added);
    expect(out.nodes).toHaveLength(6);
    for (let i = 0; i < 5; i++) {
      const n = out.nodes[1 + i];
      expect(n.id).toBe(`ri.airport.NEW${i}`);
      expect(n.label).toBe(`NEW${i}`);
      expect(Number.isFinite(n.x)).toBe(true);
      expect(Number.isFinite(n.y)).toBe(true);
    }
  });

  it('Given_addedRidAlreadyInBase_When_merge_Then_skipsDuplicate', () => {
    const out = mergeAddedNodes(baseProjection, [
      { rid: 'ri.airport.JFK', label: 'JFK (dup)' },
      { rid: 'ri.airport.LHR', label: 'LHR' },
    ]);
    // JFK was already in base; only LHR is appended.
    expect(out.nodes.map((n) => n.id)).toEqual(['ri.airport.JFK', 'ri.airport.LHR']);
  });

  it('Given_duplicatesWithinAddedList_When_merge_Then_dedupesByRid', () => {
    const out = mergeAddedNodes(baseProjection, [
      { rid: 'ri.airport.LHR', label: 'LHR' },
      { rid: 'ri.airport.LHR', label: 'LHR (again)' },
    ]);
    expect(out.nodes).toHaveLength(2);
    // First occurrence wins for label.
    expect(out.nodes[1].label).toBe('LHR');
  });

  it('Given_addedNodeMissingLabel_When_merge_Then_fallsBackToRid', () => {
    const out = mergeAddedNodes(baseProjection, [
      { rid: 'ri.airport.LHR', label: '' },
    ]);
    expect(out.nodes[1].label).toBe('ri.airport.LHR');
  });

  it('Given_baseEdges_When_merge_Then_baseEdgesArePreserved', () => {
    const proj: VertexPayloadGraph = {
      nodes: baseProjection.nodes,
      edges: [
        { key: 'e1', source: 'ri.airport.JFK', target: 'ri.airport.JFK', type: 'arrow' },
      ],
    };
    const out = mergeAddedNodes(proj, [{ rid: 'ri.airport.LHR', label: 'LHR' }]);
    expect(out.edges).toHaveLength(1);
    expect(out.edges[0].key).toBe('e1');
  });
});
