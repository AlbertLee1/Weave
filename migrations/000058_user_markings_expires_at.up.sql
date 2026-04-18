-- US-260: time-limited marking grants.
--
-- Admins can issue a marking grant that automatically expires at a fixed
-- TIMESTAMPTZ (e.g. "alice holds PII for 30 days"). The column is NULLable
-- to preserve the permanent-grant default: rows where expires_at IS NULL
-- stay valid indefinitely, matching pre-US-260 behaviour.
--
-- Enforcement lives in the read path (GetUserMarkings, ListGrantsByUser)
-- via `AND (expires_at IS NULL OR expires_at > NOW())`. No background
-- sweep is required; expired grants simply stop surfacing and can be
-- cleaned up lazily by admin tooling if desired.

ALTER TABLE user_markings
    ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_user_markings_expires_at
    ON user_markings (expires_at)
    WHERE expires_at IS NOT NULL;
