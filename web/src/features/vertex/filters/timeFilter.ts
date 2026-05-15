export interface TimeFilter {
  property: string;
  from: number;
  to: number;
}

export interface TimedNode {
  id: string;
  timestamps: Record<string, number | undefined>;
}

export interface TimeFilterResult {
  passing: Set<string>;
  dimmed: Set<string>;
}

export function applyTimeFilter(
  nodes: TimedNode[],
  filter: TimeFilter | null,
): TimeFilterResult {
  const passing = new Set<string>();
  const dimmed = new Set<string>();
  if (filter === null) {
    for (const n of nodes) passing.add(n.id);
    return { passing, dimmed };
  }
  for (const n of nodes) {
    const ts = n.timestamps[filter.property];
    const inRange =
      typeof ts === 'number' &&
      Number.isFinite(ts) &&
      ts >= filter.from &&
      ts <= filter.to;
    (inRange ? passing : dimmed).add(n.id);
  }
  return { passing, dimmed };
}
