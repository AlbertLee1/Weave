export interface RangeFilter {
  min: number;
  max: number;
}

export type HistogramFilters = Record<string, RangeFilter | undefined>;

export interface FilterableNode {
  id: string;
  properties: Record<string, unknown>;
}

export interface FilterResult {
  passing: Set<string>;
  dimmed: Set<string>;
}

export function applyHistogramFilters(
  nodes: FilterableNode[],
  filters: HistogramFilters,
): FilterResult {
  const passing = new Set<string>();
  const dimmed = new Set<string>();
  const filterEntries = Object.entries(filters).filter(
    (e): e is [string, RangeFilter] => e[1] !== undefined,
  );

  for (const node of nodes) {
    let pass = true;
    for (const [prop, range] of filterEntries) {
      const v = node.properties[prop];
      if (typeof v !== 'number' || !Number.isFinite(v) || v < range.min || v > range.max) {
        pass = false;
        break;
      }
    }
    (pass ? passing : dimmed).add(node.id);
  }
  return { passing, dimmed };
}

export function setRangeFilter(
  filters: HistogramFilters,
  property: string,
  range: RangeFilter,
): HistogramFilters {
  return { ...filters, [property]: range };
}

export function clearFilters(_filters: HistogramFilters): HistogramFilters {
  return {};
}
