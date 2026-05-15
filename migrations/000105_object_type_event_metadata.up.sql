-- VTX-077: per-ObjectType event metadata so the Vertex Timeline can render
-- event-typed objects as horizontal time bars.
--
-- is_event       : when true the ObjectType is an "event" (FlightDelay,
--                  Maintenance, Weather, Alert, …) and its rows participate
--                  in Timeline rendering. Default false preserves pre-VTX
--                  behaviour (no Timeline placement).
-- event_start_prop / event_end_prop : property API names holding the
--                  start / end timestamps. event_end_prop NULL/empty means
--                  point-in-time event (single tick, not a bar).

ALTER TABLE object_types
    ADD COLUMN IF NOT EXISTS is_event BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS event_start_prop TEXT,
    ADD COLUMN IF NOT EXISTS event_end_prop TEXT;
