DROP INDEX IF EXISTS idx_object_set_snapshots_definition_hash;
ALTER TABLE object_set_snapshots
    DROP COLUMN IF EXISTS is_immutable,
    DROP COLUMN IF EXISTS snapshot_at,
    DROP COLUMN IF EXISTS definition_hash;

DROP INDEX IF EXISTS idx_saved_object_sets_definition_hash;
DROP INDEX IF EXISTS idx_saved_object_sets_reaper;

ALTER TABLE saved_object_sets
    DROP COLUMN IF EXISTS frozen_truncated,
    DROP COLUMN IF EXISTS frozen_primary_keys,
    DROP COLUMN IF EXISTS frozen_object_type,
    DROP COLUMN IF EXISTS is_immutable,
    DROP COLUMN IF EXISTS snapshot_at,
    DROP COLUMN IF EXISTS definition_hash;

DROP SEQUENCE IF EXISTS saved_object_sets_snapshot_seq;
