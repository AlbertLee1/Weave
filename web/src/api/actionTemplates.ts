import { request } from './client';

// ActionParameters is the front-end-owned wire shape for a saved
// parameter set. Matches Record<paramId, value> — the backend stores
// it as opaque JSONB and round-trips it untouched, so the SPA can
// evolve the parameter shape without a schema change.
export type ActionParameters = Record<string, unknown>;

export interface ActionTemplate {
  id: string;
  name: string;
  ontology: string;
  actionType: string;
  createdBy: string;
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
