DROP INDEX IF EXISTS action_types_compensate_rid_idx;
ALTER TABLE action_types DROP COLUMN IF EXISTS compensate_action_rid;
