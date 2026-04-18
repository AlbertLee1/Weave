-- US-217: Function版本管理. Versions become semver strings (e.g. "1.0.0",
-- "2.1.0-beta") so multiple versions of the same function name can coexist
-- and downstream callers can pin a specific build via `name@version`.
--
-- The legacy column `version INTEGER NOT NULL DEFAULT 1` (introduced in
-- 000021) is migrated to `version TEXT NOT NULL DEFAULT '1.0.0'`. Existing
-- integer values are backfilled as `<n>.0.0` so previously-stored rows stay
-- addressable. The (ontology_rid, name) UNIQUE constraint moves to
-- (ontology_rid, name, version) so newer versions never collide with older
-- ones.

-- Column type change is gated on the column still being INTEGER. Re-running
-- the migration after a previous successful apply is a no-op.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'functions'
          AND column_name = 'version'
          AND data_type = 'integer'
    ) THEN
        ALTER TABLE functions
            ALTER COLUMN version DROP DEFAULT;
        ALTER TABLE functions
            ALTER COLUMN version TYPE TEXT USING (
                CASE WHEN version IS NULL THEN '1.0.0'
                     ELSE version::TEXT || '.0.0'
                END
            );
        ALTER TABLE functions
            ALTER COLUMN version SET DEFAULT '1.0.0';
        ALTER TABLE functions
            ALTER COLUMN version SET NOT NULL;
    END IF;
END$$;

-- Drop legacy (ontology_rid, name) UNIQUE — pgsql auto-named it
-- functions_ontology_rid_name_key. Idempotent.
ALTER TABLE functions DROP CONSTRAINT IF EXISTS functions_ontology_rid_name_key;

-- Add the new (ontology_rid, name, version) UNIQUE. ALTER TABLE … ADD
-- CONSTRAINT lacks IF NOT EXISTS for UNIQUE, so guard via pg_constraint.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'functions_ontology_rid_name_version_key'
    ) THEN
        ALTER TABLE functions
            ADD CONSTRAINT functions_ontology_rid_name_version_key
            UNIQUE (ontology_rid, name, version);
    END IF;
END$$;
