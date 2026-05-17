import { useQuery } from '@tanstack/react-query';
import {
  batchQuiverSparklines,
  type QuiverSparklinesResponse,
} from '../api/quiver';

// US-483: single batch fetch for every series in a saved Quiver
// dashboard. Replaces the per-series useTimeSeriesPoints fan-out the
// QuiverWorkbenchView used to do on initial load — one HTTP round-trip
// returns all series' points.

export interface UseQuiverSparklinesParams {
  rid: string | undefined;
  // seriesIds is an optional subset filter. Empty / undefined means
  // "every series in the saved dashboard config" (the default for the
  // initial dashboard render).
  seriesIds?: string[];
  enabled?: boolean;
}

const QUIVER_SPARKLINES_KEY = ['quiver', 'sparklines'] as const;

export function quiverSparklinesQueryKey(
  rid: string,
  seriesIds: string[] | undefined,
) {
  // Sort the series id subset so two calls that request the same logical
  // set (regardless of array order) share the cache entry.
  const sortedIds = (seriesIds ?? []).slice().sort();
  return [...QUIVER_SPARKLINES_KEY, rid, sortedIds] as const;
}

export function useQuiverSparklines(params: UseQuiverSparklinesParams) {
  const { rid, seriesIds, enabled } = params;
  return useQuery<QuiverSparklinesResponse>({
    queryKey: rid
      ? quiverSparklinesQueryKey(rid, seriesIds)
      : [...QUIVER_SPARKLINES_KEY, '__none__'],
    queryFn: () =>
      batchQuiverSparklines(rid!, seriesIds ? { seriesIds } : {}),
    enabled: !!rid && enabled !== false,
    retry: false,
  });
}
