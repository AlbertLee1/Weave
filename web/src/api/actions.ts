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
