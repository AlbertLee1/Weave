import { request } from './client';

export interface ApplicationSummary {
  id: string;
  name: string;
  description: string;
  clientId: string;
  redirectUris: string[];
  scopes: string[];
  createdBy: string;
  createdAt: string;
  updatedAt: string;
}

export interface RouteStat {
  endpoint: string;
  method: string;
  count: number;
  errors: number;
  p95Ms: number;
}

export interface UsageSummary {
  window: string;
  since: string;
  until: string;
  total: number;
  errors: number;
  byStatus: Record<string, number>;
  byMethod: Record<string, number>;
  topRoutes: RouteStat[];
  p50Ms: number;
  p95Ms: number;
  p99Ms: number;
}

export interface UsageResponse {
  applicationId: string;
  clientId: string;
  windows: UsageSummary[];
}

export async function listApplications(): Promise<ApplicationSummary[]> {
  const resp = await request<{ applications: ApplicationSummary[] }>(
    'GET',
    '/api/v2/developer/applications',
  );
  return resp.applications ?? [];
}

export function getApplicationUsage(applicationId: string): Promise<UsageResponse> {
  return request<UsageResponse>(
    'GET',
    `/api/v2/developer/applications/${encodeURIComponent(applicationId)}/usage`,
  );
}
