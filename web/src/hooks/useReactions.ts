import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createReaction,
  deleteReaction,
  getReactionSummary,
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
