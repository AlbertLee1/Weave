// VTX-023: pure Circular layout helper backed by graphology-layout/circular.

import { describe, it, expect } from 'vitest';
import { circularLayout } from './circularLayout';

describe('circularLayout (VTX-023)', () => {
  it('Given_fourNodes_When_layoutRuns_Then_allLandOnSameCircle', () => {
    const positions = circularLayout({
      nodes: [{ id: 'a' }, { id: 'b' }, { id: 'c' }, { id: 'd' }],
      edges: [],
      scale: 100,
      center: 0,
    });
    expect(positions.size).toBe(4);
    // graphology-layout/circular places nodes uniformly around a circle of
    // the given radius. Every node should sit within a tight tolerance of
    // the configured radius from the centre point.
    for (const id of ['a', 'b', 'c', 'd']) {
      const p = positions.get(id)!;
      expect(Number.isFinite(p.x)).toBe(true);
      expect(Number.isFinite(p.y)).toBe(true);
      const r = Math.hypot(p.x, p.y);
      expect(Math.abs(r - 100)).toBeLessThan(1e-6);
    }
  });

  it('Given_centerOffset_When_layoutRuns_Then_circleCentersOnGivenPoint', () => {
    const positions = circularLayout({
      nodes: [{ id: 'a' }, { id: 'b' }, { id: 'c' }, { id: 'd' }],
      edges: [],
      scale: 50,
      center: 200,
    });
    // graphology-layout/circular interprets `center` as a uniform offset
    // applied to every coordinate dimension; the wrapped helper preserves
    // that contract, so each node should sit at radius 50 from (200, 200).
    for (const id of ['a', 'b', 'c', 'd']) {
      const p = positions.get(id)!;
      const r = Math.hypot(p.x - 200, p.y - 200);
      expect(Math.abs(r - 50)).toBeLessThan(1e-6);
    }
  });

  it('Given_emptyInput_When_layoutRuns_Then_returnsEmptyMap', () => {
    const positions = circularLayout({ nodes: [], edges: [] });
    expect(positions.size).toBe(0);
  });

  it('Given_duplicateNodeIds_When_layoutRuns_Then_dedupesByFirstOccurrence', () => {
    const positions = circularLayout({
      nodes: [{ id: 'a' }, { id: 'a' }, { id: 'b' }],
      edges: [],
    });
    expect(positions.size).toBe(2);
    expect(positions.has('a')).toBe(true);
    expect(positions.has('b')).toBe(true);
  });

  it('Given_singletonGraph_When_layoutRuns_Then_returnsFinitePoint', () => {
    const positions = circularLayout({ nodes: [{ id: 'solo' }], edges: [] });
    expect(positions.size).toBe(1);
    const p = positions.get('solo')!;
    expect(Number.isFinite(p.x)).toBe(true);
    expect(Number.isFinite(p.y)).toBe(true);
  });
});
