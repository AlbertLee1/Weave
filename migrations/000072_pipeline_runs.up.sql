-- US-298 Pipeline 执行历史 API: persists one row per Pipeline execution
-- so operators can audit past runs (status, error, duration, full RunResult
-- payload). Append-only — runs are NEVER updated in place; a re-run minted
-- by the cron scheduler (US-289) or by a future manual-trigger surface
-- inserts a fresh row.
--
-- id            BIGSERIAL — monotonic ordering handle that doubles as the
--               keyset cursor for the paginated list endpoint.
-- pipeline_id   FK → pipelines.id, ON DELETE CASCADE so admin pipeline
--               deletion drags the run history with it.
-- status        free-form (matches RunResult.Status: success | failed |
--               canceled). No CHECK on the column — keeping it loose
--               lets future runner statuses (timeout, throttled, ...)
--               land without a paired migration.
-- run_result    JSONB blob mirroring pipeline.RunResult — the executor's
--               full per-node detail. Always non-NULL (default '{}').
-- triggered_by  who/what initiated the run (user id, "cron",
--               "manual:<userid>", ...).

CREATE TABLE IF NOT EXISTS pipeline_runs (
    id            BIGSERIAL PRIMARY KEY,
    pipeline_id   TEXT NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    status        TEXT NOT NULL DEFAULT '',
    started_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at   TIMESTAMPTZ,
    error_message TEXT NOT NULL DEFAULT '',
    run_result    JSONB NOT NULL DEFAULT '{}'::jsonb,
    triggered_by  TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS pipeline_runs_pipeline_id_idx
    ON pipeline_runs(pipeline_id, id DESC);
