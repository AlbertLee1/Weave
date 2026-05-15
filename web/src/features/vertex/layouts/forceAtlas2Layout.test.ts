// VTX-023: pure ForceAtlas2 layout helper backed by graphology-layout-forceatlas2.
// Lives outside any React / Sigma surface so the math is Vitest-friendly.

import { describe, it, expect } from 'vitest';
import { forceAtlas2Layout } from './forceAtlas2Layout';

describe('forceAtlas2Layout (VTX-023)', () => {
  it('Given_threeNodesInATriangle_When_layoutRuns_Then_returnsFinitePositionsForEveryNode', () => {
    const positions = forceAtlas2Layout({
      nodes: [{ id: 'a' }, { id: 'b' }, { id: 'c' }],
      edges: [
        { source: 'a', target: 'b' },
        { source: 'b', target: 'c' },
        { source: 'c', target: 'a' },
      ],
    });
    expect(positions.size).toBe(3);
    for (const id of ['a', 'b', 'c']) {
      const p = positions.get(id);
      expect(p).toBeDefined();
      expect(Number.isFinite(p!.x)).toBe(true);
      expect(Number.isFinite(p!.y)).toBe(true);
    }
  });

  it('Given_disconnectedClusters_When_layoutRuns_Then_clusterMembersStayCloserToOwnCluster', () => {
    // Two triangles, no inter-cluster edges. ForceAtlas2 attraction should
    // keep each cluster's three members closer to one another than to the
    // other cluster's centroid.
    const positions = forceAtlas2Layout({
      nodes: [
        { id: 'a1' }, { id: 'a2' }, { id: 'a3' },
        { id: 'b1' }, { id: 'b2' }, { id: 'b3' },
      ],
      edges: [
        { source: 'a1', target: 'a2' },
        { source: 'a2', target: 'a3' },
        { source: 'a3', target: 'a1' },
        { source: 'b1', target: 'b2' },
        { source: 'b2', target: 'b3' },
        { source: 'b3', target: 'b1' },
      ],
      iterations: 100,
    });
    expect(positions.size).toBe(6);
    function dist(p: string, q: string): number {
      const a = positions.get(p)!;
      const b = positions.get(q)!;
      return Math.hypot(a.x - b.x, a.y - b.y);
    }
    // Distance between connected cluster members should be smaller than
    // the distance between members of different clusters.
    const intra = dist('a1', 'a2');
    const inter = dist('a1', 'b1');
    expect(intra).toBeLessThan(inter);
  });

  it('Given_isolatedNode_When_layoutRuns_Then_isolatedNodeStillGetsPosition', () => {
    const positions = forceAtlas2Layout({
      nodes: [{ id: 'isolated' }, { id: 'a' }, { id: 'b' }],
      edges: [{ source: 'a', target: 'b' }],
    });
    expect(positions.size).toBe(3);
    const iso = positions.get('isolated');
    expect(iso).toBeDefined();
    expect(Number.isFinite(iso!.x)).toBe(true);
    expect(Number.isFinite(iso!.y)).toBe(true);
  });

  it('Given_emptyInput_When_layoutRuns_Then_returnsEmptyMap', () => {
    const positions = forceAtlas2Layout({ nodes: [], edges: [] });
    expect(positions.size).toBe(0);
  });

  it('Given_duplicateNodeIds_When_layoutRuns_Then_dedupesByFirstOccurrence', () => {
    const positions = forceAtlas2Layout({
      nodes: [{ id: 'a' }, { id: 'a' }, { id: 'b' }],
      edges: [{ source: 'a', target: 'b' }],
    });
    expect(positions.size).toBe(2);
    expect(positions.has('a')).toBe(true);
    expect(positions.has('b')).toBe(true);
  });

  it('Given_edgesReferencingUnknownNodes_When_layoutRuns_Then_edgesSilentlyDropped', () => {
    const positions = forceAtlas2Layout({
      nodes: [{ id: 'a' }, { id: 'b' }],
      edges: [
        { source: 'a', target: 'b' },
        { source: 'a', target: 'ghost' },
        { source: 'ghost', target: 'b' },
      ],
    });
    expect(positions.size).toBe(2);
  });

  it('Given_selfEdge_When_layoutRuns_Then_isIgnoredAndDoesNotCrash', () => {
    const positions = forceAtlas2Layout({
      nodes: [{ id: 'a' }, { id: 'b' }],
      edges: [
        { source: 'a', target: 'a' },
        { source: 'a', target: 'b' },
      ],
    });
    expect(positions.size).toBe(2);
  });

  it('Given_singletonGraph_When_layoutRuns_Then_returnsFiniteOrigin', () => {
    const positions = forceAtlas2Layout({ nodes: [{ id: 'solo' }], edges: [] });
    expect(positions.size).toBe(1);
    const p = positions.get('solo');
    expect(p).toBeDefined();
    expect(Number.isFinite(p!.x)).toBe(true);
    expect(Number.isFinite(p!.y)).toBe(true);
  });
});
