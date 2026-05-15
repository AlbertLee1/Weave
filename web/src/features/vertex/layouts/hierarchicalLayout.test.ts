// VTX-022: pure hierarchical layout helper backed by @dagrejs/dagre.
// Lives outside any React / Sigma surface so it can be unit-tested
// without booting jsdom.

import { describe, it, expect } from 'vitest';
import { hierarchicalLayout } from './hierarchicalLayout';

describe('hierarchicalLayout (VTX-022)', () => {
  const chainGraph = {
    nodes: [{ id: 'a' }, { id: 'b' }, { id: 'c' }],
    edges: [
      { source: 'a', target: 'b' },
      { source: 'b', target: 'c' },
    ],
  };

  it('Given_threeNodesInAChain_When_layoutRuns_Then_returnsFinitePositionsForEveryNode', () => {
    const positions = hierarchicalLayout({ ...chainGraph });
    expect(positions.size).toBe(3);
    for (const id of ['a', 'b', 'c']) {
      const p = positions.get(id);
      expect(p).toBeDefined();
      expect(Number.isFinite(p!.x)).toBe(true);
      expect(Number.isFinite(p!.y)).toBe(true);
    }
  });

  it('Given_chainAToBToC_When_layoutRunsTopDown_Then_yIncreasesAlongTheChain', () => {
    const positions = hierarchicalLayout({ ...chainGraph });
    // Top-down (rankdir TB) means the root (rank 0) gets the smallest y
    // and leaves get progressively larger y.
    const ya = positions.get('a')!.y;
    const yb = positions.get('b')!.y;
    const yc = positions.get('c')!.y;
    expect(ya).toBeLessThan(yb);
    expect(yb).toBeLessThan(yc);
  });

  it('Given_reverseTrue_When_layoutRuns_Then_directionFlipsBottomUp', () => {
    const positions = hierarchicalLayout({ ...chainGraph, reverse: true });
    const ya = positions.get('a')!.y;
    const yb = positions.get('b')!.y;
    const yc = positions.get('c')!.y;
    // BT (bottom-to-top) flips the y axis: root sits at the BOTTOM
    // (largest y) and leaves climb upward.
    expect(ya).toBeGreaterThan(yb);
    expect(yb).toBeGreaterThan(yc);
  });

  it('Given_explicitRootNodes_When_layoutRuns_Then_rootIsPlacedInTopTier', () => {
    // Two children of 'a': b and c. Default layout puts 'a' at the top.
    // We force 'c' to be a root — it should climb to the top tier so its
    // y is no greater than 'a''s y.
    const positions = hierarchicalLayout({
      nodes: [{ id: 'a' }, { id: 'b' }, { id: 'c' }],
      edges: [
        { source: 'a', target: 'b' },
        { source: 'a', target: 'c' },
      ],
      rootNodes: ['c'],
    });
    expect(positions.get('c')!.y).toBeLessThanOrEqual(positions.get('a')!.y);
  });

  it('Given_isolatedNode_When_layoutRuns_Then_isolatedNodeStillGetsPosition', () => {
    const positions = hierarchicalLayout({
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
    const positions = hierarchicalLayout({ nodes: [], edges: [] });
    expect(positions.size).toBe(0);
  });

  it('Given_duplicateNodeIds_When_layoutRuns_Then_dedupesByFirstOccurrence', () => {
    const positions = hierarchicalLayout({
      nodes: [{ id: 'a' }, { id: 'a' }, { id: 'b' }],
      edges: [{ source: 'a', target: 'b' }],
    });
    expect(positions.size).toBe(2);
    expect(positions.has('a')).toBe(true);
    expect(positions.has('b')).toBe(true);
  });

  it('Given_edgesReferencingUnknownNodes_When_layoutRuns_Then_edgesSilentlyDropped', () => {
    const positions = hierarchicalLayout({
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
    const positions = hierarchicalLayout({
      nodes: [{ id: 'a' }, { id: 'b' }],
      edges: [
        { source: 'a', target: 'a' },
        { source: 'a', target: 'b' },
      ],
    });
    expect(positions.size).toBe(2);
  });
});
