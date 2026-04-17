-- Developer Console: OAuth application registrations (US-141).
--
-- An application is a third-party OAuth 2.0 client: it owns a client_id and
-- a client_secret (stored only as a SHA-256 digest) that the future OAuth
-- endpoints (US-142) will issue access tokens against. Each row is owned by
-- the user that registered it, so callers can list/revoke their own apps.
--
-- client_secret is NEVER persisted; only its SHA-256 hash is. The secret is
-- returned in the response body EXACTLY once at creation time.

CREATE TABLE IF NOT EXISTS applications (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                TEXT NOT NULL,
    description         TEXT NOT NULL DEFAULT '',
    client_id           TEXT NOT NULL UNIQUE,
    client_secret_hash  BYTEA NOT NULL,
    redirect_uris       TEXT[] NOT NULL DEFAULT '{}',
    scopes              JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_by          TEXT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_applications_created_by ON applications (created_by);
