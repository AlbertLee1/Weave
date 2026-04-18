DROP INDEX IF EXISTS idx_api_keys_rotates_at;

ALTER TABLE api_keys
    DROP COLUMN IF EXISTS successor_id,
    DROP COLUMN IF EXISTS rotates_at;
