import { aggregateAtTime, type AggregationMethod, type TimePoint } from './aggregateAtTime';

export interface TimeWindow {
  id: string;
  from: number;
  to: number;
}

export function addCompareWindow(
  windows: TimeWindow[],
  next: TimeWindow,
): TimeWindow[] {
  if (windows.some((w) => w.id === next.id)) {
    throw new Error(`compareWindows: duplicate window id ${next.id}`);
  }
  return [...windows, next];
}

export function removeCompareWindow(
  windows: TimeWindow[],
  id: string,
): TimeWindow[] {
  return windows.filter((w) => w.id !== id);
}

export function aggregatePerWindow(
  series: TimePoint[],
  windows: TimeWindow[],
  agg: AggregationMethod,
): Record<string, number | null> {
  const out: Record<string, number | null> = {};
  for (const w of windows) {
    out[w.id] = aggregateAtTime(series, {
      selectedTime: w.to,
      windowMs: w.to - w.from,
      agg,
    });
  }
  return out;
}
