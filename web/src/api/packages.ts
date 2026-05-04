import { request } from './client';

// US-413 / US-454: shape of one row in the marketplace catalog. Mirrors
// `oms.InstalledPackage` over the wire — `manifest` is the opaque
// manifest.json payload preserved verbatim so the marketplace UI can
// surface dependency / author / license fields without an extra round
// trip.
export interface InstalledPackage {
  id: number;
  name: string;
  version: string;
  ontology: string;
  manifest: PackageManifest | null;
  migrations: string[];
  enabled: boolean;
  installedBy?: string;
  installedAt: string;
  updatedAt: string;
}

// PackageManifest is the parsed shape of `manifest.json` inside a
// .weavepkg archive. Optional fields are absent when the archive author
// did not declare them; the marketplace UI degrades gracefully.
export interface PackageManifest {
  name?: string;
  version?: string;
  author?: string;
  license?: string;
  description?: string;
  minWeaveVersion?: string;
  dependencies?: Record<string, string>;
  contents?: Record<string, unknown>;
}

export interface InstalledPackagesResponse {
  data: InstalledPackage[];
}

export function listInstalledPackages(): Promise<InstalledPackagesResponse> {
  return request<InstalledPackagesResponse>('GET', '/api/v2/pkg');
}

export function getInstalledPackage(name: string): Promise<InstalledPackage> {
  return request<InstalledPackage>(
    'GET',
    `/api/v2/pkg/${encodeURIComponent(name)}`,
  );
}

export interface SetPackageEnabledResponse {
  name: string;
  enabled: boolean;
}

export function setInstalledPackageEnabled(
  name: string,
  enabled: boolean,
): Promise<SetPackageEnabledResponse> {
  return request<SetPackageEnabledResponse>(
    'POST',
    `/api/v2/pkg/${encodeURIComponent(name)}/enabled`,
    { enabled },
  );
}

export function deleteInstalledPackage(name: string): Promise<void> {
  return request<void>('DELETE', `/api/v2/pkg/${encodeURIComponent(name)}`);
}

// US-414: Built-in example package catalog. Each row is one of the
// example packages embedded into the server binary
// (Northwind / Chinook / IoT-demo). The Marketplace UI's "Built-in"
// section consumes this list to offer one-click install.
export interface BuiltinPackageMetadata {
  slug: string;
  name: string;
  version: string;
  ontologyApiName: string;
  author?: string;
  license?: string;
  description?: string;
  minWeaveVersion?: string;
  dependencies?: Array<{ name: string; version: string }>;
  objectTypeCount: number;
  linkTypeCount: number;
  actionTypeCount: number;
  functionCount: number;
  migrationCount: number;
}

export interface BuiltinPackagesResponse {
  data: BuiltinPackageMetadata[];
}

export function listBuiltinPackages(): Promise<BuiltinPackagesResponse> {
  return request<BuiltinPackagesResponse>('GET', '/api/v2/pkg/builtin');
}

export interface InstallBuiltinPackageResponse {
  name: string;
  version: string;
  ontology: string;
  imported: Record<string, number>;
  migrationsRan: number;
  migrationsTotal: number;
  message: string;
}

export type BuiltinInstallConflictMode = 'fail' | 'overwrite' | 'skip';

export function installBuiltinPackage(
  slug: string,
  onConflict?: BuiltinInstallConflictMode,
): Promise<InstallBuiltinPackageResponse> {
  const body = onConflict ? { onConflict } : {};
  return request<InstallBuiltinPackageResponse>(
    'POST',
    `/api/v2/pkg/builtin/${encodeURIComponent(slug)}/install`,
    body,
  );
}
