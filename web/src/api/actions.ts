import { request } from './client';
import type { ActionApplyRequest, ActionApplyResponse } from './types';

export function applyAction(
  ontologyApiName: string,
  actionRequest: ActionApplyRequest,
): Promise<ActionApplyResponse> {
  return request<ActionApplyResponse>(
    'POST',
    `/api/v2/ontologies/${ontologyApiName}/actions/apply`,
    actionRequest,
  );
}
