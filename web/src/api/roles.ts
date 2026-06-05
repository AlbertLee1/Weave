import { request } from './client';

// RoleResponse is the wire shape returned by the admin role endpoints
// (pkg/auth/role_handlers.go RoleResponse). `builtin` roles are protected:
// they cannot be deleted, and their permissions are defined by the static
// matrix and cannot be overridden — both the DELETE and PUT-permissions
// endpoints return 409 for them, so the UI must guard accordingly.
export interface RoleResponse {
  name: string;
  description: string;
  builtin: boolean;
  createdAt: string;
  permissions: string[];
}

// Role is an alias for the wire shape — exported so call sites can refer to
// the domain noun without coupling to the "*Response" naming.
export type Role = RoleResponse;

export interface CreateRoleRequest {
  name: string;
  description?: string;
  permissions?: string[];
}

interface RoleListResponse {
  roles: RoleResponse[];
}

interface RolePermissionsResponse {
  permissions: string[];
}

// listRoles maps GET /api/admin/roles → RoleResponse[]. The server always
// emits a `roles` array (possibly empty), but we coalesce to [] defensively.
export async function listRoles(): Promise<RoleResponse[]> {
  const resp = await request<RoleListResponse>('GET', '/api/admin/roles');
  return resp.roles ?? [];
}

// getRole maps GET /api/admin/roles/{name} → RoleResponse.
export async function getRole(name: string): Promise<RoleResponse> {
  return request<RoleResponse>(
    'GET',
    `/api/admin/roles/${encodeURIComponent(name)}`,
  );
}

// createRole maps POST /api/admin/roles → RoleResponse (201).
export async function createRole(
  body: CreateRoleRequest,
): Promise<RoleResponse> {
  return request<RoleResponse>('POST', '/api/admin/roles', body);
}

// deleteRole maps DELETE /api/admin/roles/{name}. Built-in roles return 409
// BuiltinRoleProtected — the UI disables the delete affordance for them, but
// the function still surfaces the error to the caller if invoked directly.
export async function deleteRole(name: string): Promise<void> {
  await request<void>(
    'DELETE',
    `/api/admin/roles/${encodeURIComponent(name)}`,
  );
}

// listRolePermissions maps GET /api/admin/roles/{name}/permissions →
// string[]. For built-in roles this returns the effective static matrix.
export async function listRolePermissions(name: string): Promise<string[]> {
  const resp = await request<RolePermissionsResponse>(
    'GET',
    `/api/admin/roles/${encodeURIComponent(name)}/permissions`,
  );
  return resp.permissions ?? [];
}

// setRolePermissions maps PUT /api/admin/roles/{name}/permissions → the
// updated permission list. Built-in roles return 409 BuiltinRoleProtected.
export async function setRolePermissions(
  name: string,
  permissions: string[],
): Promise<string[]> {
  const resp = await request<RolePermissionsResponse>(
    'PUT',
    `/api/admin/roles/${encodeURIComponent(name)}/permissions`,
    { permissions },
  );
  return resp.permissions ?? [];
}
