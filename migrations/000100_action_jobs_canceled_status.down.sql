-- US-426 down: restore the legacy 4-status CHECK constraint and drop the
-- terminal-state index. Note: any rows already in CANCELED state will fail
-- the restored constraint — operators rolling back must clean those up
-- (DELETE or convert to FAILED) before re-applying the down migration.

DROP INDEX IF EXISTS action_jobs_terminal_updated_idx;

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
            CHECK (status IN ('PENDING', 'RUNNING', 'SUCCEEDED', 'FAILED'));
    END IF;
END$$;
