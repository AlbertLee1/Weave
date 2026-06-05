import { request } from './client';

// ServiceAccountResponse is the wire shape returned by every CRUD endpoint
// under /api/admin/service-accounts (see pkg/auth/service_account_handlers.go).
// expiresAt / disabledAt are nullable: a non-null disabledAt marks the account
// as soft-disabled. Although List only returns active accounts today, the
// field is modelled so a single-account Get can render the Disabled state.
export interface ServiceAccountResponse {
  id: string;
  name: string;
  description: string;
  ownerUserId: string;
  scopes: string[];
  expiresAt?: string | null;
  disabledAt?: string | null;
  createdAt: string;
  updatedAt: string;
}

// ServiceAccount is the client-facing alias. The wire and client shapes are
// identical today, but the alias keeps call sites decoupled from the raw
// response name and gives us a seam if the UI ever derives extra fields.
export type ServiceAccount = ServiceAccountResponse;

interface ServiceAccountListResponse {
  serviceAccounts: ServiceAccountResponse[];
}

// CreateServiceAccountRequest is the POST body. name is required; the rest are
// optional. scopes defaults to [] server-side when omitted.
export interface CreateServiceAccountRequest {
  name: string;
  description?: string;
  scopes?: string[];
  expiresAt?: string;
}

// UpdateServiceAccountRequest is the PATCH body. Every field is optional —
// omitting a field preserves the stored value. An empty-string expiresAt
// clears the expiry (matches the handler's pointer-field semantics).
export interface UpdateServiceAccountRequest {
  description?: string;
  scopes?: string[];
  expiresAt?: string;
}

export async function listServiceAccounts(): Promise<ServiceAccount[]> {
  const resp = await request<ServiceAccountListResponse>(
    'GET',
    '/api/admin/service-accounts',
  );
  return resp.serviceAccounts ?? [];
}

export async function getServiceAccount(id: string): Promise<ServiceAccount> {
  return request<ServiceAccount>(
    'GET',
    `/api/admin/service-accounts/${encodeURIComponent(id)}`,
  );
}

export async function createServiceAccount(
  body: CreateServiceAccountRequest,
): Promise<ServiceAccount> {
  return request<ServiceAccount>('POST', '/api/admin/service-accounts', body);
}

export async function updateServiceAccount(
  id: string,
  body: UpdateServiceAccountRequest,
): Promise<ServiceAccount> {
  return request<ServiceAccount>(
    'PATCH',
    `/api/admin/service-accounts/${encodeURIComponent(id)}`,
    body,
  );
}

export async function deleteServiceAccount(id: string): Promise<void> {
  await request<void>(
    'DELETE',
    `/api/admin/service-accounts/${encodeURIComponent(id)}`,
  );
}
