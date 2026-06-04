import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createReaction,
  deleteReaction,
  getReactionSummary,
  getReactionsBatch,
  type ReactionSummary,
} from '../api/reactions';
import { ApiRequestError } from '../api/client';

// useReactionSummary reads the aggregate (emoji → count + mine flag)
// view for a single target. In degraded mode (no PG / route unmounted)
// the call 404s; treat it as "feature unavailable" so the ReactionBar
// hides itself instead of blowing up the panel.
export function useReactionSummary(targetRid: string | null | undefined) {
  return useQuery<ReactionSummary & { available: boolean }>({
    queryKey: ['reactions', 'summary', targetRid ?? '__none__'],
    queryFn: async () => {
      try {
        const resp = await getReactionSummary(targetRid as string);
        return { ...resp, available: true };
      } catch (e) {
        if (
          e instanceof ApiRequestError &&
          (e.statusCode === 404 || e.errorName === 'ReactionsUnavailable')
        ) {
          return {
            targetRid: targetRid ?? '',
            emojis: [],
            available: false,
          };
        }
        throw e;
      }
    },
    enabled: !!targetRid,
  });
}

// useReactionsBatch loads the aggregate view for a whole page of object-list
// rows in ONE POST /api/v2/reactions/batch (instead of N parallel GETs). The
// response is index-aligned with the request, so we re-key it into a
// Map<targetRid, ReactionSummary> for O(1) per-row lookup at render time.
// Degraded mode (route unmounted / no PG → 404 / ReactionsUnavailable) yields
// an empty, "unavailable" map so the list renders without a reactions cell
// instead of erroring. The query key carries the exact RID list so paging to a
// new page refetches; the list is sorted only for cache stability, never for
// the request body (which preserves caller order for index alignment).
export interface ReactionsBatch {
  byRid: Map<string, ReactionSummary>;
  available: boolean;
}

export function useReactionsBatch(targetRids: string[]) {
  const cleaned = targetRids.filter((r) => !!r);
  const cacheKey = [...cleaned].sort().join('|');
  return useQuery<ReactionsBatch>({
    queryKey: ['reactions', 'batch', cacheKey],
    queryFn: async () => {
      try {
        const resp = await getReactionsBatch(cleaned);
        const byRid = new Map<string, ReactionSummary>();
        resp.summaries.forEach((s, i) => {
          // Trust index alignment first; fall back to the Summary's own
          // targetRid so a server that echoes RIDs out of order still keys
          // correctly.
          const rid = cleaned[i] ?? s.targetRid;
          byRid.set(rid, s);
        });
        return { byRid, available: true };
      } catch (e) {
        if (
          e instanceof ApiRequestError &&
          (e.statusCode === 404 || e.errorName === 'ReactionsUnavailable')
        ) {
          return { byRid: new Map(), available: false };
        }
        throw e;
      }
    },
    enabled: cleaned.length > 0,
  });
}

export function useCreateReaction() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ targetRid, emoji }: { targetRid: string; emoji: string }) =>
      createReaction(targetRid, emoji),
    onSuccess: (_row, vars) => {
      qc.invalidateQueries({ queryKey: ['reactions', 'summary', vars.targetRid] });
    },
  });
}

export function useDeleteReaction() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ targetRid, emoji }: { targetRid: string; emoji: string }) =>
      deleteReaction(targetRid, emoji),
    onSuccess: (_void, vars) => {
      qc.invalidateQueries({ queryKey: ['reactions', 'summary', vars.targetRid] });
    },
  });
}
