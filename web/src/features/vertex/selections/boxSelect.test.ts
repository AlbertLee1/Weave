import { describe, it, expect } from 'vitest';

import { nodesInRect, rectFromCorners, type ViewportRect } from './boxSelect';

describe('VTX-020 boxSelect helpers', () => {
  describe('rectFromCorners', () => {
    it('Given_twoCornersInOrder_When_buildRect_Then_minMaxBoxReturned', () => {
      const r = rectFromCorners({ x: 10, y: 20 }, { x: 110, y: 220 });
      expect(r).toEqual<ViewportRect>({ x: 10, y: 20, width: 100, height: 200 });
    });

    it('Given_endIsAboveStart_When_buildRect_Then_normalisesToTopLeftAndSize', () => {
      const r = rectFromCorners({ x: 100, y: 200 }, { x: 50, y: 50 });
      expect(r).toEqual<ViewportRect>({ x: 50, y: 50, width: 50, height: 150 });
    });

    it('Given_sameCorner_When_buildRect_Then_zeroSizeRect', () => {
      const r = rectFromCorners({ x: 42, y: 42 }, { x: 42, y: 42 });
      expect(r.width).toBe(0);
      expect(r.height).toBe(0);
    });
  });

  describe('nodesInRect', () => {
    const positions = new Map<string, { x: number; y: number }>([
      ['ri.A', { x: 10, y: 10 }],
      ['ri.B', { x: 60, y: 60 }],
      ['ri.C', { x: 120, y: 120 }],
      ['ri.D', { x: -5, y: 20 }],
    ]);

    it('Given_rectContainingAB_When_query_Then_returnsAandB', () => {
      const rect: ViewportRect = { x: 0, y: 0, width: 80, height: 80 };
      const got = new Set(nodesInRect(rect, positions));
      expect(got.has('ri.A')).toBe(true);
      expect(got.has('ri.B')).toBe(true);
      expect(got.has('ri.C')).toBe(false);
      expect(got.has('ri.D')).toBe(false);
    });

    it('Given_zeroSizeRect_When_query_Then_returnsEmpty', () => {
      const rect: ViewportRect = { x: 50, y: 50, width: 0, height: 0 };
      expect(nodesInRect(rect, positions)).toEqual([]);
    });

    it('Given_pointOnRectEdge_When_query_Then_isIncluded', () => {
      // ri.B sits exactly on (60,60); rect [60,60..60,60] is degenerate (excluded);
      // rect [50..60, 50..60] should INCLUDE the corner point.
      const rect: ViewportRect = { x: 50, y: 50, width: 10, height: 10 };
      expect(nodesInRect(rect, positions)).toContain('ri.B');
    });

    it('Given_emptyPositionMap_When_query_Then_returnsEmpty', () => {
      const rect: ViewportRect = { x: 0, y: 0, width: 1000, height: 1000 };
      expect(nodesInRect(rect, new Map())).toEqual([]);
    });
  });
});
