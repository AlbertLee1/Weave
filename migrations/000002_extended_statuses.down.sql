ALTER TABLE object_types DROP CONSTRAINT IF EXISTS object_types_status_check;
ALTER TABLE object_types ADD CONSTRAINT object_types_status_check
    CHECK (status IN ('ACTIVE', 'EXPERIMENTAL', 'DEPRECATED'));
ALTER TABLE object_types DROP COLUMN IF EXISTS deprecated_reason;
ALTER TABLE object_types DROP COLUMN IF EXISTS deprecated_deadline;

ALTER TABLE properties DROP COLUMN IF EXISTS status;
ALTER TABLE properties DROP COLUMN IF EXISTS deprecated_reason;

ALTER TABLE action_types DROP CONSTRAINT IF EXISTS action_types_status_check;
