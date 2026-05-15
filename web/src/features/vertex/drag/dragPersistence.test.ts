// VTX-024 — pure helpers for the drag-persistence wire. The page-level
// integration (Sigma event subscription + fetch call) is tested in
// VertexWorkspacePage.test.tsx; this file covers the bag of pure-JS pieces
// that flank it.

import { describe, expect, it } from 'vitest';

import {
  formatLayoutPatchBody,
  pinnedPositionsFromPayload,
} from './dragPersistence';

describe('VTX-024 pinnedPositionsFromPayload', () => {
  it('reads pinned=true entries from payload.positions', () => {
    const payload = {
      layers: [],
      positions: {
        A: { x: 10, y: 20, pinned: true },
        B: { x: 30, y: 40, pinned: false },
        C: { x: 50, y: 60 }, // missing pinned → not pinned
      },
    };
    const pinned = pinnedPositionsFromPayload(payload);
    expect(pinned.size).toBe(1);
    expect(pinned.get('A')).toEqual({ x: 10, y: 20 });
    expect(pinned.has('B')).toBe(false);
    expect(pinned.has('C')).toBe(false);
  });

  it('returns empty map when payload.positions is missing', () => {
    expect(pinnedPositionsFromPayload({ layers: [] }).size).toBe(0);
    expect(pinnedPositionsFromPayload(null).size).toBe(0);
    expect(pinnedPositionsFromPayload(undefined).size).toBe(0);
  });

  it('rejects entries with non-numeric coords', () => {
    const payload = {
      positions: {
        Bad: { x: 'oops', y: 20, pinned: true },
        Inf: { x: Infinity, y: 1, pinned: true },
        Good: { x: 0, y: 0, pinned: true },
      },
    };
    const pinned = pinnedPositionsFromPayload(payload);
    expect(pinned.size).toBe(1);
    expect(pinned.get('Good')).toEqual({ x: 0, y: 0 });
  });
});

describe('VTX-024 formatLayoutPatchBody', () => {
  it('emits {positions:{[id]:{x,y,pinned}}} for a single drag commit', () => {
    expect(formatLayoutPatchBody('ri.airport.JFK', 12.5, -7.25, true)).toEqual({
      positions: {
        'ri.airport.JFK': { x: 12.5, y: -7.25, pinned: true },
      },
    });
  });

  it('emits pinned=false for unpin requests', () => {
    expect(formatLayoutPatchBody('A', 1, 2, false)).toEqual({
      positions: { A: { x: 1, y: 2, pinned: false } },
    });
  });
});
