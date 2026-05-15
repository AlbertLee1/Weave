// VTX-025: tests for the graphology-style edge reducer that collapses
// same-direction same-LinkType edges into a single weighted edge.
//
// The BDD shape (PRD VTX-025):
//   Given A→B has 5 edges of the same LinkType AND Vertex setting merge=true
//     When the renderer projects the graph
//     Then the canvas shows 1 edge labeled ×5
//   Given the same payload AND merge=false
//     When the renderer projects the graph
//     Then the canvas shows 5 parallel edges (no merging)

import { describe, it, expect } from 'vitest';
import { mergeEdgesByLinkType } from './mergeEdges';
import type { VertexEdge } from './payloadToGraph';

function edge(
  key: string,
  source: string,
  target: string,
  linkTypeRid?: string,
  overrides: Partial<VertexEdge> = {},
): VertexEdge {
  return {
    key,
    source,
    target,
    type: 'arrow',
    ...(linkTypeRid !== undefined ? { linkTypeRid } : {}),
    ...overrides,
  };
}

describe('VTX-025 mergeEdgesByLinkType', () => {
  it('Given_mergeFalse_When_called_Then_returnsInputUnchanged', () => {
    const edges: VertexEdge[] = [
      edge('e1', 'A', 'B', 'ri.lt.flight'),
      edge('e2', 'A', 'B', 'ri.lt.flight'),
      edge('e3', 'A', 'B', 'ri.lt.flight'),
    ];
    const out = mergeEdgesByLinkType(edges, { merge: false });
    expect(out).toHaveLength(3);
    expect(out.map((e) => e.key)).toEqual(['e1', 'e2', 'e3']);
    // Each edge keeps the input shape — no count/label sneaking in.
    for (const e of out) {
      expect(e.count).toBeUndefined();
      expect(e.label).toBeUndefined();
    }
  });

  it('Given_5SameDirectionSameLinkType_When_merge_Then_collapsesToOneEdgeWithCount5AndTimesNLabel', () => {
    const edges: VertexEdge[] = Array.from({ length: 5 }, (_, i) =>
      edge(`e${i}`, 'A', 'B', 'ri.lt.flight'),
    );
    const out = mergeEdgesByLinkType(edges, { merge: true });
    expect(out).toHaveLength(1);
    expect(out[0].source).toBe('A');
    expect(out[0].target).toBe('B');
    expect(out[0].linkTypeRid).toBe('ri.lt.flight');
    expect(out[0].count).toBe(5);
    expect(out[0].label).toBe('×5');
  });

  it('Given_mergedEdge_When_called_Then_sizeIsThickerThanSingleEdge', () => {
    const single = mergeEdgesByLinkType(
      [edge('e0', 'A', 'B', 'ri.lt.flight')],
      { merge: true },
    );
    const many = mergeEdgesByLinkType(
      Array.from({ length: 5 }, (_, i) => edge(`e${i}`, 'A', 'B', 'ri.lt.flight')),
      { merge: true },
    );
    expect(single).toHaveLength(1);
    expect(many).toHaveLength(1);
    // size is the single visual cue that drives the rendered thickness;
    // a merge with count=5 must produce a thicker edge than the unmerged
    // single edge with count=1.
    const singleSize = single[0].size ?? 1;
    const manySize = many[0].size ?? 1;
    expect(manySize).toBeGreaterThan(singleSize);
  });

  it('Given_5EdgesAcrossTwoDistinctLinkTypes_When_merge_Then_producesOneMergedPerLinkType', () => {
    const edges: VertexEdge[] = [
      edge('e0', 'A', 'B', 'ri.lt.flight'),
      edge('e1', 'A', 'B', 'ri.lt.flight'),
      edge('e2', 'A', 'B', 'ri.lt.flight'),
      edge('e3', 'A', 'B', 'ri.lt.lease'),
      edge('e4', 'A', 'B', 'ri.lt.lease'),
    ];
    const out = mergeEdgesByLinkType(edges, { merge: true });
    expect(out).toHaveLength(2);
    const byLink = new Map(out.map((e) => [e.linkTypeRid, e]));
    expect(byLink.get('ri.lt.flight')?.count).toBe(3);
    expect(byLink.get('ri.lt.flight')?.label).toBe('×3');
    expect(byLink.get('ri.lt.lease')?.count).toBe(2);
    expect(byLink.get('ri.lt.lease')?.label).toBe('×2');
  });

  it('Given_directionFlipsOnOneEdge_When_merge_Then_doesNotMergeAcrossDirection', () => {
    const edges: VertexEdge[] = [
      edge('e0', 'A', 'B', 'ri.lt.flight'),
      edge('e1', 'B', 'A', 'ri.lt.flight'),
      edge('e2', 'A', 'B', 'ri.lt.flight'),
    ];
    const out = mergeEdgesByLinkType(edges, { merge: true });
    expect(out).toHaveLength(2);
    const ab = out.find((e) => e.source === 'A' && e.target === 'B')!;
    const ba = out.find((e) => e.source === 'B' && e.target === 'A')!;
    // A→B has 2 members → merged with count=2; B→A is a singleton, so it
    // stays count=undefined (matches the "singletonGroup" contract).
    expect(ab.count).toBe(2);
    expect(ba.count).toBeUndefined();
  });

  it('Given_edgesWithoutLinkTypeRid_When_merge_Then_leavesThemUngrouped', () => {
    const edges: VertexEdge[] = [
      edge('e0', 'A', 'B'),
      edge('e1', 'A', 'B'),
      edge('e2', 'A', 'B'),
    ];
    const out = mergeEdgesByLinkType(edges, { merge: true });
    expect(out).toHaveLength(3);
    for (const e of out) {
      expect(e.count).toBeUndefined();
      expect(e.label).toBeUndefined();
    }
  });

  it('Given_singletonGroup_When_merge_Then_doesNotAttachCountLabel', () => {
    const edges: VertexEdge[] = [edge('e0', 'A', 'B', 'ri.lt.flight')];
    const out = mergeEdgesByLinkType(edges, { merge: true });
    expect(out).toHaveLength(1);
    // A single edge in its group should render identically to the unmerged
    // case — no ×1 label clutter, no thickened size.
    expect(out[0].count).toBeUndefined();
    expect(out[0].label).toBeUndefined();
  });

  it('Given_mergedEdge_When_called_Then_preservesArrowStyleFromFirstMember', () => {
    const edges: VertexEdge[] = [
      edge('e0', 'A', 'B', 'ri.lt.flight', { type: 'line' }),
      edge('e1', 'A', 'B', 'ri.lt.flight', { type: 'line' }),
    ];
    const out = mergeEdgesByLinkType(edges, { merge: true });
    expect(out).toHaveLength(1);
    expect(out[0].type).toBe('line');
  });

  it('Given_bidirectionalFlagOnMembers_When_called_Then_preservesBothArrowsFlag', () => {
    const edges: VertexEdge[] = [
      edge('e0', 'A', 'B', 'ri.lt.flight', { bothArrows: true }),
      edge('e1', 'A', 'B', 'ri.lt.flight', { bothArrows: true }),
    ];
    const out = mergeEdgesByLinkType(edges, { merge: true });
    expect(out).toHaveLength(1);
    expect(out[0].bothArrows).toBe(true);
  });

  it('Given_emptyInput_When_called_Then_returnsEmptyArray', () => {
    expect(mergeEdgesByLinkType([], { merge: true })).toEqual([]);
    expect(mergeEdgesByLinkType([], { merge: false })).toEqual([]);
  });
});
