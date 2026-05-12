import { request } from './client';

// US-041 (PC-A07a): Row-level security policies API.
//
// Mirrors pkg/rls.{Handler,RowPolicy,AppliesTo,RowPolicyUpdate}. The backend
// stores the row predicate as a serialised pkg/oss/where.WhereClause (JSON);
// the PRD AC mentions a "CEL editor" but the wire format is in fact a JSON
// where-clause object, so the UI surfaces a JSON editor (not CEL). The
// editor enforces JSON.parse validity client-side as the equivalent of
// "syntax highlight + lint" against the actual contract.
//
// Routes are mounted under /api/admin/row-policies and require the
// PermUserManage permission at the chi.Router group level (handlers.go:46).
// extractOntologyApiName in client.ts only matches /api/(v2|admin)/ontologies/...
// so these calls are NOT branch-rewritten — admin row-policies are global.

export interface AppliesTo {
  roles?: string[];
  groups?: string[];
  users?: string[];
}

export interface RowPolicy {
  rid: string;
  objectTypeRid: string;
  predicate: unknown;
  appliesTo: AppliesTo;
  description?: string;
  createdBy?: string;
  createdAt?: string;
  updatedAt?: string;
}

export interface ListRowPoliciesResponse {
  policies: RowPolicy[];
}

export interface CreateRowPolicyRequest {
  objectTypeRid: string;
  predicate: unknown;
  appliesTo: AppliesTo;
  description?: string;
}

export interface UpdateRowPolicyRequest {
  predicate?: unknown;
  appliesTo?: AppliesTo;
  description?: string;
}

const ADMIN_PREFIX = '/api/admin/row-policies';

export async function listRowPolicies(params: {
  objectTypeRid?: string;
} = {}): Promise<RowPolicy[]> {
  const qs = new URLSearchParams();
  if (params.objectTypeRid) qs.set('objectType', params.objectTypeRid);
  const search = qs.toString();
  const resp = await request<ListRowPoliciesResponse>(
    'GET',
    `${ADMIN_PREFIX}${search ? `?${search}` : ''}`,
  );
  return resp.policies ?? [];
}

export function getRowPolicy(rid: string): Promise<RowPolicy> {
  return request<RowPolicy>('GET', `${ADMIN_PREFIX}/${encodeURIComponent(rid)}`);
}

export function createRowPolicy(
  body: CreateRowPolicyRequest,
): Promise<RowPolicy> {
  return request<RowPolicy>('POST', ADMIN_PREFIX, body);
}

export function updateRowPolicy(
  rid: string,
  body: UpdateRowPolicyRequest,
): Promise<RowPolicy> {
  return request<RowPolicy>(
    'PATCH',
    `${ADMIN_PREFIX}/${encodeURIComponent(rid)}`,
    body,
  );
}

export function deleteRowPolicy(rid: string): Promise<void> {
  return request<void>(
    'DELETE',
    `${ADMIN_PREFIX}/${encodeURIComponent(rid)}`,
  );
}

// Mirrors pkg/rls.AppliesTo.IsApplicable (model.go:38). A caller matches
// when ANY of the three dimensions overlap with the caller identity.
// Empty AppliesTo matches nobody — this is intentional (an unscoped
// policy must explicitly enumerate at least one role/group/user before it
// governs anyone). Used by the "Test as user" simulator panel.
export interface SimulatedUser {
  id: string;
  email?: string;
  roles: string[];
  groups: string[];
}

export function isPolicyApplicable(
  applies: AppliesTo,
  user: SimulatedUser,
): boolean {
  const roles = applies.roles ?? [];
  if (roles.length > 0) {
    for (const r of roles) {
      if (user.roles.includes(r)) return true;
    }
  }
  const groups = applies.groups ?? [];
  if (groups.length > 0 && user.groups.length > 0) {
    for (const g of groups) {
      if (user.groups.includes(g)) return true;
    }
  }
  const users = applies.users ?? [];
  if (users.length > 0) {
    for (const u of users) {
      if (!u) continue;
      if (u === user.id || (user.email && u === user.email)) return true;
    }
  }
  return false;
}
