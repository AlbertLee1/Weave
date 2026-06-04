import { request } from './client';

// Reaction mirrors pkg/reactions.Reaction — one row per
// (userId, targetRid, emoji) triple. Created idempotently by POST
// /api/v2/reactions; deleted via the query-string keyed DELETE.
export interface Reaction {
  id: string;
  userId: string;
  targetRid: string;
  emoji: string;
  createdAt: string;
}

export interface EmojiCount {
  emoji: string;
  count: number;
  mine: boolean;
}

export interface ReactionSummary {
  targetRid: string;
  emojis: EmojiCount[];
}

export function getReactionSummary(targetRid: string): Promise<ReactionSummary> {
  const qs = new URLSearchParams({ targetRid });
  return request<ReactionSummary>('GET', `/api/v2/reactions?${qs.toString()}`);
}

// ReactionBatchResponse mirrors pkg/reactions.batchResponse — summaries is
// index-aligned with the request's targetRids so summaries[i] belongs to
// targetRids[i] (no re-keying needed).
export interface ReactionBatchResponse {
  summaries: ReactionSummary[];
}

// getReactionsBatch collapses the N per-row GET /api/v2/reactions calls a
// rendered object list would otherwise make into ONE POST
// /api/v2/reactions/batch (on PG: WHERE target_rid = ANY($1)). The response
// is index-aligned with targetRids. An empty input short-circuits without a
// round-trip — the backend already returns {summaries:[]} for it, but there
// is no reason to ask. Every Summary's emojis slice is non-nil on the wire.
export function getReactionsBatch(
  targetRids: string[],
): Promise<ReactionBatchResponse> {
  if (targetRids.length === 0) {
    return Promise.resolve({ summaries: [] });
  }
  return request<ReactionBatchResponse>('POST', '/api/v2/reactions/batch', {
    targetRids,
  });
}

export function createReaction(
  targetRid: string,
  emoji: string,
): Promise<Reaction> {
  return request<Reaction>('POST', '/api/v2/reactions', { targetRid, emoji });
}

export function deleteReaction(targetRid: string, emoji: string): Promise<void> {
  const qs = new URLSearchParams({ targetRid, emoji });
  return request<void>('DELETE', `/api/v2/reactions?${qs.toString()}`);
}
