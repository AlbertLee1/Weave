// VTX-024 — pinned nodes opt OUT of every layout algorithm. Once a user
// drags + persists a node's coordinates, re-running Hierarchical / Force /
// Circular / Auto must leave that node where the user put it while the
// unpinned siblings are free to reflow.
//
// The contract: each layout helper accepts an optional
// `pinnedPositions: Map<string, {x,y}>` and the returned Map carries each
// pinned id at the supplied coords. Unpinned ids may move freely.

import { describe, expect, it } from 'vitest';

import { hierarchicalLayout } from './hierarchicalLayout';
import { forceAtlas2Layout } from './forceAtlas2Layout';
import { circularLayout } from './circularLayout';

describe('VTX-024 hierarchicalLayout pinned', () => {
  it('returns supplied pinned coords verbatim and lets unpinned reflow', () => {
    const pinned = new Map([['A', { x: 999, y: -42 }]]);
    const positions = hierarchicalLayout({
      nodes: [{ id: 'A' }, { id: 'B' }, { id: 'C' }],
      edges: [
        { source: 'A', target: 'B' },
        { source: 'B', target: 'C' },
      ],
      pinnedPositions: pinned,
    });
    expect(positions.get('A')).toEqual({ x: 999, y: -42 });
    expect(positions.has('B')).toBe(true);
    expect(positions.has('C')).toBe(true);
    // B is unpinned and should NOT match the pinned coords.
    expect(positions.get('B')).not.toEqual({ x: 999, y: -42 });
  });

  it('ignores pinned ids unknown to the node set', () => {
    const pinned = new Map([['ghost', { x: 1, y: 2 }]]);
    const positions = hierarchicalLayout({
      nodes: [{ id: 'A' }, { id: 'B' }],
      edges: [{ source: 'A', target: 'B' }],
      pinnedPositions: pinned,
    });
    expect(positions.has('ghost')).toBe(false);
  });
});

describe('VTX-024 forceAtlas2Layout pinned', () => {
  it('returns supplied pinned coords verbatim', () => {
    const pinned = new Map([['A', { x: -1, y: -2 }]]);
    const positions = forceAtlas2Layout({
      nodes: [{ id: 'A' }, { id: 'B' }, { id: 'C' }],
      edges: [
        { source: 'A', target: 'B' },
        { source: 'B', target: 'C' },
      ],
      iterations: 50,
      pinnedPositions: pinned,
    });
    expect(positions.get('A')).toEqual({ x: -1, y: -2 });
  });
});

describe('VTX-024 circularLayout pinned', () => {
  it('returns supplied pinned coords verbatim', () => {
    const pinned = new Map([['A', { x: 7, y: 11 }]]);
    const positions = circularLayout({
      nodes: [{ id: 'A' }, { id: 'B' }, { id: 'C' }, { id: 'D' }],
      radius: 50,
      pinnedPositions: pinned,
    });
    expect(positions.get('A')).toEqual({ x: 7, y: 11 });
    // B/C/D should remain on the circle (radius 50 from origin) — i.e. unaffected by pinning.
    for (const id of ['B', 'C', 'D']) {
      const p = positions.get(id)!;
      const r = Math.hypot(p.x, p.y);
      expect(r).toBeCloseTo(50, 5);
    }
  });
});
