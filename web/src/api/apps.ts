import { request } from './client';

// LayoutNode mirrors pkg/apps/layout.go::ValidateLayout. The wire shape
// is recursive — `row` carries a children array of `col`s, `col` has an
// integer width (1..12) plus a single child node, `component` is a leaf
// identified by `componentType` plus an opaque `props` bag.
//
// US-393 extension: the root row may carry a `variables` array (App-level
// runtime state declarations) and components may carry an `events` map
// (named handlers — currently `onClick`). Both fields are opaque to the
// Go validator (extra row/component object keys are ignored) so no
// backend schema change is needed.
export type LayoutNode = LayoutRow | LayoutCol | LayoutComponent;

export interface LayoutRow {
  type: 'row';
  children: LayoutCol[];
  variables?: AppVariable[];
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
  events?: AppEventMap;
}

export type AppVariableType = 'string' | 'number' | 'boolean';

export interface AppVariable {
  name: string;
  type: AppVariableType;
  default: string;
}

// AppEventMap keys are event names (currently just `onClick`). Each
// entry is a discriminated union of action handlers.
export interface AppEventMap {
  onClick?: AppEvent;
}

export type AppEvent =
  | { kind: 'setVariable'; name: string; value: string }
  | { kind: 'runAction'; actionType: string; params?: Record<string, string> }
  | { kind: 'navigate'; to: string };

export interface App {
  rid: string;
  name: string;
  ownerId: string;
  layoutJson: LayoutNode;
  version: number;
  createdAt: string;
  updatedAt: string;
  publishedVersion?: number;
  publishedAt?: string;
  publishedBy?: string;
}

// PublishedAppView is the read-only viewer-facing snapshot returned by
// GET /api/v2/apps/{rid}/view (US-396). Any authenticated user can
// fetch it once an owner has published the App; the layout is pinned
// to the version recorded at publish time.
export interface PublishedAppView {
  rid: string;
  name: string;
  ownerId: string;
  publishedVersion: number;
  publishedAt: string;
  publishedBy: string;
  layoutJson: LayoutNode;
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

// publishApp pins the App's current version as the read-only published
// snapshot. Owner-only on the server.
export function publishApp(rid: string): Promise<PublishedAppView> {
  return request<PublishedAppView>(
    'POST',
    `/api/v2/apps/${encodeURIComponent(rid)}/publish`,
  );
}

// unpublishApp clears the publish pin. Owner-only on the server.
export function unpublishApp(rid: string): Promise<void> {
  return request<void>(
    'POST',
    `/api/v2/apps/${encodeURIComponent(rid)}/unpublish`,
  );
}

// viewApp fetches the published snapshot. Accessible to any
// authenticated viewer.
export function viewApp(rid: string): Promise<PublishedAppView> {
  return request<PublishedAppView>(
    'GET',
    `/api/v2/apps/${encodeURIComponent(rid)}/view`,
  );
}

// rollbackApp restores the App's Name + LayoutJSON from a historical
// version, bumping the live Version and recording a fresh history row.
// Owner-only. Returns the post-rollback live App row.
export function rollbackApp(rid: string, version: number): Promise<App> {
  return request<App>(
    'POST',
    `/api/v2/apps/${encodeURIComponent(rid)}/versions/${encodeURIComponent(
      String(version),
    )}/rollback`,
  );
}
