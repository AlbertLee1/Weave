-- Reverse US-217. Drops the (ontology_rid, name, version) UNIQUE, restores
-- the (ontology_rid, name) UNIQUE, and converts version back to INTEGER by
-- parsing the leading numeric segment of the semver string. Rows whose
-- version doesn't start with an integer (e.g. "alpha") collapse to 1.

ALTER TABLE functions DROP CONSTRAINT IF EXISTS functions_ontology_rid_name_version_key;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'functions_ontology_rid_name_key'
    ) THEN
        ALTER TABLE functions
            ADD CONSTRAINT functions_ontology_rid_name_key
            UNIQUE (ontology_rid, name);
    END IF;
END$$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'functions'
          AND column_name = 'version'
          AND data_type = 'text'
    ) THEN
        ALTER TABLE functions
            ALTER COLUMN version DROP DEFAULT;
        ALTER TABLE functions
            ALTER COLUMN version TYPE INTEGER USING (
                COALESCE(NULLIF(split_part(version, '.', 1), '')::INTEGER, 1)
            );
        ALTER TABLE functions
            ALTER COLUMN version SET DEFAULT 1;
        ALTER TABLE functions
            ALTER COLUMN version SET NOT NULL;
    END IF;
END$$;
