-- Tier 2.4: API keys for server-to-server (non-interactive) authentication.
--
-- Stored as SHA-256 BYTEA (key_hash, never the raw secret) plus a short
-- lookup-only prefix (key_prefix, the first 8 base32 chars of the raw key).
-- The auth middleware extracts the prefix from a "wvk_<prefix>_<random>"
-- bearer token, looks up the row by prefix, then constant-time compares the
-- SHA-256 of the full token against key_hash.
--
-- Each key is owned by a user. Authorization for the request inherits from
-- the owning user's roles via UserRepository.ListUserRoles. The optional
-- scopes column reserves space for future per-key permission narrowing.

CREATE TABLE api_keys (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key_hash     BYTEA NOT NULL,
    key_prefix   TEXT NOT NULL,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    scopes       TEXT[] NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ
);

-- Lookup index: only active (non-revoked) prefixes are unique. Revoked keys
-- can keep their original prefix without colliding with new ones.
CREATE UNIQUE INDEX idx_api_keys_prefix ON api_keys (key_prefix) WHERE revoked_at IS NULL;
CREATE INDEX idx_api_keys_user ON api_keys (user_id) WHERE revoked_at IS NULL;
