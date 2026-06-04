import { request } from './client';

export interface Marking {
  name: string;
  displayName: string;
  description: string;
  color: string;
  createdAt?: string;
}

export interface MarkingGrant {
  userId: string;
  markingName: string;
  grantedAt: string;
  grantedBy: string;
  expiresAt?: string;
}

export interface GrantMarkingOptions {
  expiresAt?: string;
  expiresInDays?: number;
}

interface MarkingListResponse {
  markings: Marking[];
}

interface MarkingGrantsResponse {
  grants: MarkingGrant[];
}

export async function listMarkings(): Promise<Marking[]> {
  const resp = await request<MarkingListResponse>('GET', '/api/admin/markings');
  return resp.markings ?? [];
}

export async function listGrantsByMarking(name: string): Promise<MarkingGrant[]> {
  const resp = await request<MarkingGrantsResponse>(
    'GET',
    `/api/admin/markings/${encodeURIComponent(name)}/grants`,
  );
  return resp.grants ?? [];
}

export async function listGrantsByUser(userId: string): Promise<MarkingGrant[]> {
  const resp = await request<MarkingGrantsResponse>(
    'GET',
    `/api/admin/users/${encodeURIComponent(userId)}/markings`,
  );
  return resp.grants ?? [];
}

export async function grantMarking(
  userId: string,
  marking: string,
  options?: GrantMarkingOptions,
): Promise<MarkingGrant[]> {
  const body: Record<string, string | number> = { marking };
  if (options?.expiresInDays && options.expiresInDays > 0) {
    body.expiresInDays = options.expiresInDays;
  } else if (options?.expiresAt) {
    body.expiresAt = options.expiresAt;
  }
  const resp = await request<MarkingGrantsResponse>(
    'POST',
    `/api/admin/users/${encodeURIComponent(userId)}/markings`,
    body,
  );
  return resp.grants ?? [];
}

export async function revokeMarking(userId: string, marking: string): Promise<void> {
  await request<void>(
    'DELETE',
    `/api/admin/users/${encodeURIComponent(userId)}/markings/${encodeURIComponent(marking)}`,
  );
}
