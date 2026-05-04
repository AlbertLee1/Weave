-- US-426: extend the action_jobs status enum to admit the CANCELED terminal
-- state introduced by US-318 (cancel handler) and add an index that
-- accelerates the hourly cleanup sweep that drops terminal-state rows older
-- than 24h.
--
-- The existing constraint added in 000046 only admitted PENDING / RUNNING /
-- SUCCEEDED / FAILED. The CancelActionJob path has been writing CANCELED
-- rows since US-318 — a setup that "works" only because pkg/oms reaches the
-- DB through code rather than direct SQL inserts AND the executor's
-- UPDATE goes through pgActionJobStore which never re-validated. This
-- migration aligns the constraint with the runtime contract so future
-- direct-SQL admin tooling cannot bypass the canonical state machine.

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'action_jobs_status_enum') THEN
        ALTER TABLE action_jobs
            DROP CONSTRAINT action_jobs_status_enum;
    END IF;
END$$;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'action_jobs_status_enum') THEN
        ALTER TABLE action_jobs
            ADD CONSTRAINT action_jobs_status_enum
            CHECK (status IN ('PENDING', 'RUNNING', 'SUCCEEDED', 'FAILED', 'CANCELED'));
    END IF;
END$$;

-- Partial index over terminal-state rows ordered by updated_at lets the
-- hourly reaper sweep `DELETE ... WHERE status IN (...) AND updated_at < $1`
-- without scanning live PENDING/RUNNING rows.
CREATE INDEX IF NOT EXISTS action_jobs_terminal_updated_idx
    ON action_jobs(updated_at)
    WHERE status IN ('SUCCEEDED', 'FAILED', 'CANCELED');
