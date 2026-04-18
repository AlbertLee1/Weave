DROP INDEX IF EXISTS ix_object_types_extends_rid;

ALTER TABLE object_types
    DROP COLUMN IF EXISTS extends_rid;
