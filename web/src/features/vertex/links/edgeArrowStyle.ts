// VTX-010: pick edge arrow style from LinkType.typeClasses tags.
//
// Each LinkType may carry one or more Vertex graph tags that the graph
// renderer reads to decide how to draw the edge arrow. The mapping is:
//   vertex:link_primary_direction → "primary"  (source → target arrow)
//   vertex:link_undirectional     → "none"     (line, no arrow)
//   vertex:link_bidirectional     → "both"     (source ↔ target arrows)
//
// Default when no tag is set is "primary" — matches the historic Weave
// behaviour where every edge had a single forward arrow.

export const VERTEX_LINK_TYPE_CLASSES = [
  'vertex:link_primary_direction',
  'vertex:link_undirectional',
  'vertex:link_bidirectional',
] as const;

export type VertexLinkTypeClass = (typeof VERTEX_LINK_TYPE_CLASSES)[number];

export type EdgeArrowStyle = 'primary' | 'none' | 'both';

export function pickEdgeArrowStyle(
  tags: readonly string[] | undefined,
): EdgeArrowStyle {
  if (!tags || tags.length === 0) return 'primary';
  // bidirectional is the most permissive — when it appears alongside a
  // primary tag we still want both arrows.
  if (tags.includes('vertex:link_bidirectional')) return 'both';
  if (tags.includes('vertex:link_undirectional')) return 'none';
  if (tags.includes('vertex:link_primary_direction')) return 'primary';
  return 'primary';
}
