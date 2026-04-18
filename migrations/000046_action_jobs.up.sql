-- US-240: Async Long-Running Actions. Persistent job state for
-- POST /actions/{action}/apply?async=true and the GET /actions/jobs/{id}
-- polling endpoint. `status` is the enum PENDING | RUNNING | SUCCEEDED | FAILED;
-- `progress` is 0..100; `result` holds the SyncApplyActionResponseV2 envelope
-- once the job succeeds, empty otherwise. `error_message` is populated on
-- FAILED so pollers can surface a human-readable reason.

CREATE TABLE IF NOT EXISTS action_jobs (
    job_id            TEXT PRIMARY KEY,
    ontology_api_name TEXT NOT NULL,
    action_type       TEXT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'PENDING',
    progress          INTEGER NOT NULL DEFAULT 0,
    result            JSONB,
    error_message     TEXT NOT NULL DEFAULT '',
    created_by        TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Pollers filter by ontology; admin backfills filter by created_by.
CREATE INDEX IF NOT EXISTS action_jobs_ontology_created_idx
    ON action_jobs(ontology_api_name, created_at DESC);

CREATE INDEX IF NOT EXISTS action_jobs_created_by_idx
    ON action_jobs(created_by)
    WHERE created_by <> '';

-- Progress must stay within [0, 100]. Wrap in DO $$ so re-applying the
-- migration in the migrate-up-then-up dev flow does not fail on a
-- duplicate-constraint error (there is no IF NOT EXISTS on CHECKs). Prior
-- art: migrations/000040 functions_runtime_check.
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'action_jobs_progress_range') THEN
        ALTER TABLE action_jobs
            ADD CONSTRAINT action_jobs_progress_range
            CHECK (progress >= 0 AND progress <= 100);
    END IF;
END$$;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'action_jobs_status_enum') THEN
        ALTER TABLE action_jobs
            ADD CONSTRAINT action_jobs_status_enum
            CHECK (status IN ('PENDING', 'RUNNING', 'SUCCEEDED', 'FAILED'));
    END IF;
END$$;
