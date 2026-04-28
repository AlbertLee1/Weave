import { request } from './client';
import type {
  ActionApplyRequest,
  ActionApplyResponse,
  ActionBatchApplyRequest,
  BatchApplyActionResponse,
} from './types';

// Foundry OSv2 action apply: the action API name is a path segment.
// The request body carries only { parameters, options? }; any actionType
// field is silently ignored server-side.
//
//   POST /api/v2/ontologies/{ontology}/actions/{action}/apply
//
// cf. palantir/foundry-platform-python action.py:58
export function applyAction(
  ontologyApiName: string,
  actionApiName: string,
  actionRequest: ActionApplyRequest,
): Promise<ActionApplyResponse> {
  return request<ActionApplyResponse>(
    'POST',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/actions/${encodeURIComponent(actionApiName)}/apply`,
    actionRequest,
  );
}

// Foundry OSv2 batch apply.
export function applyBatch(
  ontologyApiName: string,
  actionApiName: string,
  batchRequest: ActionBatchApplyRequest,
): Promise<BatchApplyActionResponse> {
  return request<BatchApplyActionResponse>(
    'POST',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/actions/${encodeURIComponent(actionApiName)}/applyBatch`,
    batchRequest,
  );
}

// AsyncApplyResponse mirrors the server's actions.AsyncApplyResponse — the
// 202 envelope returned by ?async=true on /apply or /applyBatch. US-318.
export interface AsyncApplyResponse {
  jobId: string;
  status: string;
}

// AsyncBatchApplyRequest is the wire shape for the async batch path. The
// server reads `actions:` (matching its existing `actions:` json tag for the
// sync path), distinct from the bare-`requests:` shape some legacy callers
// pass to applyBatch — keeping the new path on the canonical key avoids
// inheriting any existing wire mismatch.
export interface AsyncBatchApplyRequest {
  actions: Array<{ parameters: Record<string, unknown> }>;
  options?: { returnEdits?: 'ALL' | 'NONE' };
}

// applyBatchAsync POSTs to /applyBatch?async=true and returns the new job's
// id. Caller is expected to subscribe to live progress via WebSocket
// subscribeActionJob {jobId} and/or poll GET /actions/jobs/{jobId}. US-318.
export function applyBatchAsync(
  ontologyApiName: string,
  actionApiName: string,
  body: AsyncBatchApplyRequest,
): Promise<AsyncApplyResponse> {
  return request<AsyncApplyResponse>(
    'POST',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/actions/${encodeURIComponent(actionApiName)}/applyBatch?async=true`,
    body,
  );
}

// ActionJobStatus enumerates every state action_jobs.status may take. US-318
// adds CANCELED to the existing US-240 set.
export type ActionJobStatus =
  | 'PENDING'
  | 'RUNNING'
  | 'SUCCEEDED'
  | 'FAILED'
  | 'CANCELED';

// ActionJob mirrors the server's actions.ActionJob row. Result is left as
// unknown — handlers serialise SyncApplyActionResponseV2 / BatchApplyActionResponseV2
// into it depending on the source endpoint.
export interface ActionJob {
  jobId: string;
  ontologyApiName: string;
  actionType: string;
  status: ActionJobStatus;
  progress: number;
  result?: unknown;
  errorMessage?: string;
  createdBy?: string;
  createdAt: string;
  updatedAt: string;
}

// getActionJob fetches the persisted state of an async action job. Useful as
// a fallback for clients that miss live WebSocket events (reconnect window).
export function getActionJob(
  ontologyApiName: string,
  jobId: string,
): Promise<ActionJob> {
  return request<ActionJob>(
    'GET',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/actions/jobs/${encodeURIComponent(jobId)}`,
  );
}

// cancelActionJob signals the in-flight worker to stop. Returns the current
// job row (status will likely still be RUNNING — the worker marks CANCELED
// in its own time once the next iteration boundary is reached). 404 on
// unknown jobs, 409 ActionJobAlreadyTerminal on already-finished jobs.
// US-318.
export function cancelActionJob(
  ontologyApiName: string,
  jobId: string,
): Promise<ActionJob> {
  return request<ActionJob>(
    'POST',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/actions/jobs/${encodeURIComponent(jobId)}/cancel`,
  );
}
