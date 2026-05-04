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
