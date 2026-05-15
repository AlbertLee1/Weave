import { describe, it, expect } from 'vitest';
import { duplicateGraphPayload, type GraphPayload } from './duplicateGraph';

const payload: GraphPayload = {
  name: 'JFK Ops',
  nodes: [{ id: 'a' }, { id: 'b' }],
  edges: [{ src: 'a', dst: 'b' }],
  styling: { theme: 'dark' },
};

describe('VTX-084 duplicateGraphPayload', () => {
  it('given_Name_when_Duplicate_then_NameSuffixedWithCopy', () => {
    const dup = duplicateGraphPayload(payload);
    expect(dup.name).toBe('JFK Ops (Copy)');
  });

  it('given_NameAlreadyEndsWithCopy_when_Duplicate_then_AppendsAgain', () => {
    const dup = duplicateGraphPayload({ ...payload, name: 'JFK Ops (Copy)' });
    expect(dup.name).toBe('JFK Ops (Copy) (Copy)');
  });

  it('given_Duplicate_when_Edit_then_OriginalNotMutated', () => {
    const dup = duplicateGraphPayload(payload);
    dup.nodes.push({ id: 'c' });
    expect(payload.nodes).toHaveLength(2);
  });

  it('given_NestedStyling_when_Duplicate_then_DeepCloned', () => {
    const dup = duplicateGraphPayload(payload);
    (dup.styling as Record<string, string>).theme = 'light';
    expect(payload.styling).toEqual({ theme: 'dark' });
  });

  it('given_EmptyName_when_Duplicate_then_NameIsCopy', () => {
    const dup = duplicateGraphPayload({ ...payload, name: '' });
    expect(dup.name).toBe('(Copy)');
  });
});
