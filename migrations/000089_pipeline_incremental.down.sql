DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'pipelines_mode_check') THEN
        ALTER TABLE pipelines DROP CONSTRAINT pipelines_mode_check;
    END IF;
END$$;

ALTER TABLE pipeline_runs DROP COLUMN IF EXISTS last_committed_offset;

ALTER TABLE pipelines DROP COLUMN IF EXISTS last_known_schema;
ALTER TABLE pipelines DROP COLUMN IF EXISTS mode;
