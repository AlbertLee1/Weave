import { useQuery } from '@tanstack/react-query';
import {
  getActionHistoryEntry,
  listActionHistory,
  type ListActionHistoryParams,
} from '../api/actionHistory';
import { ApiRequestError } from '../api/client';

export function useActionHistory(
  ontologyApiName: string,
  params: ListActionHistoryParams = {},
) {
  return useQuery({
    queryKey: ['actionHistory', ontologyApiName, params],
    queryFn: async () => {
      try {
        return await listActionHistory(ontologyApiName, params);
      } catch (err) {
        // Degraded mode (no PG, no executor) silently shapes as an empty
        // page rather than failing the panel — same shape as
        // useSavedSearches' 404 / Unavailable short-circuit (US-311).
        if (
          err instanceof ApiRequestError &&
          (err.statusCode === 404 || err.statusCode === 405)
        ) {
          return { data: [] };
        }
        throw err;
      }
    },
    enabled: !!ontologyApiName,
  });
}

export function useActionHistoryEntry(
  ontologyApiName: string,
  logId: number | null,
) {
  return useQuery({
    queryKey: ['actionHistoryEntry', ontologyApiName, logId],
    queryFn: () => getActionHistoryEntry(ontologyApiName, logId as number),
    enabled: !!ontologyApiName && logId !== null,
  });
}
