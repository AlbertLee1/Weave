import { request } from './client';

// Feature Flags admin surface (US-276). Mirrors the wire shapes emitted by
// pkg/featureflags/handlers.go + pkg/featureflags/flag.go. The backend exposes
// full CRUD on named flags; each flag carries a global `enabled` switch plus
// two targeted-rollout allow-lists (`realms` / `users`) that scope the flag to
// specific realms / users even while the global switch is off.

// FeatureFlag mirrors pkg/featureflags/flag.go Flag. realms / users may be
// omitted by the server (`omitempty`) when empty, so callers must coalesce to
// [] before reading `.length`.
export interface FeatureFlag {
  name: string;
  description: string;
  enabled: boolean;
  realms: string[];
  users: string[];
  createdAt: string;
  updatedAt: string;
}

// CreateFlagRequest mirrors pkg/featureflags/handlers.go createFlagRequest —
// name is required; the rest default (empty description, enabled=false, empty
// scopes) when omitted.
export interface CreateFlagRequest {
  name: string;
  description: string;
  enabled: boolean;
  realms: string[];
  users: string[];
}

// UpdateFlagRequest mirrors pkg/featureflags/handlers.go updateFlagRequest —
// every field is optional (omitted fields are preserved via pointer PATCH
// semantics server-side). The inline `enabled`-only toggle uses this shape.
export interface UpdateFlagRequest {
  description?: string;
  enabled?: boolean;
  realms?: string[];
  users?: string[];
}

interface FlagListResponse {
  flags: FeatureFlag[];
}

// listFlags maps GET /api/admin/feature-flags → FeatureFlag[]. The server
// always emits a `flags` array (possibly empty); coalesce to [] defensively.
export async function listFlags(): Promise<FeatureFlag[]> {
  const resp = await request<FlagListResponse>('GET', '/api/admin/feature-flags');
  return resp.flags ?? [];
}

// getFlag maps GET /api/admin/feature-flags/{name} → FeatureFlag.
export async function getFlag(name: string): Promise<FeatureFlag> {
  return request<FeatureFlag>(
    'GET',
    `/api/admin/feature-flags/${encodeURIComponent(name)}`,
  );
}

// createFlag maps POST /api/admin/feature-flags → FeatureFlag (201).
export async function createFlag(body: CreateFlagRequest): Promise<FeatureFlag> {
  return request<FeatureFlag>('POST', '/api/admin/feature-flags', body);
}

// updateFlag maps PUT /api/admin/feature-flags/{name} → the updated FeatureFlag.
export async function updateFlag(
  name: string,
  body: UpdateFlagRequest,
): Promise<FeatureFlag> {
  return request<FeatureFlag>(
    'PUT',
    `/api/admin/feature-flags/${encodeURIComponent(name)}`,
    body,
  );
}

// deleteFlag maps DELETE /api/admin/feature-flags/{name} (204/200).
export async function deleteFlag(name: string): Promise<void> {
  await request<void>(
    'DELETE',
    `/api/admin/feature-flags/${encodeURIComponent(name)}`,
  );
}
