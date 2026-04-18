DROP INDEX IF EXISTS idx_users_mfa_enabled;

ALTER TABLE users
    DROP COLUMN IF EXISTS mfa_enabled,
    DROP COLUMN IF EXISTS mfa_secret;
