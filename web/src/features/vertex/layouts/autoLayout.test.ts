// VTX-023: Auto-mode heuristic. < 100 nodes → force-directed,
// otherwise → hierarchical.

import { describe, it, expect } from 'vitest';
import { pickAutoLayoutKind } from './autoLayout';

describe('pickAutoLayoutKind (VTX-023)', () => {
  it('Given_nodeCountUnder100_When_pickRuns_Then_returnsForce', () => {
    expect(pickAutoLayoutKind(0)).toBe('force');
    expect(pickAutoLayoutKind(1)).toBe('force');
    expect(pickAutoLayoutKind(50)).toBe('force');
    expect(pickAutoLayoutKind(99)).toBe('force');
  });

  it('Given_nodeCount100OrMore_When_pickRuns_Then_returnsHierarchical', () => {
    expect(pickAutoLayoutKind(100)).toBe('hierarchical');
    expect(pickAutoLayoutKind(500)).toBe('hierarchical');
    expect(pickAutoLayoutKind(5000)).toBe('hierarchical');
  });
});
