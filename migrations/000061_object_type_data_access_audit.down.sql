-- Reverse US-264 per-ObjectType data-access audit toggle.

ALTER TABLE object_types
    DROP COLUMN IF EXISTS audit_data_access;
