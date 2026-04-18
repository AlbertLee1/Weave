DROP TABLE IF EXISTS ldap_sync_runs;

DROP INDEX IF EXISTS idx_groups_ldap_dn;
ALTER TABLE groups
    DROP COLUMN IF EXISTS last_synced_at,
    DROP COLUMN IF EXISTS ldap_dn;

DROP INDEX IF EXISTS idx_users_disabled_at;
DROP INDEX IF EXISTS idx_users_ldap_dn;
ALTER TABLE users
    DROP COLUMN IF EXISTS last_synced_at,
    DROP COLUMN IF EXISTS ldap_dn,
    DROP COLUMN IF EXISTS disabled_at;
