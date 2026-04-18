-- US-254: active-session inventory so users can review which devices are
-- logged in and revoke any they do not recognise from
-- GET/DELETE /api/auth/sessions.
--
-- One row per successful login (/api/auth/login, /api/auth/mfa/verify,
-- /api/auth/oidc/callback, /api/auth/saml/acs). Refresh-token rotation keeps
-- the session row stable — only the refresh_tokens row changes on each
-- /api/auth/refresh call — so the session ID surfaced to the user does not
-- churn across rotations. refresh_token_id points at the CURRENT row in
-- refresh_tokens so DELETE /api/auth/sessions/{id} can revoke the rotation
-- chain in one round trip.
--
-- ON DELETE SET NULL on the refresh_token_id FK keeps the session visible
-- in the list view after rotation cleanup re-inserts a fresh chain root;
-- the session row stays until the user explicitly revokes it or login mints
-- a new one.

CREATE TABLE IF NOT EXISTS sessions (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    refresh_token_id  UUID REFERENCES refresh_tokens(id) ON DELETE SET NULL,
    ip                TEXT,
    user_agent        TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_sessions_user      ON sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_last_seen ON sessions (user_id, last_seen DESC);
