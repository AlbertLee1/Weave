-- US-252: LDAP/AD directory sync.
--
-- Adds two columns to support periodic synchronisation of users + groups
-- from an external LDAP / Active Directory directory:
--
--   users.disabled_at        soft-delete tombstone. Set when the upstream
--                            directory no longer returns a user that was
--                            previously synced. NULL means "active".
--   users.ldap_dn            distinguished name carried back from the
--                            directory; used as the stable correlation key
--                            across syncs (email can change in AD).
--   users.last_synced_at     last successful re-import time. Allows the
--                            disable-orphans pass to skip never-synced
--                            (locally provisioned) accounts.
--   groups.ldap_dn           same idea for groups; allows recreating a
--                            group with the same name after a rename.
--   groups.last_synced_at    same idea as the user-side column.
--
-- A separate `ldap_sync_runs` table records every attempt with start/end
-- timestamps and counters so operators can see when the last sync ran and
-- how many users/groups it affected. Bounded to the most recent 100 rows
-- by an opportunistic prune in the recorder.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS disabled_at    TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS ldap_dn        TEXT,
    ADD COLUMN IF NOT EXISTS last_synced_at TIMESTAMPTZ;

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_ldap_dn ON users (ldap_dn) WHERE ldap_dn IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_users_disabled_at ON users (disabled_at) WHERE disabled_at IS NOT NULL;

ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS ldap_dn        TEXT,
    ADD COLUMN IF NOT EXISTS last_synced_at TIMESTAMPTZ;

CREATE UNIQUE INDEX IF NOT EXISTS idx_groups_ldap_dn ON groups (ldap_dn) WHERE ldap_dn IS NOT NULL;

CREATE TABLE IF NOT EXISTS ldap_sync_runs (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    started_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at       TIMESTAMPTZ,
    users_seen        INT NOT NULL DEFAULT 0,
    users_created     INT NOT NULL DEFAULT 0,
    users_updated     INT NOT NULL DEFAULT 0,
    users_disabled    INT NOT NULL DEFAULT 0,
    groups_seen       INT NOT NULL DEFAULT 0,
    groups_created    INT NOT NULL DEFAULT 0,
    groups_updated    INT NOT NULL DEFAULT 0,
    memberships_added INT NOT NULL DEFAULT 0,
    error_message     TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_ldap_sync_runs_started_at ON ldap_sync_runs (started_at DESC);
