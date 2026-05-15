import { describe, it, expect } from 'vitest';
import { diffGraphPayloads, type GraphPayloadV2 } from './graphDiff';

const v3: GraphPayloadV2 = {
  name: 'g',
  nodes: [
    { id: 'a', objectType: 'Airport', style: { fill: 'red' } },
    { id: 'b', objectType: 'Airport', style: { fill: 'red' } },
    { id: 'c', objectType: 'Airport', style: { fill: 'red' } },
  ],
  edges: [
    { src: 'a', dst: 'b' },
    { src: 'b', dst: 'c' },
  ],
};

const v5: GraphPayloadV2 = {
  name: 'g',
  nodes: [
    { id: 'a', objectType: 'Airport', style: { fill: 'red' } },
    { id: 'b', objectType: 'Airport', style: { fill: 'blue' } }, // style changed
    { id: 'd', objectType: 'Airport', style: { fill: 'green' } }, // added; c removed
  ],
  edges: [
    { src: 'a', dst: 'b' },
    { src: 'a', dst: 'd' }, // added; b->c removed
  ],
};

describe('VTX-088 diffGraphPayloads', () => {
  it('given_NodesAddedAndRemoved_when_Diff_then_TwoLists', () => {
    const d = diffGraphPayloads(v3, v5);
    expect(d.nodesAdded.map((n) => n.id)).toEqual(['d']);
    expect(d.nodesRemoved.map((n) => n.id)).toEqual(['c']);
  });

  it('given_NodeStyleChanged_when_Diff_then_NodesChanged', () => {
    const d = diffGraphPayloads(v3, v5);
    expect(d.nodesChanged).toHaveLength(1);
    expect(d.nodesChanged[0].id).toBe('b');
    expect(d.nodesChanged[0].before.style).toEqual({ fill: 'red' });
    expect(d.nodesChanged[0].after.style).toEqual({ fill: 'blue' });
  });

  it('given_EdgesAddedAndRemoved_when_Diff_then_TwoLists', () => {
    const d = diffGraphPayloads(v3, v5);
    expect(d.edgesAdded).toEqual([{ src: 'a', dst: 'd' }]);
    expect(d.edgesRemoved).toEqual([{ src: 'b', dst: 'c' }]);
  });

  it('given_IdenticalGraphs_when_Diff_then_AllEmpty', () => {
    const d = diffGraphPayloads(v3, v3);
    expect(d.nodesAdded).toEqual([]);
    expect(d.nodesRemoved).toEqual([]);
    expect(d.nodesChanged).toEqual([]);
    expect(d.edgesAdded).toEqual([]);
    expect(d.edgesRemoved).toEqual([]);
  });

  it('given_OneSideEmpty_when_Diff_then_AllAddedRemoved', () => {
    const empty: GraphPayloadV2 = { name: 'g', nodes: [], edges: [] };
    const d = diffGraphPayloads(empty, v5);
    expect(d.nodesAdded).toHaveLength(3);
    expect(d.nodesRemoved).toEqual([]);
    expect(d.edgesAdded).toHaveLength(2);
  });
});
