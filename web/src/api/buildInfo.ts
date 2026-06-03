import { request } from './client';

// BuildInfo mirrors pkg/buildinfo handler.go BuildInfoResponse — the
// running server's version, git commit, Go toolchain, and build time.
export interface BuildInfo {
  version: string;
  commit: string;
  goVersion: string;
  buildTime: string;
}

// Feature mirrors pkg/buildinfo features.go — one toggle-able server
// capability with its enabled state and an optional reason/description.
export interface Feature {
  name: string;
  enabled: boolean;
  description?: string;
  reason?: string;
}

export interface FeaturesResponse {
  features: Feature[];
}

export function getBuildInfo(): Promise<BuildInfo> {
  return request<BuildInfo>('GET', '/api/v2/build-info');
}

export function getBuildFeatures(): Promise<FeaturesResponse> {
  return request<FeaturesResponse>('GET', '/api/v2/build-info/features');
}
