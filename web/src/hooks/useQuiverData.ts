import { useQuery } from '@tanstack/react-query';
import { getQuiverData, type QuiverDataResponse } from '../api/quiver';

// US-482: windowed + bucketed fetch for a saved Quiver dashboard. Where
// useQuiverSparklines returns the raw points, this hook drives the
// time-range + step picker: the backend clips each series to [from, to]
// and reduces every `step` bucket to its mean, so the chart shows a
// fixed-resolution window instead of the full unbounded series.

export interface UseQuiverDataParams {
  rid: string | undefined;
  // from / to are RFC3339 (or unix-millis) strings; empty/undefined means
  // "all time" on that side. step is the required bucket width (Go
  // duration string). The query stays disabled until both rid and step
  // are present.
  from?: string;
  to?: string;
  step: string | undefined;
  enabled?: boolean;
}

const QUIVER_DATA_KEY = ['quiver', 'data'] as const;

export function quiverDataQueryKey(
  rid: string,
  from: string | undefined,
  to: string | undefined,
  step: string,
) {
  return [...QUIVER_DATA_KEY, rid, from ?? '', to ?? '', step] as const;
}

export function useQuiverData(params: UseQuiverDataParams) {
  const { rid, from, to, step, enabled } = params;
  const keyOk = !!rid && !!step && step.trim().length > 0;
  return useQuery<QuiverDataResponse>({
    queryKey: keyOk
      ? quiverDataQueryKey(rid!, from, to, step!)
      : [...QUIVER_DATA_KEY, '__none__'],
    queryFn: () =>
      getQuiverData(rid!, {
        step: step!,
        ...(from ? { from } : {}),
        ...(to ? { to } : {}),
      }),
    enabled: keyOk && enabled !== false,
    retry: false,
  });
}
