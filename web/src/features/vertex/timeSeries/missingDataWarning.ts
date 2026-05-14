import type { TimePoint } from './aggregateAtTime';

export interface MissingDataParams {
  selectedTime: number;
  missingDataWarningHours: number;
}

export interface MissingDataResult {
  warn: boolean;
  lastObservedMs: number | null;
  gapHours: number | null;
}

const HOUR_MS = 3600_000;

export function computeMissingDataWarning(
  series: TimePoint[],
  params: MissingDataParams,
): MissingDataResult {
  let lastObservedMs: number | null = null;
  for (const p of series) {
    if (p.t <= params.selectedTime && (lastObservedMs === null || p.t > lastObservedMs)) {
      lastObservedMs = p.t;
    }
  }
  if (lastObservedMs === null) {
    return { warn: true, lastObservedMs: null, gapHours: null };
  }
  const gapMs = params.selectedTime - lastObservedMs;
  const gapHours = Math.round((gapMs / HOUR_MS) * 100) / 100;
  return {
    warn: gapHours > params.missingDataWarningHours,
    lastObservedMs,
    gapHours,
  };
}
