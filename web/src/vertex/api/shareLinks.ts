// VTX-013 — Vertex graph quick share-link API client.
//
// Wraps the three graphsvc share-link endpoints
// (pkg/vertex/graphsvc/handler.go):
//
//   POST   /api/vertex/v1/graphs/{rid}/share-links   -> 201 CreatedShareLink
//   GET    /api/vertex/v1/graphs/{rid}/share-links    -> 200 {shareLinks:[…]}
//   DELETE /api/vertex/v1/share-links/{token}         -> 204
//
// Security note (mirrors the server): the FULL opaque token is only ever
// returned at create time (one-time disclosure). The list response carries
// `tokenSuffix` (last 8 chars) only — never the full token — so an owner
// can disambiguate links in the UI without being able to reconstruct a
// live share URL from list output. Revoke therefore operates on the full
// token the caller held onto at create time, not on a list row.

import { request } from '../../api/client';

/** 201 response body from createShareLink — the only place the full token is disclosed. */
export interface CreatedShareLink {
  /** Full opaque share token. Disclosed once, at create time. */
  token: string;
  graphRid: string;
  createdBy: string;
  /** RFC3339 timestamp. */
  createdAt: string;
}

/** A row in the listShareLinks response. The full token is intentionally absent. */
export interface ShareLinkSummary {
  /** Last 8 chars of the token — identification only, never the full token. */
  tokenSuffix: string;
  graphRid: string;
  createdBy: string;
  createdAt: string;
  revoked: boolean;
  /** Present only when the link carries an expiry. RFC3339. */
  expiresAt?: string;
  /** Present only when the link has been revoked. RFC3339. */
  revokedAt?: string;
}

interface ListShareLinksResponse {
  shareLinks: ShareLinkSummary[];
}

/**
 * createShareLink mints a new share link for the graph and returns the
 * full token (one-time disclosure). Owner-only on the server (403 for
 * non-owners). Surface {@link shareLinkUrl} on the returned token to build
 * the recipient-facing URL.
 */
export function createShareLink(rid: string): Promise<CreatedShareLink> {
  return request<CreatedShareLink>(
    'POST',
    `/api/vertex/v1/graphs/${encodeURIComponent(rid)}/share-links`,
  );
}

/**
 * listShareLinks returns every share link minted on the graph (including
 * revoked ones) so the owner's manage surface can render. Each row carries
 * `tokenSuffix` only — never the full token.
 */
export async function listShareLinks(rid: string): Promise<ShareLinkSummary[]> {
  const res = await request<ListShareLinksResponse>(
    'GET',
    `/api/vertex/v1/graphs/${encodeURIComponent(rid)}/share-links`,
  );
  return res?.shareLinks ?? [];
}

/**
 * revokeShareLink marks a share link revoked by its full token. Owner-only
 * on the server; already-revoked links return 204 idempotently. The 204 has
 * no body, so this resolves to void.
 */
export function revokeShareLink(token: string): Promise<void> {
  return request<void>(
    'DELETE',
    `/api/vertex/v1/share-links/${encodeURIComponent(token)}`,
  );
}

/**
 * shareLinkUrl builds the recipient-facing URL for a freshly minted share
 * token. Points at the masked read endpoint the server exposes
 * (getViaShareLink): GET /api/vertex/v1/share-links/{token}/graph.
 *
 * Returns an absolute URL when a window origin is available (browser),
 * otherwise the path-only form (tests / SSR) so the token is always
 * copy-pasteable.
 */
export function shareLinkUrl(token: string): string {
  const path = `/api/vertex/v1/share-links/${encodeURIComponent(token)}/graph`;
  if (typeof window !== 'undefined' && window.location?.origin) {
    return `${window.location.origin}${path}`;
  }
  return path;
}
