import { request } from './client';

// funnelDLQ.ts mirrors cmd/server/admin_funnel_dlq.go — the operator-facing
// surface over the Funnel EditBatch dead-letter queue (OBJECT_EDITS_DLQ
// JetStream stream). Three routes back this module:
//   GET  /api/admin/funnel/dlq?limit=N        — list pending entries + depth
//   POST /api/admin/funnel/dlq/{id}/replay    — republish + delete
//   POST /api/admin/funnel/dlq/{id}/discard   — delete without replay
//
// When the DLQ is not configured/enabled (degraded bootstrap without NATS),
// the backend returns HTTP 503 with errorName "FunnelDLQNotConfigured". The
// caller (FunnelDLQAdminPage) is expected to special-case statusCode === 503
// into a friendly "not enabled" state rather than an error toast.

// DLQMessage is the failure envelope captured for each dead-lettered
// EditBatch. originalSubject + reason + maxDeliveries drive the operator's
// triage; the stream/consumer sequences are diagnostic provenance.
export interface DLQMessage {
  originalSubject: string;
  reason: string;
  maxDeliveries: number;
  streamSequence: number;
  consumerSequence: number;
}

// DLQEntry is a single pending row in the funnel DLQ. `id` is the opaque
// handle the replay/discard routes accept; `subject` is the DLQ subject the
// entry currently sits on (distinct from message.originalSubject, the
// subject it will be republished to on replay).
export interface DLQEntry {
  id: string;
  subject: string;
  message: DLQMessage;
}

// ListFunnelDLQResponse mirrors adminFunnelDLQListResponse. `size` is the
// authoritative DLQ depth and may exceed entries.length when `limit` caps
// the returned page.
export interface ListFunnelDLQResponse {
  entries: DLQEntry[];
  size: number;
}

export interface ReplayFunnelDLQResponse {
  id: string;
  originalSubject: string;
}

export interface DiscardFunnelDLQResponse {
  id: string;
  dropped: boolean;
}

// listFunnelDLQ fetches the pending DLQ page. `limit` is optional; the
// backend defaults to 100 and hard-caps at 1000 when omitted/exceeded.
export function listFunnelDLQ(limit?: number): Promise<ListFunnelDLQResponse> {
  const qs = limit !== undefined ? `?limit=${encodeURIComponent(String(limit))}` : '';
  return request<ListFunnelDLQResponse>('GET', `/api/admin/funnel/dlq${qs}`);
}

// replayFunnelDLQ republishes a single entry to its original subject and
// drops it from the DLQ. Resolves with the original subject so the UI can
// confirm where the batch was re-delivered.
export function replayFunnelDLQ(id: string): Promise<ReplayFunnelDLQResponse> {
  return request<ReplayFunnelDLQResponse>(
    'POST',
    `/api/admin/funnel/dlq/${encodeURIComponent(id)}/replay`,
    {},
  );
}

// discardFunnelDLQ drops a single entry from the DLQ without republishing.
export function discardFunnelDLQ(id: string): Promise<DiscardFunnelDLQResponse> {
  return request<DiscardFunnelDLQResponse>(
    'POST',
    `/api/admin/funnel/dlq/${encodeURIComponent(id)}/discard`,
    {},
  );
}
