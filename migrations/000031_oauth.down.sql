DROP INDEX IF EXISTS idx_oauth_tokens_client_id;
DROP INDEX IF EXISTS idx_oauth_tokens_prefix;
DROP TABLE IF EXISTS oauth_tokens;
DROP INDEX IF EXISTS idx_oauth_authorization_codes_expires_at;
DROP TABLE IF EXISTS oauth_authorization_codes;
