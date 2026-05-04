import { request } from './client';

// SagaDLQEntry mirrors pkg/actions/saga_store.go::SagaDLQEntry. The
// fields are the on-wire shape produced by ListSagaDLQ.
export interface SagaDLQEntry {
  dlqId: string;
  sagaId: string;
  stepId: string;
  ontology: string;
  editsJson?: unknown;
  failureMessage?: string;
  status: 'PENDING' | 'RESOLVED' | 'DROPPED';
  attempts: number;
  lastAttemptAt?: string;
  createdAt: string;
  updatedAt: string;
}

export interface ListSagaDLQResponse {
  entries: SagaDLQEntry[];
}

export type SagaDLQStatusFilter = 'PENDING' | 'RESOLVED' | 'DROPPED';

export interface ListSagaDLQParams {
  status?: SagaDLQStatusFilter;
  limit?: number;
}

export function listSagaDLQ(
  ontologyApiName: string,
  params: ListSagaDLQParams = {},
): Promise<ListSagaDLQResponse> {
  const query = new URLSearchParams();
  if (params.status) query.set('status', params.status);
  if (params.limit !== undefined) query.set('limit', String(params.limit));
  const qs = query.toString();
  return request<ListSagaDLQResponse>(
    'GET',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/actions/saga/dlq${qs ? `?${qs}` : ''}`,
  );
}

export interface SagaDLQActionResponse {
  dlqId: string;
  status: 'RESOLVED' | 'DROPPED';
}

export function retrySagaDLQ(
  ontologyApiName: string,
  dlqId: string,
): Promise<SagaDLQActionResponse> {
  return request<SagaDLQActionResponse>(
    'POST',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/actions/saga/dlq/${encodeURIComponent(dlqId)}/retry`,
    {},
  );
}

export function dropSagaDLQ(
  ontologyApiName: string,
  dlqId: string,
): Promise<SagaDLQActionResponse> {
  return request<SagaDLQActionResponse>(
    'POST',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/actions/saga/dlq/${encodeURIComponent(dlqId)}/drop`,
    {},
  );
}
