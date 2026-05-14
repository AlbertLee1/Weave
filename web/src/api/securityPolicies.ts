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

// ---------------------------------------------------------------------------
// US-043 (PC-A07c): Cell-level masking policies (CEL).
//
// Mirrors pkg/cellsec.{Handler,CellMask,CreateRequest,CellMaskUpdate}.
// Routes are mounted under /api/admin/cell-masks; the path lives outside
// /api/(v2|admin)/ontologies/{name}/... so the global admin route is NOT
// branch-rewritten by client.ts:30.
//
// Wire shape differences vs RowPolicy/ColumnMask:
//   - PrimaryKey + PropertyAPIName together pin the mask to one specific
//     (object instance, property) cell — column masks operate on every row
//     for the property; cell masks operate on one row's property only.
//   - Two distinct mask-name taxonomies coexist (US-376):
//       * MaskRule = lowercase legacy `hash | redact | partial` (matches
//         column-mask shape).
//       * MaskStrategy = uppercase `REDACT | HASH | NULL | PARTIAL`. When
//         set MaskStrategy wins; when empty backend derives strategy from
//         MaskRule. The PRD AC nominates Cell Masks as the "CEL" surface
//         so this UI accepts a mask-strategy enum from the start.
//   - Expression = optional CEL predicate evaluated server-side per row at
//     read time. When non-empty AND it returns true, MASK applies (caller
//     receives the masked value). When empty, the engine falls back to the
//     legacy AppliesTo allow list (matching = exempt). Note: this is the
//     INVERSE direction from RowPolicy (matching = governed), and DIFFERENT
//     from ColumnMask (allow list only — no Expression hook on column).
//
// The "Test as user" simulator panel honest-maps the dual-mode contract:
// (a) masks with a non-empty Expression cannot be evaluated client-side
// (CEL evaluation needs the full row + cel-go runtime), so the panel
// surfaces them with a "server-side" marker; (b) masks with empty
// Expression fall back to allow-list semantics shared with column masks.
// ---------------------------------------------------------------------------

export type MaskStrategy = 'REDACT' | 'HASH' | 'NULL' | 'PARTIAL';

export const KNOWN_MASK_STRATEGIES: ReadonlyArray<MaskStrategy> = [
  'REDACT',
  'HASH',
  'NULL',
  'PARTIAL',
];

export interface CellMask {
  rid: string;
  objectTypeRid: string;
  primaryKey: string;
  propertyApiName: string;
  maskRule?: MaskRule;
  maskStrategy?: MaskStrategy;
  expression?: string;
  appliesTo: AppliesTo;
  description?: string;
  createdBy?: string;
  createdAt?: string;
  updatedAt?: string;
}

export interface ListCellMasksResponse {
  masks: CellMask[];
}

export interface CreateCellMaskRequest {
  objectTypeRid: string;
  primaryKey: string;
  propertyApiName: string;
  maskRule?: MaskRule;
  maskStrategy?: MaskStrategy;
  expression?: string;
  appliesTo: AppliesTo;
  description?: string;
}

export interface UpdateCellMaskRequest {
  maskRule?: MaskRule;
  maskStrategy?: MaskStrategy;
  expression?: string;
  appliesTo?: AppliesTo;
  description?: string;
}

const CELL_MASKS_PREFIX = '/api/admin/cell-masks';

export async function listCellMasks(
  params: { objectTypeRid?: string } = {},
): Promise<CellMask[]> {
  const qs = new URLSearchParams();
  if (params.objectTypeRid) qs.set('objectType', params.objectTypeRid);
  const search = qs.toString();
  const resp = await request<ListCellMasksResponse>(
    'GET',
    `${CELL_MASKS_PREFIX}${search ? `?${search}` : ''}`,
  );
  return resp.masks ?? [];
}

export function getCellMask(rid: string): Promise<CellMask> {
  return request<CellMask>(
    'GET',
    `${CELL_MASKS_PREFIX}/${encodeURIComponent(rid)}`,
  );
}

export function createCellMask(
  body: CreateCellMaskRequest,
): Promise<CellMask> {
  return request<CellMask>('POST', CELL_MASKS_PREFIX, body);
}

export function updateCellMask(
  rid: string,
  body: UpdateCellMaskRequest,
): Promise<CellMask> {
  return request<CellMask>(
    'PATCH',
    `${CELL_MASKS_PREFIX}/${encodeURIComponent(rid)}`,
    body,
  );
}

