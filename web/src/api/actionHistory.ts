import { request } from './client';

// ActionHistoryEntry mirrors pkg/oms.ActionLog. Parameters / edits / prevEdits
// are opaque JSON the UI renders as-is — same shape Approvals.parameters uses.
export interface ActionHistoryEntry {
  id: number;
  actionTypeRid: string;
  userId: string;
  parameters?: unknown;
  edits?: unknown;
  prevEdits?: unknown;
  status: string;
  errorMessage?: string;
  createdAt: string;
}

export interface ActionHistoryListResponse {
  data: ActionHistoryEntry[];
  total?: number;
  limit?: number;
  offset?: number;
  nextOffset?: number;
}

export interface ListActionHistoryParams {
  actionType?: string;
  status?: 'SUCCESS' | 'FAILED';
  userId?: string;
  since?: string;
  until?: string;
  limit?: number;
  offset?: number;
}

export function listActionHistory(
  ontologyApiName: string,
  params: ListActionHistoryParams = {},
): Promise<ActionHistoryListResponse> {
  const query = new URLSearchParams();
  if (params.actionType) query.set('actionType', params.actionType);
  if (params.status) query.set('status', params.status);
  if (params.userId) query.set('userId', params.userId);
  if (params.since) query.set('since', params.since);
  if (params.until) query.set('until', params.until);
  if (params.limit !== undefined) query.set('limit', String(params.limit));
  if (params.offset !== undefined) query.set('offset', String(params.offset));
  const qs = query.toString();
  return request<ActionHistoryListResponse>(
    'GET',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/actions/history${qs ? `?${qs}` : ''}`,
  );
}

export function getActionHistoryEntry(
  ontologyApiName: string,
  logId: number,
): Promise<ActionHistoryEntry> {
  return request<ActionHistoryEntry>(
    'GET',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/actions/history/${logId}`,
  );
}
