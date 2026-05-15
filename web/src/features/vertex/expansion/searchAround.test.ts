import { describe, it, expect } from 'vitest';
import {
  checkExpansionLimits,
  collapseUniqueNeighbors,
  type GraphSnapshot,
} from './searchAround';

describe('VTX-069 checkExpansionLimits', () => {
  it('given_200Max_and_201Neighbors_then_RefusesWithMessage', () => {
    const r = checkExpansionLimits({ neighborCount: 201, maxNodes: 200 });
    if (r.ok) throw new Error('expected refusal');
    expect(r.message).toMatch(/Too many neighbors/);
  });

  it('given_200Max_and_200Neighbors_then_Allows', () => {
    expect(checkExpansionLimits({ neighborCount: 200, maxNodes: 200 }).ok).toBe(true);
  });

  it('given_200Max_and_0_then_Allows', () => {
    expect(checkExpansionLimits({ neighborCount: 0, maxNodes: 200 }).ok).toBe(true);
  });
});

describe('VTX-069 collapseUniqueNeighbors', () => {
  const graph: GraphSnapshot = {
    nodes: new Set(['hub', 'a', 'b', 'c', 'd']),
    edges: [
      { src: 'hub', dst: 'a' },
      { src: 'hub', dst: 'b' },
      { src: 'hub', dst: 'c' },
      { src: 'd', dst: 'a' },
    ],
  };

  it('given_HubWith3Neighbors_when_Collapse_then_RemovesOnlyOrphans', () => {
    // b and c only touch hub → collapse them.
    // a also touches d → keep it.
    const removed = collapseUniqueNeighbors(graph, 'hub');
    expect([...removed].sort()).toEqual(['b', 'c']);
  });

  it('given_NodeNotInGraph_then_RemovesNothing', () => {
    expect(collapseUniqueNeighbors(graph, 'zzz')).toEqual(new Set());
  });

  it('given_NodeWithNoNeighbors_then_RemovesNothing', () => {
    const g: GraphSnapshot = { nodes: new Set(['alone']), edges: [] };
    expect(collapseUniqueNeighbors(g, 'alone')).toEqual(new Set());
  });

  it('given_AllNeighborsAreOrphans_then_AllRemoved', () => {
    const g: GraphSnapshot = {
      nodes: new Set(['hub', 'a', 'b']),
      edges: [
        { src: 'hub', dst: 'a' },
        { src: 'hub', dst: 'b' },
      ],
    };
    expect([...collapseUniqueNeighbors(g, 'hub')].sort()).toEqual(['a', 'b']);
  });

  it('given_BidirectionalEdge_when_NeighborOnlyReferencesHub_then_Removed', () => {
    const g: GraphSnapshot = {
      nodes: new Set(['hub', 'a']),
      edges: [
        { src: 'hub', dst: 'a' },
        { src: 'a', dst: 'hub' },
      ],
    };
    expect([...collapseUniqueNeighbors(g, 'hub')]).toEqual(['a']);
  });
});
