import { request } from './client';

// AutomationRule mirrors pkg/oms.AutomationRule (models.go:710). The
// `triggerConfig`, `effects`, and `retryPolicy` fields are JSON.RawMessage
// on the wire — opaque blobs the UI parses + re-stringifies for editing.
export interface AutomationRule {
  id: string;
  ontologyRid: string;
  name: string;
  description?: string;
  // Status drives the pause/resume toggle. Backend persists "active",
  // "paused", or "disabled"; the UI only swaps between active/paused.
  status: 'active' | 'paused' | 'disabled';
  // TriggerType is one of: "schedule", "dataChange", "manual"
  // (CreateAutomationRule validates this allowlist).
  triggerType: 'schedule' | 'dataChange' | 'manual';
  triggerConfig?: unknown;
  effects?: unknown;
  retryPolicy?: unknown;
  createdBy?: string;
  createdAt: string;
  updatedAt: string;
}

export interface ListAutomationRulesResponse {
  data: AutomationRule[];
}

export interface ListAutomationRulesParams {
  status?: 'active' | 'paused' | 'disabled';
}

export function listAutomationRules(
  ontology: string,
  params: ListAutomationRulesParams = {},
): Promise<ListAutomationRulesResponse> {
  const qs = new URLSearchParams();
  if (params.status) qs.set('status', params.status);
  const search = qs.toString();
  return request<ListAutomationRulesResponse>(
    'GET',
    `/api/v2/ontologies/${encodeURIComponent(ontology)}/automationRules${search ? `?${search}` : ''}`,
  );
}

export interface CreateAutomationRuleRequest {
  name: string;
  description?: string;
  triggerType: 'schedule' | 'dataChange' | 'manual';
  triggerConfig?: unknown;
  effects?: unknown;
  retryPolicy?: unknown;
  createdBy?: string;
}

export function createAutomationRule(
  ontology: string,
  body: CreateAutomationRuleRequest,
): Promise<AutomationRule> {
  return request<AutomationRule>(
    'POST',
    `/api/v2/ontologies/${encodeURIComponent(ontology)}/automationRules`,
    body,
  );
}

export interface UpdateAutomationRuleRequest {
  name?: string;
  description?: string;
  triggerType?: 'schedule' | 'dataChange' | 'manual';
  triggerConfig?: unknown;
  effects?: unknown;
  retryPolicy?: unknown;
}

// NOTE: pkg/oms ListAutomationRules / GetAutomationRule / Pause / Resume
// / ListExecutions chi handlers treat the `{ontologyApiName}` URL segment
// as the raw RID (see pkg/oms/handlers_automation.go:111). The frontend
// uses the apiName slug consistently — for `dev` mode the apiName slug
// equals the storage RID for the demo ontology, but if you ever hit
// `OntologyNotFound` here the cause is likely apiName-vs-RID confusion.
export function updateAutomationRule(
  ontology: string,
  ruleId: string,
  body: UpdateAutomationRuleRequest,
): Promise<AutomationRule> {
  return request<AutomationRule>(
    'PUT',
    `/api/v2/ontologies/${encodeURIComponent(ontology)}/automationRules/${encodeURIComponent(ruleId)}`,
    body,
  );
}

export function deleteAutomationRule(
  ontology: string,
  ruleId: string,
): Promise<void> {
  return request<void>(
    'DELETE',
    `/api/v2/ontologies/${encodeURIComponent(ontology)}/automationRules/${encodeURIComponent(ruleId)}`,
  );
}

export function pauseAutomationRule(
  ontology: string,
  ruleId: string,
): Promise<AutomationRule> {
  return request<AutomationRule>(
    'POST',
    `/api/v2/ontologies/${encodeURIComponent(ontology)}/automationRules/${encodeURIComponent(ruleId)}/pause`,
    {},
  );
}

export function resumeAutomationRule(
  ontology: string,
  ruleId: string,
): Promise<AutomationRule> {
  return request<AutomationRule>(
    'POST',
    `/api/v2/ontologies/${encodeURIComponent(ontology)}/automationRules/${encodeURIComponent(ruleId)}/resume`,
    {},
  );
}

// AutomationExecution mirrors pkg/oms.AutomationExecution (models.go:726).
// `triggerEvent` / `result` are JSON.RawMessage and surface as opaque
// JSON for the executions drawer to pretty-print.
export interface AutomationExecution {
  id: string;
  ruleId: string;
  triggerEvent?: unknown;
  startedAt: string;
  completedAt?: string;
  status: 'running' | 'success' | 'error' | 'retrying';
  error?: string;
  retryCount: number;
  result?: unknown;
}

export interface ListExecutionsResponse {
  data: AutomationExecution[];
  total: number;
  offset: number;
  limit: number;
}

export interface ListExecutionsParams {
  status?: 'running' | 'success' | 'error' | 'retrying';
  limit?: number;
  offset?: number;
}

export function listAutomationExecutions(
  ontology: string,
  ruleId: string,
  params: ListExecutionsParams = {},
): Promise<ListExecutionsResponse> {
  const qs = new URLSearchParams();
  if (params.status) qs.set('status', params.status);
  if (params.limit !== undefined) qs.set('limit', String(params.limit));
  if (params.offset !== undefined) qs.set('offset', String(params.offset));
  const search = qs.toString();
  return request<ListExecutionsResponse>(
    'GET',
    `/api/v2/ontologies/${encodeURIComponent(ontology)}/automationRules/${encodeURIComponent(ruleId)}/executions${search ? `?${search}` : ''}`,
  );
}
