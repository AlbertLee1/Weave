import { request } from './client';

// ActionParameters is the front-end-owned wire shape for a saved
// parameter set. Matches Record<paramId, value> — the backend stores
// it as opaque JSONB and round-trips it untouched, so the SPA can
// evolve the parameter shape without a schema change.
export type ActionParameters = Record<string, unknown>;

// US-427: action templates are visible at three discrete levels.
// PRIVATE = owner only; TEAM = anyone sharing an auth.Group with the
// owner; PUBLIC = any authenticated user. The legacy `shared`
// boolean is kept on the wire for v1 SDK compatibility and is the
// projection `scope !== 'PRIVATE'`.
export type ActionTemplateScope = 'PRIVATE' | 'TEAM' | 'PUBLIC';

export interface ActionTemplate {
  id: string;
  name: string;
  ontology: string;
  actionType: string;
  createdBy: string;
  scope: ActionTemplateScope;
  shared: boolean;
  parameters: ActionParameters;
  createdAt: string;
  updatedAt: string;
}

export interface ListActionTemplatesResponse {
  actionTemplates: ActionTemplate[];
}

export interface ListActionTemplatesParams {
  ontology: string;
  actionType: string;
}

export function listActionTemplates(
  params: ListActionTemplatesParams,
): Promise<ListActionTemplatesResponse> {
  const qs = new URLSearchParams({
    ontology: params.ontology,
    actionType: params.actionType,
  });
  return request<ListActionTemplatesResponse>(
    'GET',
    `/api/v2/action-templates?${qs.toString()}`,
  );
}

export interface CreateActionTemplateInput {
  name: string;
  ontology: string;
  actionType: string;
  scope?: ActionTemplateScope;
  // Legacy boolean retained for callers still on the US-320 wire.
  // When `scope` is supplied it wins; otherwise `shared:true` maps to
  // PUBLIC and `shared:false` (or absent) to PRIVATE.
  shared?: boolean;
  parameters: ActionParameters;
}

export function createActionTemplate(
  input: CreateActionTemplateInput,
): Promise<ActionTemplate> {
  return request<ActionTemplate>('POST', '/api/v2/action-templates', input);
}

export interface UpdateActionTemplateInput {
  id: string;
  name?: string;
  scope?: ActionTemplateScope;
  shared?: boolean;
  parameters?: ActionParameters;
}

export function updateActionTemplate(
  input: UpdateActionTemplateInput,
): Promise<ActionTemplate> {
  const { id, ...body } = input;
  return request<ActionTemplate>(
    'PUT',
    `/api/v2/action-templates/${encodeURIComponent(id)}`,
    body,
  );
}

export function deleteActionTemplate(id: string): Promise<void> {
  return request<void>(
    'DELETE',
    `/api/v2/action-templates/${encodeURIComponent(id)}`,
  );
}
