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

// ---------------------------------------------------------------------------
// US-042 (PC-A07b): Column-level masking policies.
//
// Mirrors pkg/masking.{Handler,ColumnMask,AppliesTo,ColumnMaskUpdate}. The
// backend exposes CRUD under /api/admin/column-masks. Like row-policies the
// path lives outside /api/(v2|admin)/ontologies/{name}/... so the global
// admin route is NOT branch-rewritten by client.ts:30.
//
// Semantic flip vs. RowPolicy.AppliesTo:
//   - RowPolicy.AppliesTo identifies callers GOVERNED by the predicate. A
//     match means "this policy filters my reads".
//   - ColumnMask.AppliesTo identifies callers ALLOWED to see the CLEAR
//     value. A match means "I am exempt from this mask — the column reads
//     untouched". Non-matching callers receive the masked value.
//
// The simulator UI surfaces both terms explicitly so operators don't
// confuse the two; see pkg/masking/model.go:52 for the canonical comment.
// ---------------------------------------------------------------------------

export type MaskRule = 'hash' | 'redact' | 'partial';

export const KNOWN_MASK_RULES: ReadonlyArray<MaskRule> = [
  'hash',
  'redact',
  'partial',
];

export interface ColumnMask {
  rid: string;
  objectTypeRid: string;
  propertyApiName: string;
  maskRule: MaskRule;
  appliesTo: AppliesTo;
  description?: string;
  createdBy?: string;
  createdAt?: string;
  updatedAt?: string;
}

export interface ListColumnMasksResponse {
  masks: ColumnMask[];
}

export interface CreateColumnMaskRequest {
  objectTypeRid: string;
  propertyApiName: string;
  maskRule: MaskRule;
  appliesTo: AppliesTo;
  description?: string;
}

export interface UpdateColumnMaskRequest {
  maskRule?: MaskRule;
  appliesTo?: AppliesTo;
  description?: string;
}

const COLUMN_MASKS_PREFIX = '/api/admin/column-masks';

export async function listColumnMasks(
  params: { objectTypeRid?: string } = {},
): Promise<ColumnMask[]> {
  const qs = new URLSearchParams();
  if (params.objectTypeRid) qs.set('objectType', params.objectTypeRid);
  const search = qs.toString();
  const resp = await request<ListColumnMasksResponse>(
    'GET',
    `${COLUMN_MASKS_PREFIX}${search ? `?${search}` : ''}`,
  );
  return resp.masks ?? [];
}

export function getColumnMask(rid: string): Promise<ColumnMask> {
  return request<ColumnMask>(
    'GET',
    `${COLUMN_MASKS_PREFIX}/${encodeURIComponent(rid)}`,
  );
}

export function createColumnMask(
  body: CreateColumnMaskRequest,
): Promise<ColumnMask> {
  return request<ColumnMask>('POST', COLUMN_MASKS_PREFIX, body);
}

export function updateColumnMask(
  rid: string,
  body: UpdateColumnMaskRequest,
): Promise<ColumnMask> {
  return request<ColumnMask>(
    'PATCH',
    `${COLUMN_MASKS_PREFIX}/${encodeURIComponent(rid)}`,
    body,
  );
}

export function deleteColumnMask(rid: string): Promise<void> {
  return request<void>(
    'DELETE',
    `${COLUMN_MASKS_PREFIX}/${encodeURIComponent(rid)}`,
  );
}

// Mirrors the same set-intersection algorithm pkg/masking.AppliesTo
// uses (model.go:64). Reuses isPolicyApplicable because the matching
// logic is identical even though the semantic meaning of a hit is
// inverted ("exempt from mask" rather than "governed by policy"). The
// simulator UI is in charge of labeling the hit correctly.
export function isMaskExempt(applies: AppliesTo, user: SimulatedUser): boolean {
  return isPolicyApplicable(applies, user);
}
