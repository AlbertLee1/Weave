import { request } from './client';
import type {
  ActionApplyRequest,
  ActionApplyResponse,
  ActionBatchApplyOptions,
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

// applyActionAsync POSTs to /apply?async=true and returns the new job's id.
// The server (pkg/actions/handlers.go US-240) persists a PENDING action_jobs
// row, returns 202 {jobId, status}, and runs the Apply in a detached
// goroutine. When no ActionJobStore is wired the query param is silently
// ignored and the call falls through to the sync envelope — callers must
// therefore tolerate a body that may be either AsyncApplyResponse (has jobId)
// or the plain ActionApplyResponse. The hook layer branches on `jobId`.
// Caller polls GET /actions/jobs/{jobId} until a terminal status.
export function applyActionAsync(
  ontologyApiName: string,
  actionApiName: string,
  actionRequest: ActionApplyRequest,
): Promise<AsyncApplyResponse> {
  return request<AsyncApplyResponse>(
    'POST',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/actions/${encodeURIComponent(actionApiName)}/apply?async=true`,
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

// ── Real-time field-level validation ────────────────────────────────────────
//
// Foundry OSv2 exposes a dedicated, side-effect-free validate surface so an
// editing form can red-line individual fields as the user types without
// running (or even validate-only-applying) the action. The server mirror is
// pkg/actions/handlers.go::Validate + pkg/actions/executor.go's
// ValidateActionResponse. The endpoint ALWAYS returns HTTP 200 — even when
// `result === 'INVALID'` — because the envelope is a validation report, not
// an HTTP error. Callers must therefore branch on `result`, not on the HTTP
// status. The action API name lives in the URL; the body carries only
// { parameters } (any actionType field in the body is ignored server-side).
//
//   POST /api/v2/ontologies/{ontology}/actions/{action}/validate

// EvaluatedConstraint mirrors actions.EvaluatedConstraint — one evaluated
// constraint on a parameter (e.g. {type:'required', result:'INVALID'}). The
// taxonomy is forwards-compatible: switch on `type` and tolerate unknown
// kinds.
export interface EvaluatedConstraint {
  type: string;
  result: 'VALID' | 'INVALID';
}

// ParameterValidationResult mirrors actions.ParameterValidationResult — the
// per-parameter entry in ValidateActionResponse.parameters, keyed by
// parameter id. `result === 'INVALID'` is what a form maps to a field-level
// (inline) error.
export interface ParameterValidationResult {
  result: 'VALID' | 'INVALID';
  required: boolean;
  evaluatedConstraints: EvaluatedConstraint[];
}

// SubmissionCriterionResult mirrors actions.SubmissionCriterionResult — one
// submission-criteria envelope. On INVALID the server synthesizes a single
// entry carrying the underlying validation error verbatim in
// configuredFailureMessage, which a form renders as a form-level banner.
export interface SubmissionCriterionResult {
  result: 'VALID' | 'INVALID';
  configuredFailureMessage?: string;
}

// ValidateActionResponse mirrors actions.ValidateActionResponse — the wire
// envelope of the /validate endpoint. submissionCriteria and parameters are
// always present ([]/{}) so callers can iterate without nil-guards.
export interface ValidateActionResponse {
  result: 'VALID' | 'INVALID';
  submissionCriteria: SubmissionCriterionResult[];
  parameters: Record<string, ParameterValidationResult>;
}

// validateAction POSTs the current parameter draft to the dedicated validate
// surface and returns the structured per-parameter + form-level report. It
// never throws on an INVALID result (that's a 200 carrying result:'INVALID');
// it only rejects on transport / 4xx-5xx errors via the shared `request`
// helper.
export function validateAction(
  ontologyApiName: string,
  actionApiName: string,
  parameters: Record<string, unknown>,
): Promise<ValidateActionResponse> {
  return request<ValidateActionResponse>(
    'POST',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/actions/${encodeURIComponent(actionApiName)}/validate`,
    { parameters },
  );
}

// AsyncApplyResponse mirrors the server's actions.AsyncApplyResponse — the
// 202 envelope returned by ?async=true on /apply or /applyBatch. US-240/US-318.
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
  // Same narrowed batch option set as the sync path — the server rejects
  // ALL_V2_WITH_DELETIONS on /applyBatch (sync or async) with 400.
  options?: ActionBatchApplyOptions;
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

// revertActionLog reverses a persisted action by publishing a reverse
// EditBatch (CREATE→DELETE, MODIFY→MODIFY-with-prevState, DELETE→CREATE,
// LINK_CREATE↔LINK_DELETE). The original action_logs row is marked REVERTED;
// a second call returns 409 AlreadyReverted. US-319 wires this to the toast
// Undo button and the Action History per-row Undo affordance.
//
//   POST /api/v2/ontologies/{ontology}/actions/revert
//   body: { actionLogId }
//
// Response shape mirrors apply's SyncApplyActionResponseV2 — operationId is
// the reverse batch id, edits is the standard counts envelope.
export interface RevertActionResponse {
  operationId?: string;
  edits?: import('./types').ActionResults;
}

export function revertActionLog(
  ontologyApiName: string,
  actionLogId: number,
): Promise<RevertActionResponse> {
  return request<RevertActionResponse>(
    'POST',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/actions/revert`,
    { actionLogId },
  );
}
