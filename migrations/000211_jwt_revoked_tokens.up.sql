-- US-491: JWT access-token revocation blacklist.
--
-- Tracks JTIs (RFC 7519 JWT IDs) that the operator has explicitly revoked,
-- alongside the original `exp` claim so a background sweeper can prune rows
-- after natural expiration. Middleware does an indexed lookup on every
-- request; an in-memory TTL cache in front keeps the hot path under one
-- query per N seconds per JTI.

CREATE TABLE auth_revoked_tokens (
    jti        TEXT PRIMARY KEY,
    user_id    TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    reason     TEXT
);

-- Used by the periodic sweep that prunes naturally-expired entries from
-- the blacklist; the partial predicate lets the sweeper plan an index-only
-- scan on the rows that actually need attention.
CREATE INDEX idx_auth_revoked_tokens_expires ON auth_revoked_tokens(expires_at);
