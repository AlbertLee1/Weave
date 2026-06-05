import { request } from './client';

// RotateSigningKeysResponse mirrors the body returned by
// POST /api/admin/auth/keys/rotate (pkg/auth key-rotation handler). The
// backend generates a fresh RSA key pair, appends it to the keyring, and
// returns the new active key id plus every kid currently in the ring. There
// is deliberately NO GET endpoint for the keyring, so this response is the
// only way the UI ever learns the current key ids.
export interface RotateSigningKeysResponse {
  // activeKeyId is the kid the server now signs new tokens with.
  activeKeyId: string;
  // keyIds is every kid in the ring (old kids are retained so previously
  // issued, still-valid tokens keep verifying).
  keyIds: string[];
  // rotatedAt is the RFC3339 instant the rotation took effect.
  rotatedAt: string;
}

// RevokeTokenRequest is the optional body for POST
// /api/auth/tokens/{jti}/revoke. The jti is carried in the path; every body
// field is optional. expiresAt, when supplied, must be RFC3339 — it caps how
// long the revocation entry is retained (typically the token's own expiry).
export interface RevokeTokenRequest {
  userId?: string;
  reason?: string;
  expiresAt?: string;
}

// RevokeTokenResponse mirrors the body returned on a successful revoke.
export interface RevokeTokenResponse {
  jti: string;
  // revokedAt is the RFC3339 instant the token was blacklisted.
  revokedAt: string;
}

// rotateSigningKeys triggers a JWT signing-key rotation. Resolves with the new
// active kid + the full keyring; rejects (ApiRequestError) on 5xx. The endpoint
// takes no body.
export function rotateSigningKeys(): Promise<RotateSigningKeysResponse> {
  return request<RotateSigningKeysResponse>(
    'POST',
    '/api/admin/auth/keys/rotate',
  );
}

// revokeToken blacklists a single token by its jti. The jti is path-encoded;
// the optional userId/reason/expiresAt travel in the body. Resolves with the
// jti + revokedAt; rejects (ApiRequestError) on 400 (empty jti) / 5xx.
export function revokeToken(
  jti: string,
  body: RevokeTokenRequest = {},
): Promise<RevokeTokenResponse> {
  return request<RevokeTokenResponse>(
    'POST',
    `/api/auth/tokens/${encodeURIComponent(jti)}/revoke`,
    body,
  );
}
