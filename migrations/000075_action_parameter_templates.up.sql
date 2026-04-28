-- US-320 Action 参数模板: per-user named parameter sets for the Action
-- Console.
--
-- One row per template. Templates are scoped to a single ActionType
-- (referenced by apiName so the wire shape mirrors the URL the SPA
-- already uses on /actions/:ontology). Per-owner uniqueness is keyed
-- by (created_by, action_type, name) so the same name can be reused
-- across action types and across users without colliding.
--
-- The `shared` boolean flips a row from private (visible only to its
-- creator) to shared (visible to anyone with permission to read this
-- ontology's action types). Edit/delete remain owner-only — `shared`
-- only widens read access.
--
-- The `parameters` JSONB carries the persisted values as a flat
-- {paramId: value} map; the action console feeds this directly into
-- the ParameterForm so future parameter shapes need no schema change.

CREATE TABLE IF NOT EXISTS action_parameter_templates (
    id            UUID PRIMARY KEY,
    name          TEXT NOT NULL,
    ontology      TEXT NOT NULL,
    action_type   TEXT NOT NULL,
    created_by    TEXT NOT NULL,
    shared        BOOLEAN NOT NULL DEFAULT FALSE,
    parameters    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'action_parameter_templates_name_format') THEN
        ALTER TABLE action_parameter_templates
            ADD CONSTRAINT action_parameter_templates_name_format
            CHECK (length(name) BETWEEN 1 AND 128);
    END IF;
END$$;

CREATE UNIQUE INDEX IF NOT EXISTS action_parameter_templates_owner_name_idx
    ON action_parameter_templates(created_by, action_type, name);

CREATE INDEX IF NOT EXISTS action_parameter_templates_scope_idx
    ON action_parameter_templates(ontology, action_type);

CREATE INDEX IF NOT EXISTS action_parameter_templates_shared_idx
    ON action_parameter_templates(ontology, action_type)
    WHERE shared = TRUE;
