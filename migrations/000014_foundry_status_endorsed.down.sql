-- Revert: restore old 5-value status enum
UPDATE object_types SET status = 'PROMOTED' WHERE status = 'ENDORSED';
ALTER TABLE object_types DROP CONSTRAINT IF EXISTS object_types_status_check;
ALTER TABLE object_types ADD CONSTRAINT object_types_status_check
    CHECK (status IN ('PROMOTED', 'ACTIVE', 'EXPERIMENTAL', 'DEPRECATED', 'EXAMPLE'));

UPDATE action_types SET status = 'PROMOTED' WHERE status = 'ENDORSED';
ALTER TABLE action_types DROP CONSTRAINT IF EXISTS action_types_status_check;
ALTER TABLE action_types ADD CONSTRAINT action_types_status_check
    CHECK (status IN ('PROMOTED', 'ACTIVE', 'EXPERIMENTAL', 'DEPRECATED', 'EXAMPLE'));
