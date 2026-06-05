import { request } from './client';

// GroupResponse mirrors the backend's auth.GroupResponse wire shape
// (pkg/auth/group_handlers.go). A Group is a named RBAC bucket of users;
// members are tracked separately and addressed by opaque userId strings.
export interface GroupResponse {
  id: string;
  name: string;
  description: string;
  createdAt: string;
  updatedAt: string;
}

// Group is the in-app alias for a GroupResponse. The two are byte-for-byte
// identical today; the alias exists so call sites can read as "a Group"
// while leaving room for a future client-side projection.
export type Group = GroupResponse;

export interface CreateGroupRequest {
  name: string;
  description?: string;
}

export interface UpdateGroupRequest {
  name?: string;
  description?: string;
}

interface GroupListResponse {
  groups: GroupResponse[] | null;
}

interface GroupMembersResponse {
  members: string[] | null;
}

// listGroups returns every group definition. The backend wraps the array in
// `{ groups }`; a missing/null array degrades to [].
export async function listGroups(): Promise<Group[]> {
  const resp = await request<GroupListResponse>('GET', '/api/admin/groups');
  return resp.groups ?? [];
}

export async function getGroup(id: string): Promise<Group> {
  return request<Group>('GET', `/api/admin/groups/${encodeURIComponent(id)}`);
}

export async function createGroup(body: CreateGroupRequest): Promise<Group> {
  return request<Group>('POST', '/api/admin/groups', body);
}

export async function updateGroup(
  id: string,
  body: UpdateGroupRequest,
): Promise<Group> {
  return request<Group>(
    'PATCH',
    `/api/admin/groups/${encodeURIComponent(id)}`,
    body,
  );
}

export async function deleteGroup(id: string): Promise<void> {
  await request<void>('DELETE', `/api/admin/groups/${encodeURIComponent(id)}`);
}

// listGroupMembers returns the userId strings belonging to a group. The
// backend wraps the array in `{ members }`; a missing/null array degrades
// to [].
export async function listGroupMembers(id: string): Promise<string[]> {
  const resp = await request<GroupMembersResponse>(
    'GET',
    `/api/admin/groups/${encodeURIComponent(id)}/members`,
  );
  return resp.members ?? [];
}

export async function addGroupMember(
  id: string,
  userId: string,
): Promise<void> {
  await request<void>(
    'POST',
    `/api/admin/groups/${encodeURIComponent(id)}/members`,
    { userId },
  );
}

export async function removeGroupMember(
  id: string,
  userId: string,
): Promise<void> {
  await request<void>(
    'DELETE',
    `/api/admin/groups/${encodeURIComponent(id)}/members/${encodeURIComponent(userId)}`,
  );
}
