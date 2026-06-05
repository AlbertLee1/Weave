import { request } from './client';

// SideEffectDLQRow mirrors pkg/oms/models.go::SideEffectDLQRow — one entry in
// the action-side-effect dead-letter queue. The executor inserts a row when a
// webhook (or other side-effect) exhausts its retry budget on a transient
// failure. Operators inspect pending rows via the admin API and either replay
// (re-dispatch through the same retry loop) or abandon (give up, flip to
// terminal 'abandoned').
//
// `id` is an int64 on the wire — path params are formatted as decimal digits.
// `effectConfig` and `outcome` are opaque JSON blobs surfaced for diagnostics.
// `replayStatus` is one of the three taxonomy strings below; only 'pending'
// rows are replayable / abandonable.
export interface SideEffectDLQRow {
  id: number;
  actionLogId: number;
  effectIndex: number;
  effectType: string;
  effectConfig?: unknown;
  outcome: unknown;
  createdAt: string;
  replayStatus: SideEffectDLQReplayStatus;
  replayedAt?: string;
  replayCount: number;
}

export type SideEffectDLQReplayStatus = 'pending' | 'replayed' | 'abandoned';

interface ListSideEffectDLQResponse {
  entries: SideEffectDLQRow[];
}

// listSideEffectDLQ maps GET /api/admin/side-effect-dlq → SideEffectDLQRow[].
// The server always emits an `entries` array (possibly empty); we coalesce to
// [] defensively so callers never see undefined.
export async function listSideEffectDLQ(): Promise<SideEffectDLQRow[]> {
  const resp = await request<ListSideEffectDLQResponse>(
    'GET',
    '/api/admin/side-effect-dlq',
  );
  return resp.entries ?? [];
}

// AbandonSideEffectDLQResponse is the wire shape for POST .../{id}/abandon.
export interface AbandonSideEffectDLQResponse {
  id: number;
  abandoned: boolean;
  status: string;
}

// abandonSideEffectDLQ maps POST /api/admin/side-effect-dlq/{id}/abandon. Only
// valid from 'pending'; 'replayed' rows return 409 (can't mask a successful
// dispatch). `id` is serialized as a decimal integer path segment.
export function abandonSideEffectDLQ(
  id: number,
): Promise<AbandonSideEffectDLQResponse> {
  return request<AbandonSideEffectDLQResponse>(
    'POST',
    `/api/admin/side-effect-dlq/${id}/abandon`,
    {},
  );
}

// ReplaySideEffectDLQResponse is the wire shape for POST .../{id}/replay. The
// endpoint returns 200 even when the webhook itself stays broken — `replayed`
// reflects whether this attempt succeeded, and `outcome` carries the
// per-attempt SideEffectOutcome for diagnostics.
export interface ReplaySideEffectDLQResponse {
  id: number;
  replayed: boolean;
  status: string;
  replayCount: number;
  outcome: unknown;
}

// replaySideEffectDLQ maps POST /api/admin/side-effect-dlq/{id}/replay. Only
// valid from 'pending'; replayed/abandoned rows return 409.
export function replaySideEffectDLQ(
  id: number,
): Promise<ReplaySideEffectDLQResponse> {
  return request<ReplaySideEffectDLQResponse>(
    'POST',
    `/api/admin/side-effect-dlq/${id}/replay`,
    {},
  );
}
