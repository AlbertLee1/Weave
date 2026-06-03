import { request } from './client';

export interface AuditEvent {
  id: string;
  actor_id: string;
  action: string;
  resource_type: string;
  resource_rid: string;
  diff_json?: unknown;
  ip: string;
  user_agent: string;
  ts: string;
}

export interface AuditEventsResponse {
  data: AuditEvent[];
  nextPageToken?: string;
}

export interface ListAuditEventsParams {
  actor?: string;
  action?: string;
  resource_type?: string;
  /**
   * Pull every audit row for a single resource (US-493). Serialized to the
   * `resourceRid` query param — the camelCase name the backend handler reads
   * first (it also accepts `resource_rid`, but we send the canonical form).
   */
  resourceRid?: string;
  since?: string;
  until?: string;
  pageSize?: number;
  pageToken?: string;
}

export function listAuditEvents(
  params: ListAuditEventsParams = {},
): Promise<AuditEventsResponse> {
  const query = new URLSearchParams();
  if (params.actor) query.set('actor', params.actor);
  if (params.action) query.set('action', params.action);
  if (params.resource_type) query.set('resource_type', params.resource_type);
  if (params.resourceRid) query.set('resourceRid', params.resourceRid);
  if (params.since) query.set('since', params.since);
  if (params.until) query.set('until', params.until);
  if (params.pageSize) query.set('pageSize', String(params.pageSize));
  if (params.pageToken) query.set('pageToken', params.pageToken);
  const qs = query.toString();
  return request<AuditEventsResponse>(
    'GET',
    `/api/v2/admin/auditEvents${qs ? `?${qs}` : ''}`,
  );
}
