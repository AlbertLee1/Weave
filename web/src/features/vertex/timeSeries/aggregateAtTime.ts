export interface TimePoint {
  t: number;
  v: number;
}

export type AggregationMethod = 'avg' | 'sum' | 'max' | 'min' | 'last' | 'count';

export interface AggregateParams {
  selectedTime: number;
  windowMs: number;
  agg: AggregationMethod;
}

export function aggregateAtTime(
  series: TimePoint[],
  params: AggregateParams,
): number | null {
  const start = params.selectedTime - params.windowMs;
  const end = params.selectedTime;
  const inWindow = series.filter((p) => p.t >= start && p.t <= end);
  if (inWindow.length === 0) return null;
  switch (params.agg) {
    case 'count':
      return inWindow.length;
    case 'sum':
      return inWindow.reduce((acc, p) => acc + p.v, 0);
    case 'avg':
      return inWindow.reduce((acc, p) => acc + p.v, 0) / inWindow.length;
    case 'max':
      return Math.max(...inWindow.map((p) => p.v));
    case 'min':
      return Math.min(...inWindow.map((p) => p.v));
    case 'last':
      return inWindow.reduce((latest, p) => (p.t > latest.t ? p : latest), inWindow[0]).v;
  }
}
