DROP INDEX IF EXISTS idx_user_markings_expires_at;

ALTER TABLE user_markings DROP COLUMN IF EXISTS expires_at;
