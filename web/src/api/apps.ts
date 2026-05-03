import { request } from './client';

// LayoutNode mirrors pkg/apps/layout.go::ValidateLayout. The wire shape
// is recursive — `row` carries a children array of `col`s, `col` has an
// integer width (1..12) plus a single child node, `component` is a leaf
// identified by `componentType` plus an opaque `props` bag.
export type LayoutNode = LayoutRow | LayoutCol | LayoutComponent;

export interface LayoutRow {
  type: 'row';
  children: LayoutCol[];
}

export interface LayoutCol {
  type: 'col';
  width: number;
  child: LayoutNode;
}

export interface LayoutComponent {
  type: 'component';
  componentType: string;
  props?: Record<string, unknown>;
}

export interface App {
  rid: string;
  name: string;
  ownerId: string;
  layoutJson: LayoutNode;
  version: number;
  createdAt: string;
  updatedAt: string;
}

export interface AppVersion {
  appRid: string;
  version: number;
  name: string;
  layoutJson: LayoutNode;
  createdAt: string;
  createdBy: string;
}

export interface ListAppsResponse {
  apps: App[];
}

export interface ListAppVersionsResponse {
  versions: AppVersion[];
}

export interface CreateAppInput {
  name: string;
  layoutJson: LayoutNode;
}

export interface UpdateAppInput {
  rid: string;
  name?: string;
  layoutJson?: LayoutNode;
}

export function listApps(): Promise<ListAppsResponse> {
  return request<ListAppsResponse>('GET', '/api/v2/apps');
}

export function getApp(rid: string): Promise<App> {
  return request<App>('GET', `/api/v2/apps/${encodeURIComponent(rid)}`);
}

export function createApp(input: CreateAppInput): Promise<App> {
  return request<App>('POST', '/api/v2/apps', input);
}

export function updateApp(input: UpdateAppInput): Promise<App> {
  const { rid, ...body } = input;
  return request<App>('PUT', `/api/v2/apps/${encodeURIComponent(rid)}`, body);
}

export function deleteApp(rid: string): Promise<void> {
  return request<void>('DELETE', `/api/v2/apps/${encodeURIComponent(rid)}`);
}

export function listAppVersions(rid: string): Promise<ListAppVersionsResponse> {
  return request<ListAppVersionsResponse>(
    'GET',
    `/api/v2/apps/${encodeURIComponent(rid)}/versions`,
  );
}
