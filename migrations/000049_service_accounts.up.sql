-- US-249: Service Accounts — non-interactive principals for CI/CD and
-- machine-to-machine API access.
--
-- Each service account is owned by a human user (owner_user_id) and carries
-- an independent list of scopes plus an optional absolute expiry. Soft delete
-- lives in disabled_at so an audit trail survives deletion; the unique
-- partial index on name limits the uniqueness invariant to active rows so an
-- operator can recreate a service account under the same name after
-- disabling the prior one.
--
-- Authorization keys off the Weave RBAC layer via owner_user_id (the
-- handler reads the owning user's roles). API keys minted against this
-- principal live in the existing api_keys table with user_id set to the
-- synthetic service-account user id; this migration does NOT extend
-- api_keys, the join is handled at the request path.

CREATE TABLE service_accounts (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name           TEXT NOT NULL,
    description    TEXT NOT NULL DEFAULT '',
    owner_user_id  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    scopes         TEXT[] NOT NULL DEFAULT '{}',
    expires_at     TIMESTAMPTZ,
    disabled_at    TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Active-name uniqueness: revoked/disabled service accounts don't block
-- recreation under the same name.
CREATE UNIQUE INDEX idx_service_accounts_name_active ON service_accounts (name) WHERE disabled_at IS NULL;
CREATE INDEX idx_service_accounts_owner ON service_accounts (owner_user_id) WHERE disabled_at IS NULL;
