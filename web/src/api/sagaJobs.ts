import { request } from './client';

// US-044 (PC-A08): saga / job monitoring read paths. Mirrors the wire
// shape produced by pkg/actions/saga_handler.go::{ListSagas,GetSaga}
// + the existing SagaStore models in pkg/actions/saga_store.go.

// Saga lifecycle states match the CHECK constraint on action_sagas.
// Five states drive the page's status filter and the colored badge.
export type SagaStatus =
  | 'RUNNING'
  | 'SUCCESS'
  | 'COMPENSATING'
  | 'COMPENSATED'
  | 'FAILED';

export const SAGA_STATUSES: SagaStatus[] = [
  'RUNNING',
  'SUCCESS',
  'COMPENSATING',
  'COMPENSATED',
  'FAILED',
];

// Per-step lifecycle. The detail drawer uses these to render the
// timeline marker (PENDING → APPLIED, COMPENSATED on rollback,
// COMPENSATION_FAILED → DLQ link).
export type SagaStepStatus =
  | 'PENDING'
  | 'APPLIED'
  | 'FAILED'
  | 'COMPENSATED'
  | 'COMPENSATION_FAILED';

export interface Saga {
  sagaId: string;
  idempotencyKey?: string;
  ontology: string;
  status: SagaStatus;
  requestedBy?: string;
  failureMessage?: string;
  createdAt: string;
  updatedAt: string;
}

export interface SagaStep {
  stepId: string;
  sagaId: string;
  stepIndex: number;
  actionType: string;
  // The four JSON.RawMessage fields are pretty-printed in the detail
  // drawer's expandable "edits" / "inverse edits" panels.
  parameters?: unknown;
  editsJson?: unknown;
  inverseEditsJson?: unknown;
  status: SagaStepStatus;
  createdAt: string;
  updatedAt: string;
}

export interface ListSagasParams {
  status?: SagaStatus;
  limit?: number;
  offset?: number;
}

export interface ListSagasResponse {
  data: Saga[];
}

export function listSagas(
  ontologyApiName: string,
  params: ListSagasParams = {},
): Promise<ListSagasResponse> {
  const qs = new URLSearchParams();
  if (params.status) qs.set('status', params.status);
  if (params.limit !== undefined) qs.set('limit', String(params.limit));
  if (params.offset !== undefined) qs.set('offset', String(params.offset));
  const search = qs.toString();
  return request<ListSagasResponse>(
    'GET',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/actions/sagas${search ? `?${search}` : ''}`,
  );
}

export interface GetSagaResponse {
  saga: Saga;
  steps: SagaStep[];
}

export function getSaga(
  ontologyApiName: string,
  sagaId: string,
): Promise<GetSagaResponse> {
  return request<GetSagaResponse>(
    'GET',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/actions/sagas/${encodeURIComponent(sagaId)}`,
  );
}
