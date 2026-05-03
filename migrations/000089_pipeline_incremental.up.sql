-- US-378 Pipeline 增量构建 + schema evolution.
--
-- Three additions across two existing tables:
--
-- 1. pipelines.mode (TEXT, default '')
--    Run mode for the pipeline. Empty / 'FULL' = the pre-US-378 behaviour
--    where every run scans the entire source. 'APPEND' = the new
--    incremental shape: every run only processes rows whose offset is
--    strictly greater than the prior successful run's last_committed_offset.
--
-- 2. pipelines.last_known_schema (JSONB, default '[]'::jsonb)
--    The most-recent observed source schema (an array of {name, type}
--    objects). Persisted on the pipeline row so a fresh run can compute
--    a schema diff against it BEFORE accepting any data. The diff drives
--    the schema-evolution gate: new columns auto-add, dropped columns
--    raise WEAVE_PIPELINE_BREAKING_CHANGE 422 and abort the run.
--
-- 3. pipeline_runs.last_committed_offset (BIGINT, default 0)
--    The high-water-mark offset (rows processed since the start of the
--    source's append-log) that this run advanced the pipeline to. The
--    next APPEND run starts from this value + 1. NOT NULL with a
--    default of 0 so legacy rows pre-migration read cleanly as "no
--    incremental progress recorded yet" — equivalent to a full re-scan
--    on the next APPEND run.
--
-- Replace-by-pipeline write semantics (mirror US-377): pipelines.last_known_schema
-- is overwritten on every successful APPEND run that advances the schema; a
-- run that doesn't change the schema doesn't bump it.

ALTER TABLE pipelines
    ADD COLUMN IF NOT EXISTS mode TEXT NOT NULL DEFAULT '';

ALTER TABLE pipelines
    ADD COLUMN IF NOT EXISTS last_known_schema JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE pipeline_runs
    ADD COLUMN IF NOT EXISTS last_committed_offset BIGINT NOT NULL DEFAULT 0;

-- Mode CHECK keeps the column tight against the in-package enum so a typo
-- ('apend') gets rejected at write time rather than silently dropping the
-- pipeline back to FULL semantics.
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'pipelines_mode_check') THEN
        ALTER TABLE pipelines
            ADD CONSTRAINT pipelines_mode_check
            CHECK (mode IN ('', 'FULL', 'APPEND'));
    END IF;
END$$;
