-- Rollback US-396 publish columns. Drop CHECK constraints first so the
-- column drop doesn't trip the consistency invariant.

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'apps_published_consistency') THEN
        ALTER TABLE apps DROP CONSTRAINT apps_published_consistency;
    END IF;
END$$;

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'apps_published_version_positive') THEN
        ALTER TABLE apps DROP CONSTRAINT apps_published_version_positive;
    END IF;
END$$;

ALTER TABLE apps
    DROP COLUMN IF EXISTS published_by,
    DROP COLUMN IF EXISTS published_at,
    DROP COLUMN IF EXISTS published_version;
