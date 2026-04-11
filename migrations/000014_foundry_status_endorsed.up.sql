-- US-016: Align ObjectType/ActionType status enum with Foundry OSv2
-- Foundry statuses: ACTIVE, ENDORSED, EXPERIMENTAL, DEPRECATED
-- Remove Weave-only statuses: PROMOTED, EXAMPLE

-- Migrate existing PROMOTED → ENDORSED, EXAMPLE → EXPERIMENTAL
UPDATE object_types SET status = 'ENDORSED' WHERE status = 'PROMOTED';
UPDATE object_types SET status = 'EXPERIMENTAL' WHERE status = 'EXAMPLE';

ALTER TABLE object_types DROP CONSTRAINT IF EXISTS object_types_status_check;
ALTER TABLE object_types ADD CONSTRAINT object_types_status_check
    CHECK (status IN ('ACTIVE', 'ENDORSED', 'EXPERIMENTAL', 'DEPRECATED'));

-- Same for action_types
UPDATE action_types SET status = 'ENDORSED' WHERE status = 'PROMOTED';
UPDATE action_types SET status = 'EXPERIMENTAL' WHERE status = 'EXAMPLE';

ALTER TABLE action_types DROP CONSTRAINT IF EXISTS action_types_status_check;
ALTER TABLE action_types ADD CONSTRAINT action_types_status_check
    CHECK (status IN ('ACTIVE', 'ENDORSED', 'EXPERIMENTAL', 'DEPRECATED'));
