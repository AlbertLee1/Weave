-- US-396 Apps publish & share: per-row publication state on the live
-- `apps` row. published_version pins which AppVersion is the current
-- read-only snapshot served by /apps/{rid}/view; published_at and
-- published_by are audit metadata.
--
-- An App is "published" iff published_version IS NOT NULL. Re-publish
-- bumps the pin to the latest version (and refreshes published_at /
-- published_by). Unpublish clears all three columns. Editors are the
-- owner of the row; viewers are any authenticated user with the RID.

ALTER TABLE apps
    ADD COLUMN IF NOT EXISTS published_version INT NULL,
    ADD COLUMN IF NOT EXISTS published_at      TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS published_by      TEXT NULL;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'apps_published_version_positive') THEN
        ALTER TABLE apps
            ADD CONSTRAINT apps_published_version_positive
            CHECK (published_version IS NULL OR published_version >= 1);
    END IF;
END$$;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'apps_published_consistency') THEN
        ALTER TABLE apps
            ADD CONSTRAINT apps_published_consistency
            CHECK (
                (published_version IS NULL AND published_at IS NULL AND published_by IS NULL)
                OR
                (published_version IS NOT NULL AND published_at IS NOT NULL AND published_by IS NOT NULL)
            );
    END IF;
END$$;
