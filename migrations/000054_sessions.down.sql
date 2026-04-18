-- US-254 rollback: drop the sessions table. The refresh_tokens table is
-- untouched so existing refresh chains continue to work under the
-- pre-US-254 /api/auth/login flow.

DROP INDEX IF EXISTS idx_sessions_last_seen;
DROP INDEX IF EXISTS idx_sessions_user;
DROP TABLE IF EXISTS sessions;
