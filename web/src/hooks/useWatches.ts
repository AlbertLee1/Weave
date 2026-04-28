import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createWatch,
  deleteWatch,
  getWatchStatus,
  listWatches,
  type Watch,
  type WatchStatus,
} from '../api/watches';
import { ApiRequestError } from '../api/client';

// useWatchStatus reads the boolean toggle state for a single target. In
// degraded mode (no PG / route unmounted) the call 404s; treat it as
// "watching=false" so the WatchButton hides itself instead of blowing
// up the ObjectDetail panel.
export function useWatchStatus(targetRid: string | null | undefined) {
  return useQuery<WatchStatus & { available: boolean }>({
    queryKey: ['watches', 'status', targetRid ?? '__none__'],
    queryFn: async () => {
      try {
        const resp = await getWatchStatus(targetRid as string);
        return { ...resp, available: true };
      } catch (e) {
        if (
          e instanceof ApiRequestError &&
          (e.statusCode === 404 || e.errorName === 'WatchesUnavailable')
        ) {
          return {
            targetRid: targetRid ?? '',
            watching: false,
            available: false,
          };
        }
        throw e;
      }
    },
    enabled: !!targetRid,
  });
}

// useWatches returns the caller's full watchlist. Same degraded-mode
// short-circuit as useWatchStatus so the future Watch sidebar can render
// an empty list rather than failing.
export function useWatches() {
  return useQuery<Watch[]>({
    queryKey: ['watches', 'list'],
    queryFn: async () => {
      try {
        const resp = await listWatches();
        return resp.watches ?? [];
      } catch (e) {
        if (
          e instanceof ApiRequestError &&
          (e.statusCode === 404 || e.errorName === 'WatchesUnavailable')
        ) {
          return [];
        }
        throw e;
      }
    },
  });
}

export function useCreateWatch() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (targetRid: string) => createWatch(targetRid),
    onSuccess: (row) => {
      qc.invalidateQueries({ queryKey: ['watches'] });
      qc.setQueryData(['watches', 'status', row.targetRid], {
        targetRid: row.targetRid,
        watching: true,
        available: true,
      });
    },
  });
}

export function useDeleteWatch() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (targetRid: string) => deleteWatch(targetRid),
    onSuccess: (_void, targetRid) => {
      qc.invalidateQueries({ queryKey: ['watches'] });
      qc.setQueryData(['watches', 'status', targetRid], {
        targetRid,
        watching: false,
        available: true,
      });
    },
  });
}
