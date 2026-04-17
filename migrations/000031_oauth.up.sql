-- Developer Console: OAuth 2.0 authorization_code flow (US-142).
--
-- Two tables back the flow:
--
--   oauth_authorization_codes — short-lived single-use codes returned by the
--     /oauth/authorize approval. The code is handed to the caller by redirect
--     and is never sent to the client in a response body; the DB row keeps
--     the PKCE challenge so /oauth/token can verify the matching verifier
--     before issuing an access token. Rows are deleted (or consumed_at is
--     stamped) on first use; expiry is typically 10 minutes.
--
--   oauth_tokens — access + refresh tokens issued by /oauth/token. The raw
--     token string is NEVER persisted; only its SHA-256 hash and a short
--     token_prefix land here. token_prefix is used as the O(1) lookup index
--     by the auth middleware so a full-table scan is never required on an
--     incoming bearer.
--
-- Both tables soft-reference applications.client_id (not a FK to the UUID
-- surrogate) so that a hard DELETE of the application row leaves the
-- existing tokens as orphans the middleware will reject with "unknown
-- client_id" — the same effect as revoking them, without a cascading delete.

CREATE TABLE IF NOT EXISTS oauth_authorization_codes (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code                   TEXT NOT NULL UNIQUE,
    client_id              TEXT NOT NULL,
    user_id                TEXT NOT NULL,
    redirect_uri           TEXT NOT NULL,
    scopes                 JSONB NOT NULL DEFAULT '[]'::jsonb,
    code_challenge         TEXT NOT NULL,
    code_challenge_method  TEXT NOT NULL DEFAULT 'S256',
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at             TIMESTAMPTZ NOT NULL,
    consumed_at            TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_oauth_authorization_codes_expires_at
    ON oauth_authorization_codes (expires_at);

CREATE TABLE IF NOT EXISTS oauth_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash   BYTEA NOT NULL,
    token_prefix TEXT NOT NULL,
    token_type   TEXT NOT NULL CHECK (token_type IN ('access', 'refresh')),
    client_id    TEXT NOT NULL,
    user_id      TEXT,
    scopes       JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ NOT NULL,
    revoked_at   TIMESTAMPTZ,
    UNIQUE (token_prefix, token_hash)
);

CREATE INDEX IF NOT EXISTS idx_oauth_tokens_prefix ON oauth_tokens (token_prefix);
CREATE INDEX IF NOT EXISTS idx_oauth_tokens_client_id ON oauth_tokens (client_id);
