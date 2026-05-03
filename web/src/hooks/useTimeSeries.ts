import { useQuery } from '@tanstack/react-query';
import { streamTimeSeriesPoints, type TimeSeriesPoint } from '../api/timeseries';

export type TimeSeriesRange = '1h' | '24h' | '7d' | '30d';

const RANGE_MS: Record<TimeSeriesRange, number> = {
  '1h': 60 * 60 * 1000,
  '24h': 24 * 60 * 60 * 1000,
  '7d': 7 * 24 * 60 * 60 * 1000,
  '30d': 30 * 24 * 60 * 60 * 1000,
};

export function filterPointsByRange(
  points: TimeSeriesPoint[],
  range: TimeSeriesRange,
  now: number = Date.now(),
): TimeSeriesPoint[] {
  const minMs = now - RANGE_MS[range];
  return points.filter((p) => {
    const t = Date.parse(p.time);
    return Number.isFinite(t) && t >= minMs;
  });
}

export interface UseTimeSeriesPointsParams {
  ontologyApiName: string;
  objectType: string;
  primaryKey: string;
  property: string;
  // US-404: optional explicit branch. When set the fetch overrides the
  // store's active branch and the queryKey is keyed on it so multi-branch
  // overlays cache independently.
  branch?: string;
  enabled?: boolean;
}

export function useTimeSeriesPoints(params: UseTimeSeriesPointsParams) {
  const { ontologyApiName, objectType, primaryKey, property, branch, enabled } =
    params;
  const keyOk = !!(ontologyApiName && objectType && primaryKey && property);
  const branchKey = branch && branch.trim().length > 0 ? branch.trim() : '';
  return useQuery<TimeSeriesPoint[]>({
    queryKey: [
      'timeseries',
      ontologyApiName,
      objectType,
      primaryKey,
      property,
      branchKey,
    ],
    queryFn: () =>
      streamTimeSeriesPoints({
        ontologyApiName,
        objectType,
        primaryKey,
        property,
        ...(branchKey ? { branch: branchKey } : {}),
      }),
    enabled: keyOk && enabled !== false,
  });
}
