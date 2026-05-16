-- Reverse VTX-077 per-ObjectType event metadata.

ALTER TABLE object_types
    DROP COLUMN IF EXISTS event_end_prop,
    DROP COLUMN IF EXISTS event_start_prop,
    DROP COLUMN IF EXISTS is_event;