export function deleteCellMask(rid: string): Promise<void> {
  return request<void>(
    'DELETE',
    `${CELL_MASKS_PREFIX}/${encodeURIComponent(rid)}`,
  );
}

// Resolve the effective MaskStrategy from a CellMask the way pkg/cellsec
// does (model.go:75 — explicit MaskStrategy wins; otherwise derive from the
// legacy MaskRule). Returns the empty strategy when neither is set; the
// backend rejects this case at the admin boundary but the helper stays
// total so the UI can render "—" without exploding on transient state.
export function effectiveMaskStrategy(
  mask: Pick<CellMask, 'maskRule' | 'maskStrategy'>,
): MaskStrategy | '' {
  if (mask.maskStrategy) return mask.maskStrategy;
  if (!mask.maskRule) return '';
  switch (mask.maskRule) {
    case 'hash':
      return 'HASH';
    case 'redact':
      return 'REDACT';
    case 'partial':
      return 'PARTIAL';
  }
}

// previewMaskedValue renders a representative masked output for a strategy.
// Mirrors pkg/masking.ApplyMaskStrategy intent so admins can see what their
// callers will receive before the policy hits real data. The output here
// is intentionally illustrative — backend strategies/strategies.go remains
// authoritative for production values.
export function previewMaskedValue(
  strategy: MaskStrategy | '',
  sample: string,
): string {
  if (!strategy) return sample;
  switch (strategy) {
    case 'REDACT':
      return '***';
    case 'NULL':
      return 'null';
    case 'HASH':
      // sha256 hex prefix — illustrative; backend uses crypto/sha256.
      return 'sha256:' + simulatedHashPrefix(sample);
    case 'PARTIAL':
      return partialMask(sample);
  }
}

function simulatedHashPrefix(s: string): string {
  // 12-char alpha-numeric digest — deterministic per input so spec
  // assertions are stable without depending on Web Crypto.
  let h = 0x811c9dc5;
  for (let i = 0; i < s.length; i++) {
    h = Math.imul(h ^ s.charCodeAt(i), 0x01000193);
  }
  const hex = (h >>> 0).toString(16).padStart(8, '0');
  return hex + hex.slice(0, 4);
}

function partialMask(s: string): string {
  if (s.length <= 2) return s.replace(/./g, '*');
  return s[0] + '*'.repeat(Math.max(1, s.length - 2)) + s[s.length - 1];
}

// lintCellExpression performs a structural lint of a CEL expression string
// without taking on a full CEL parser. Mirrors the kind of checks
// pkg/cellsec/celmask.Validate performs server-side; the backend is the
// authority but client-side fast-fail keeps the editor responsive.
//
// Checks (cheap + deterministic):
//   - non-empty after trim
//   - balanced () and []
//   - balanced single/double quotes (counting escapes so "alice\" stays well-formed)
//   - no trailing binary operator (&&, ||, ==, !=, ., +)
//
// Returns null when the expression looks well-formed; otherwise a short
// human-readable reason. Callers should treat this as advisory and let the
// backend reject any final wire-shape violations.
export function lintCellExpression(expr: string): string | null {
  const trimmed = expr.trim();
  if (trimmed === '') return 'Expression is required.';

  let parens = 0;
  let brackets = 0;
  let inSingle = false;
  let inDouble = false;
  for (let i = 0; i < trimmed.length; i++) {
    const ch = trimmed[i];
    const prev = i > 0 ? trimmed[i - 1] : '';
    if (inSingle) {
      if (ch === "'" && prev !== '\\') inSingle = false;
      continue;
    }
    if (inDouble) {
      if (ch === '"' && prev !== '\\') inDouble = false;
      continue;
    }
    if (ch === "'") {
      inSingle = true;
      continue;
    }
    if (ch === '"') {
      inDouble = true;
      continue;
    }
    if (ch === '(') parens++;
    else if (ch === ')') {
      parens--;
      if (parens < 0) return 'Unbalanced parentheses.';
    } else if (ch === '[') brackets++;
    else if (ch === ']') {
      brackets--;
      if (brackets < 0) return 'Unbalanced brackets.';
    }
  }
  if (inSingle || inDouble) return 'Unterminated string literal.';
  if (parens !== 0) return 'Unbalanced parentheses.';
  if (brackets !== 0) return 'Unbalanced brackets.';

  if (/(&&|\|\||==|!=|\.|\+|-|\*|\/)\s*$/.test(trimmed)) {
    return 'Trailing operator.';
  }
  // Empty parens like `foo()` are valid; `()` standalone is not a useful
  // predicate but parses to a no-op — still let the backend reject it.

  return null;
}
