-- Reverse Tier 2.4 api_keys table.
DROP INDEX IF EXISTS idx_api_keys_user;
DROP INDEX IF EXISTS idx_api_keys_prefix;
DROP TABLE IF EXISTS api_keys;
