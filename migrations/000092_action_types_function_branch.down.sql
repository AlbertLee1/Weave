-- US-384: rollback of branch_id column on action_types and functions.

DROP INDEX IF EXISTS action_types_branch_idx;

ALTER TABLE action_types
    DROP CONSTRAINT IF EXISTS action_types_ontology_rid_api_name_branch_id_key;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'action_types_ontology_rid_api_name_key'
    ) THEN
        ALTER TABLE action_types
            ADD CONSTRAINT action_types_ontology_rid_api_name_key
            UNIQUE (ontology_rid, api_name);
    END IF;
END$$;

ALTER TABLE action_types
    DROP COLUMN IF EXISTS branch_id;

DROP INDEX IF EXISTS functions_branch_idx;

ALTER TABLE functions
    DROP CONSTRAINT IF EXISTS functions_ontology_rid_name_version_branch_id_key;

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

ALTER TABLE functions
    DROP COLUMN IF EXISTS branch_id;
