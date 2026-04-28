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
