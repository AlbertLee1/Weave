-- JWT Phase 3: refresh_tokens table for opaque rotating refresh tokens.
-- See .omc/scientist/reports/20260406_jwt_auth_design.md (FINDING:JWT_3, JWT_6).
--
-- Refresh tokens are opaque random strings (NOT JWTs) stored as SHA-256 hex
-- hashes. They are single-use: every successful refresh inserts a new row and
-- marks the previous one revoked, with parent_id forming a rotation chain so
-- that reuse can revoke the entire family.

CREATE TABLE refresh_tokens (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash         TEXT NOT NULL UNIQUE,
    issued_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at         TIMESTAMPTZ NOT NULL,
    last_used_at       TIMESTAMPTZ,
    revoked_at         TIMESTAMPTZ,
    revocation_reason  TEXT,
    user_agent         TEXT,
    ip                 TEXT,
    parent_id          UUID REFERENCES refresh_tokens(id) ON DELETE SET NULL
);

CREATE INDEX idx_refresh_tokens_user ON refresh_tokens(user_id);
CREATE INDEX idx_refresh_tokens_hash ON refresh_tokens(token_hash);
CREATE INDEX idx_refresh_tokens_expires ON refresh_tokens(expires_at) WHERE revoked_at IS NULL;
