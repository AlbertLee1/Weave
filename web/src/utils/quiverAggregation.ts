import type { TimeSeriesPoint } from '../api/timeseries';

export interface RangeAggregate {
  count: number;
  sum: number;
  avg: number;
  min: number;
  max: number;
}

export const EMPTY_AGGREGATE: RangeAggregate = {
  count: 0,
  sum: 0,
  avg: 0,
  min: 0,
  max: 0,
};

function toNumber(v: unknown): number | null {
  if (typeof v === 'number' && Number.isFinite(v)) return v;
  if (typeof v === 'string') {
    const n = Number(v);
    return Number.isFinite(n) ? n : null;
  }
  return null;
}

// US-401: aggregate the numeric values of a single series within a half-open
// time window [startMs, endMs). Returns EMPTY_AGGREGATE when no point falls
// in range so callers can render a stable shape without nullguards.
export function aggregateRange(
  points: TimeSeriesPoint[],
  startMs: number,
  endMs: number,
): RangeAggregate {
  if (!Number.isFinite(startMs) || !Number.isFinite(endMs)) return EMPTY_AGGREGATE;
  const lo = Math.min(startMs, endMs);
  const hi = Math.max(startMs, endMs);
  let count = 0;
  let sum = 0;
  let min = Number.POSITIVE_INFINITY;
  let max = Number.NEGATIVE_INFINITY;
  for (const p of points) {
    const t = Date.parse(p.time);
    if (!Number.isFinite(t)) continue;
    if (t < lo || t >= hi) continue;
    const v = toNumber(p.value);
    if (v === null) continue;
    count += 1;
    sum += v;
    if (v < min) min = v;
    if (v > max) max = v;
  }
  if (count === 0) return EMPTY_AGGREGATE;
  return { count, sum, avg: sum / count, min, max };
}

// Build a uPlot-compatible AlignedData array: a shared sorted timestamp axis
// (in seconds, uPlot's native unit) and one parallel y-array per series with
// `null` for timestamps that series does not have. Non-numeric values (which
// memory/PG backends accept but uPlot cannot render) collapse to null too.
export function buildAlignedData(
  series: { id: string; points: TimeSeriesPoint[] }[],
): { xs: number[]; ys: (number | null)[][] } {
  const tsSet = new Set<number>();
  const perSeriesMap: Map<string, Map<number, number>> = new Map();
  for (const s of series) {
    const m = new Map<number, number>();
    for (const p of s.points) {
      const t = Date.parse(p.time);
      if (!Number.isFinite(t)) continue;
      const v = toNumber(p.value);
      if (v === null) continue;
      const sec = Math.floor(t / 1000);
      tsSet.add(sec);
      m.set(sec, v);
    }
    perSeriesMap.set(s.id, m);
  }
  const xs = Array.from(tsSet).sort((a, b) => a - b);
  const ys = series.map((s) => {
    const m = perSeriesMap.get(s.id) ?? new Map<number, number>();
    return xs.map((x) => (m.has(x) ? (m.get(x) as number) : null));
  });
  return { xs, ys };
}

// Distinct-enough palette for dark theme overlays. Cycled when more series
// are added than colors — Quiver is a lightweight workbench, not a publication
// tool, so a 8-way cycle is plenty.
export const QUIVER_PALETTE = [
  '#22d3ee', // cyan
  '#f59e0b', // amber
  '#a78bfa', // violet
  '#34d399', // emerald
  '#f472b6', // pink
  '#facc15', // yellow
  '#60a5fa', // blue
  '#fb7185', // rose
];

export function pickColor(index: number): string {
  return QUIVER_PALETTE[index % QUIVER_PALETTE.length];
}
