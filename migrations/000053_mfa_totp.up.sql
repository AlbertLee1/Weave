-- US-253: TOTP-based multi-factor authentication.
--
-- Adds two columns to users so the password-login flow can detect MFA-enabled
-- accounts and verify a 6-digit TOTP code as a second factor:
--
--   users.mfa_secret    base32-encoded TOTP shared secret. Persisted on
--                       /api/auth/mfa/setup; cleared on /api/auth/mfa/disable.
--                       Existence does NOT mean MFA is enforced — see
--                       mfa_enabled below.
--   users.mfa_enabled   boolean flag flipped on by /api/auth/mfa/enable after
--                       the user proves possession of the secret with a valid
--                       code. While true the login handler issues a challenge
--                       (HTTP 202) instead of an access token; the SPA must
--                       call /api/auth/mfa/verify with the code to complete
--                       login.
--
-- A partial index on mfa_enabled = true keeps "is anyone enrolled?" lookups
-- cheap without a full table scan.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS mfa_secret  TEXT,
    ADD COLUMN IF NOT EXISTS mfa_enabled BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_users_mfa_enabled ON users (mfa_enabled) WHERE mfa_enabled = TRUE;
