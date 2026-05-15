import { describe, it, expect } from 'vitest';
import {
  pickEdgeArrowStyle,
  VERTEX_LINK_TYPE_CLASSES,
  type VertexLinkTypeClass,
  type EdgeArrowStyle,
} from './edgeArrowStyle';

describe('VTX-010 pickEdgeArrowStyle', () => {
  it('given_PrimaryDirectionTag_then_primaryArrow', () => {
    const tags: VertexLinkTypeClass[] = ['vertex:link_primary_direction'];
    const got: EdgeArrowStyle = pickEdgeArrowStyle(tags);
    expect(got).toBe('primary');
  });

  it('given_UndirectionalTag_then_noneArrow', () => {
    const got = pickEdgeArrowStyle(['vertex:link_undirectional']);
    expect(got).toBe('none');
  });

  it('given_BidirectionalTag_then_bothArrow', () => {
    const got = pickEdgeArrowStyle(['vertex:link_bidirectional']);
    expect(got).toBe('both');
  });

  it('given_NoTags_then_defaultsToPrimary', () => {
    expect(pickEdgeArrowStyle([])).toBe('primary');
    expect(pickEdgeArrowStyle(undefined)).toBe('primary');
  });

  it('given_UnknownTag_then_defaultsToPrimary', () => {
    expect(pickEdgeArrowStyle(['vertex:bogus' as VertexLinkTypeClass])).toBe(
      'primary',
    );
  });

  it('given_BidirectionalAndPrimary_then_bidirectionalWins', () => {
    // Bidirectional is the most permissive label, so it wins when stacked
    // against a primary tag — both arrows are drawn either way.
    const got = pickEdgeArrowStyle([
      'vertex:link_primary_direction',
      'vertex:link_bidirectional',
    ]);
    expect(got).toBe('both');
  });

  it('exports_KnownTagList_for_admin_UI', () => {
    expect(VERTEX_LINK_TYPE_CLASSES).toEqual([
      'vertex:link_primary_direction',
      'vertex:link_undirectional',
      'vertex:link_bidirectional',
    ]);
  });
});
