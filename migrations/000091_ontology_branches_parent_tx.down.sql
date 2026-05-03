-- Reverse US-383 metadata extensions on ontology_branches.

ALTER TABLE ontology_branches
    DROP CONSTRAINT IF EXISTS ontology_branches_status_check;

ALTER TABLE ontology_branches
    ADD CONSTRAINT ontology_branches_status_check
    CHECK (status IN ('open', 'merged', 'closed'));

DROP INDEX IF EXISTS idx_ontology_branches_parent;

ALTER TABLE ontology_branches
    DROP COLUMN IF EXISTS base_tx,
    DROP COLUMN IF EXISTS parent_branch_id;
