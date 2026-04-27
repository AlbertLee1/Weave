-- US-287 Pipeline DSL: persistent declarative data-pipeline definitions.
-- A pipeline is the YAML/JSON-shaped (inputs, transforms, outputs,
-- schedule) descriptor a data engineer authors and the runtime later
-- executes. This migration only persists the descriptor itself; the
-- DAG-execution engine (US-288) and the cron scheduler (US-289) ride on
-- top of the same row.
--
-- id           unique identifier (matches the same allowlist used by
--              aip_logic_flows / aip_threads / feature_flags)
-- name         free-form display name shown in the UI
-- description  free-form description
-- inputs       JSONB array — list of named source descriptors
-- transforms   JSONB array — ordered list of transform nodes
-- outputs      JSONB array — list of named sink descriptors
-- schedule     CRON expression (or empty string when run-on-demand)
-- enabled      when false the pipeline is hidden from the scheduler
-- created_by   tracks the authoring user

CREATE TABLE IF NOT EXISTS pipelines (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL DEFAULT '',
    description  TEXT NOT NULL DEFAULT '',
    inputs       JSONB NOT NULL DEFAULT '[]'::jsonb,
    transforms   JSONB NOT NULL DEFAULT '[]'::jsonb,
    outputs      JSONB NOT NULL DEFAULT '[]'::jsonb,
    schedule     TEXT NOT NULL DEFAULT '',
    enabled      BOOLEAN NOT NULL DEFAULT TRUE,
    created_by   TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS pipelines_created_by_idx
    ON pipelines(created_by, created_at DESC);

CREATE INDEX IF NOT EXISTS pipelines_enabled_idx
    ON pipelines(enabled);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'pipelines_id_format') THEN
        ALTER TABLE pipelines
            ADD CONSTRAINT pipelines_id_format
            CHECK (id ~ '^[A-Za-z0-9._-]{1,128}$');
    END IF;
END$$;
