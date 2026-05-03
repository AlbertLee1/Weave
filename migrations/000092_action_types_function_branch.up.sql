-- US-384: Branch 上的 ActionType / Function 修改.
--
-- The PRD demands that the same `apiName` (for action_types) and the same
-- `(name, version)` pair (for functions) can coexist as independent rows
-- across different branches. The canonical "main" trunk continues to use
-- branch_id = 'main'; a feature branch publishing its own ActionType v2
-- writes a row with branch_id = '<branch>' that does NOT collide with the
-- main row thanks to the widened UNIQUE constraints below.
--
-- The schema change is additive and keeps existing rows valid: every
-- pre-US-384 row defaults to branch_id = 'main', which matches what the
-- legacy single-row-per-apiName invariant always implied. Read paths that
-- want branch-scoped routing call the new *OnBranch repository methods
-- (pkg/oms); legacy reads that don't care about branches still see only
-- the main row because the helper falls back to branch_id = 'main' when no
-- branch-specific override exists.

-- action_types: drop the (ontology_rid, api_name) UNIQUE and replace with
-- (ontology_rid, api_name, branch_id). The legacy constraint is named
-- action_types_ontology_rid_api_name_key by pgsql; guard with IF EXISTS so
-- a re-run after a successful apply is a no-op.
ALTER TABLE action_types
    ADD COLUMN IF NOT EXISTS branch_id TEXT NOT NULL DEFAULT 'main';

ALTER TABLE action_types
    DROP CONSTRAINT IF EXISTS action_types_ontology_rid_api_name_key;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'action_types_ontology_rid_api_name_branch_id_key'
    ) THEN
        ALTER TABLE action_types
            ADD CONSTRAINT action_types_ontology_rid_api_name_branch_id_key
            UNIQUE (ontology_rid, api_name, branch_id);
    END IF;
END$$;

CREATE INDEX IF NOT EXISTS action_types_branch_idx
    ON action_types (ontology_rid, api_name, branch_id);

-- functions: the (ontology_rid, name, version) UNIQUE constraint added in
-- 000041 is widened to include branch_id so a branch can publish its own
-- semver line without colliding with main.
ALTER TABLE functions
    ADD COLUMN IF NOT EXISTS branch_id TEXT NOT NULL DEFAULT 'main';

ALTER TABLE functions
    DROP CONSTRAINT IF EXISTS functions_ontology_rid_name_version_key;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'functions_ontology_rid_name_version_branch_id_key'
    ) THEN
        ALTER TABLE functions
            ADD CONSTRAINT functions_ontology_rid_name_version_branch_id_key
            UNIQUE (ontology_rid, name, version, branch_id);
    END IF;
END$$;

CREATE INDEX IF NOT EXISTS functions_branch_idx
    ON functions (ontology_rid, name, branch_id);
