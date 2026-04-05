-- Extend status CHECK constraints to support 5 levels
ALTER TABLE object_types DROP CONSTRAINT IF EXISTS object_types_status_check;
ALTER TABLE object_types ADD CONSTRAINT object_types_status_check
    CHECK (status IN ('PROMOTED', 'ACTIVE', 'EXPERIMENTAL', 'DEPRECATED', 'EXAMPLE'));

-- Add deprecation metadata to object_types
ALTER TABLE object_types ADD COLUMN IF NOT EXISTS deprecated_reason TEXT;
ALTER TABLE object_types ADD COLUMN IF NOT EXISTS deprecated_deadline TIMESTAMPTZ;

-- Add status column to properties
ALTER TABLE properties ADD COLUMN IF NOT EXISTS status TEXT DEFAULT 'ACTIVE'
    CHECK (status IN ('ACTIVE', 'EXPERIMENTAL', 'DEPRECATED'));
ALTER TABLE properties ADD COLUMN IF NOT EXISTS deprecated_reason TEXT;

-- Add status CHECK to action_types (if not exists)
ALTER TABLE action_types DROP CONSTRAINT IF EXISTS action_types_status_check;
ALTER TABLE action_types ADD CONSTRAINT action_types_status_check
    CHECK (status IN ('PROMOTED', 'ACTIVE', 'EXPERIMENTAL', 'DEPRECATED', 'EXAMPLE'));
