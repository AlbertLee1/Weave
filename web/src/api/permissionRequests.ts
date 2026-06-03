import { request } from './client';

// Status mirrors pkg/permissionrequests const Status* — keep the
// canonical capitalisation from the wire so SDK callers can switch
// without re-uppercasing.
export type PermissionRequestStatus =
  | 'PENDING'
  | 'APPROVED'
  | 'REJECTED'
  | 'CANCELLED';

// PermissionRequest mirrors pkg/permissionrequests.Request. DecidedAt /
// DecidedBy / DecisionNote are absent until the row transitions to a
// terminal status.
export interface PermissionRequest {
  id: string;
  targetRid: string;
  requestedBy: string;
  reason?: string;
  status: PermissionRequestStatus;
  decidedBy?: string;
  decisionNote?: string;
  createdAt: string;
  updatedAt: string;
  decidedAt?: string;
}

export interface ListPermissionRequestsResponse {
  requests: PermissionRequest[];
  total: number;
  limit: number;
  offset: number;
}

export interface ListPermissionRequestsQuery {
  mine?: boolean;
  status?: PermissionRequestStatus;
  targetRid?: string;
  limit?: number;
  offset?: number;
}

function buildListQuery(q: ListPermissionRequestsQuery = {}): string {
  const params = new URLSearchParams();
  if (q.mine) params.set('mine', 'true');
  if (q.status) params.set('status', q.status);
  if (q.targetRid) params.set('targetRid', q.targetRid);
  if (q.limit !== undefined) params.set('limit', String(q.limit));
  if (q.offset !== undefined) params.set('offset', String(q.offset));
  const qs = params.toString();
  return qs ? `?${qs}` : '';
}

export function listPermissionRequests(
  q: ListPermissionRequestsQuery = {},
): Promise<ListPermissionRequestsResponse> {
  return request<ListPermissionRequestsResponse>(
    'GET',
    `/api/v2/permission-requests${buildListQuery(q)}`,
  );
}

export function getPermissionRequest(id: string): Promise<PermissionRequest> {
  return request<PermissionRequest>('GET', `/api/v2/permission-requests/${encodeURIComponent(id)}`);
}

export function createPermissionRequest(
  targetRid: string,
  reason?: string,
): Promise<PermissionRequest> {
  const body: Record<string, string> = { targetRid };
  if (reason) body.reason = reason;
  return request<PermissionRequest>('POST', '/api/v2/permission-requests', body);
}

export function approvePermissionRequest(
  id: string,
  note?: string,
): Promise<PermissionRequest> {
  return request<PermissionRequest>(
    'POST',
    `/api/v2/permission-requests/${encodeURIComponent(id)}/approve`,
    note ? { note } : {},
  );
}

export function rejectPermissionRequest(
  id: string,
  note?: string,
): Promise<PermissionRequest> {
  return request<PermissionRequest>(
    'POST',
    `/api/v2/permission-requests/${encodeURIComponent(id)}/reject`,
    note ? { note } : {},
  );
}

// cancelPermissionRequest withdraws the caller's own PENDING request — the
// backend soft-cancels it to terminal CANCELLED state (only the original
// requester may cancel; DELETE returns 204). See pkg/permissionrequests
// handler Cancel.
export function cancelPermissionRequest(id: string): Promise<void> {
  return request<void>(
    'DELETE',
    `/api/v2/permission-requests/${encodeURIComponent(id)}`,
  );
}
