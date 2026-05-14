export interface RawEvent {
  rid: string;
  objectType: string;
  start: number;
  end?: number;
}

export interface AnnotatedEvent extends RawEvent {
  color: string;
}

export interface AnnotateOptions {
  /** Explicit objectType → color overrides; wins over the palette. */
  colorMap?: Record<string, string>;
  /** Optional seed string for palette assignment. Stable across runs. */
  paletteSeed?: string;
}

// 10-color qualitative palette (Tableau 10). Deterministic assignment via
// stable hash keeps the same ObjectType pinned to the same color across
// renders, regardless of event ordering.
const PALETTE = [
  '#4E79A7',
  '#F28E2B',
  '#E15759',
  '#76B7B2',
  '#59A14F',
  '#EDC948',
  '#B07AA1',
  '#FF9DA7',
  '#9C755F',
  '#BAB0AC',
];

function hash(s: string): number {
  // Simple deterministic 32-bit hash; collisions are tolerated — they
  // just mean two ObjectTypes share a palette slot.
  let h = 0x811c9dc5;
  for (let i = 0; i < s.length; i += 1) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return h >>> 0;
}

export function annotateEventsWithColor(
  events: RawEvent[],
  opts: AnnotateOptions,
): AnnotatedEvent[] {
  const seed = opts.paletteSeed ?? '';
  const overrides = opts.colorMap ?? {};
  return events.map((e) => ({
    ...e,
    color:
      overrides[e.objectType] ??
      PALETTE[hash(seed + e.objectType) % PALETTE.length],
  }));
}

export function filterEventsByCategory(
  events: RawEvent[],
  selectedTypes: Set<string>,
): RawEvent[] {
  return events.filter((e) => selectedTypes.has(e.objectType));
}
