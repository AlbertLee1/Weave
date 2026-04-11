-- Reverse US-019 object_history.source column.

DROP INDEX IF EXISTS idx_object_history_source;
ALTER TABLE object_history DROP COLUMN IF EXISTS source;
