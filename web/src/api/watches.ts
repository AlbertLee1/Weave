import { request } from './client';

// Watch mirrors pkg/watches.Watch — one row per (userId, targetRid)
// pair. Created idempotently by POST /api/v2/watches; deleted via the
// query-string keyed DELETE.
export interface Watch {
  id: string;
  userId: string;
  targetRid: string;
  createdAt: string;
}

export interface ListWatchesResponse {
  watches: Watch[];
}

export interface WatchStatus {
  targetRid: string;
  watching: boolean;
}

export function listWatches(): Promise<ListWatchesResponse> {
  return request<ListWatchesResponse>('GET', '/api/v2/watches');
}

export function getWatchStatus(targetRid: string): Promise<WatchStatus> {
  const qs = new URLSearchParams({ targetRid });
  return request<WatchStatus>('GET', `/api/v2/watches/status?${qs.toString()}`);
}

export function createWatch(targetRid: string): Promise<Watch> {
  return request<Watch>('POST', '/api/v2/watches', { targetRid });
}

export function deleteWatch(targetRid: string): Promise<void> {
  const qs = new URLSearchParams({ targetRid });
  return request<void>('DELETE', `/api/v2/watches?${qs.toString()}`);
}
