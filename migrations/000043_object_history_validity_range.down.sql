DROP INDEX IF EXISTS idx_object_history_valid_range;

ALTER TABLE object_history
    DROP COLUMN IF EXISTS valid_to,
    DROP COLUMN IF EXISTS valid_from;
