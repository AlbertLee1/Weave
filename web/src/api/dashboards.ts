import { request } from './client';

// DashboardDefinition is the front-end-owned envelope persisted on the
// backend as opaque JSONB. The widgets array carries whatever the
// editor's discriminated-union widget shape is at the time of save.
export interface DashboardDefinition {
  widgets: unknown[];
}

export interface Dashboard {
  id: string;
  name: string;
  createdBy: string;
  isPublic: boolean;
  definition: DashboardDefinition;
  createdAt: string;
  updatedAt: string;
}

export interface ListDashboardsResponse {
  dashboards: Dashboard[];
}

export function listDashboards(): Promise<ListDashboardsResponse> {
  return request<ListDashboardsResponse>('GET', '/api/v2/dashboards');
}

export function getDashboard(id: string): Promise<Dashboard> {
  return request<Dashboard>(
    'GET',
    `/api/v2/dashboards/${encodeURIComponent(id)}`,
  );
}

export interface CreateDashboardInput {
  name: string;
  definition: DashboardDefinition;
  isPublic?: boolean;
}

export function createDashboard(
  input: CreateDashboardInput,
): Promise<Dashboard> {
  return request<Dashboard>('POST', '/api/v2/dashboards', input);
}

export interface UpdateDashboardInput {
  id: string;
  name?: string;
  definition?: DashboardDefinition;
  isPublic?: boolean;
}

export function updateDashboard(
  input: UpdateDashboardInput,
): Promise<Dashboard> {
  const { id, ...body } = input;
  return request<Dashboard>(
    'PUT',
    `/api/v2/dashboards/${encodeURIComponent(id)}`,
    body,
  );
}

export function deleteDashboard(id: string): Promise<void> {
  return request<void>(
    'DELETE',
    `/api/v2/dashboards/${encodeURIComponent(id)}`,
  );
}
