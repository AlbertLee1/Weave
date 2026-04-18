DROP INDEX IF EXISTS link_types_inverse_rid_idx;
ALTER TABLE link_types DROP COLUMN IF EXISTS inverse_link_rid;
