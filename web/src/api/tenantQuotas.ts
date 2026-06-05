import { request } from './client';

// Tenant quota + usage admin surface (US-277 / US-438). Mirrors the wire
// shapes emitted by pkg/tenants/handlers.go. The backend exposes full CRUD on
// per-tenant quotas plus a read-only per-tenant monthly usage view; the UI
// lists/edits quotas and reads usage, but does not expose the usage-write
// (POST /tenant-usage/{tenant}/{metric}) path.

// Quota mirrors pkg/tenants/quota.go Quota. maxObjects / maxStorage are int64
// counters, maxQPS is a float rate, burst is the token-bucket burst size.
export interface Quota {
  tenant: string;
  maxObjects: number;
  maxStorage: number;
  maxQPS: number;
  burst: number;
  description: string;
  createdAt: string;
  updatedAt: string;
}

// CreateQuotaRequest mirrors pkg/tenants/handlers.go createRequest — every
// field is required on POST.
export interface CreateQuotaRequest {
  tenant: string;
  maxObjects: number;
  maxStorage: number;
  maxQPS: number;
  burst: number;
  description: string;
}

// UpdateQuotaRequest mirrors pkg/tenants/handlers.go updateRequest — every
// field is optional (omitted fields are left unchanged via pointer PATCH
// semantics in QuotaUpdate).
export interface UpdateQuotaRequest {
  maxObjects?: number;
  maxStorage?: number;
  maxQPS?: number;
  burst?: number;
  description?: string;
}

// MonthlyUsage mirrors pkg/tenants/usage.go MonthlyUsage. percent is an
// integer 0-100 (clamped server-side); amount / cap are absolute counts for
// the metric in the current calendar month.
export interface MonthlyUsage {
  tenant: string;
  month: string;
  metric: string;
  amount: number;
  cap: number;
  percent: number;
}

interface QuotaListResponse {
  quotas: Quota[];
}

interface UsageListResponse {
  usage: MonthlyUsage[];
}

// listQuotas maps GET /api/admin/tenant-quotas → Quota[]. The server always
// emits a `quotas` array (possibly empty); coalesce to [] defensively.
export async function listQuotas(): Promise<Quota[]> {
  const resp = await request<QuotaListResponse>('GET', '/api/admin/tenant-quotas');
  return resp.quotas ?? [];
}

// getQuota maps GET /api/admin/tenant-quotas/{tenant} → Quota.
export async function getQuota(tenant: string): Promise<Quota> {
  return request<Quota>(
    'GET',
    `/api/admin/tenant-quotas/${encodeURIComponent(tenant)}`,
  );
}

// createQuota maps POST /api/admin/tenant-quotas → Quota.
export async function createQuota(body: CreateQuotaRequest): Promise<Quota> {
  return request<Quota>('POST', '/api/admin/tenant-quotas', body);
}

// updateQuota maps PUT /api/admin/tenant-quotas/{tenant} → the updated Quota.
export async function updateQuota(
  tenant: string,
  body: UpdateQuotaRequest,
): Promise<Quota> {
  return request<Quota>(
    'PUT',
    `/api/admin/tenant-quotas/${encodeURIComponent(tenant)}`,
    body,
  );
}

// deleteQuota maps DELETE /api/admin/tenant-quotas/{tenant} (204/200).
export async function deleteQuota(tenant: string): Promise<void> {
  await request<void>(
    'DELETE',
    `/api/admin/tenant-quotas/${encodeURIComponent(tenant)}`,
  );
}

// getUsage maps GET /api/admin/tenant-usage/{tenant} → MonthlyUsage[] — the
// current calendar month's per-metric usage for one tenant.
export async function getUsage(tenant: string): Promise<MonthlyUsage[]> {
  const resp = await request<UsageListResponse>(
    'GET',
    `/api/admin/tenant-usage/${encodeURIComponent(tenant)}`,
  );
  return resp.usage ?? [];
}
