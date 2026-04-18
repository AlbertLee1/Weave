-- US-239: Saga / Compensating Actions. Adds a self-referential pointer on
-- action_types so a modeler can declare the ActionType to execute as a
-- compensation when a multi-step batch fails. Nullable because most actions
-- have no compensator; handler-layer validation resolves the RID at write
-- time. Partial index keeps the lookup cheap while excluding the common
-- "no compensator" rows.

ALTER TABLE action_types
    ADD COLUMN IF NOT EXISTS compensate_action_rid TEXT;

CREATE INDEX IF NOT EXISTS action_types_compensate_rid_idx
    ON action_types(compensate_action_rid)
    WHERE compensate_action_rid IS NOT NULL;
