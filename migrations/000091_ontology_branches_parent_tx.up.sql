-- US-383 Ontology Branch metadata: parent branch + base_tx + ACTIVE/ABANDONED statuses.
--
-- Adds the two missing columns demanded by the PRD acceptance criterion:
--   - parent_branch_id: optional reference to another branch row whose overlay
--     this branch inherits from (parent fallback resolution). NULL means the
--     branch's parent is the canonical "main" trunk.
--   - base_tx: optional dataset_transactions.tx_id checkpoint that bounds the
--     branch's view of the underlying dataset history. NULL means "HEAD at
--     creation". The column is loosely typed (TEXT, no FK) because
--     dataset_transactions only began recording rows in US-379 — branches
--     created before then have no tx to point at.
--
-- The status CHECK constraint is widened to accept the PRD's uppercase enum
-- (ACTIVE | MERGED | ABANDONED) alongside the legacy lowercase values
-- (open | merged | closed). The two namespaces are equivalent: ACTIVE↔open,
-- MERGED↔merged, ABANDONED↔closed. Mapping helpers in pkg/oms/models.go
-- (NormalizeBranchStatus) keep wire format consistent with whichever flavour
-- a caller supplies.

ALTER TABLE ontology_branches
    ADD COLUMN IF NOT EXISTS parent_branch_id TEXT
        REFERENCES ontology_branches(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS base_tx TEXT;

CREATE INDEX IF NOT EXISTS idx_ontology_branches_parent
    ON ontology_branches (parent_branch_id)
    WHERE parent_branch_id IS NOT NULL;

ALTER TABLE ontology_branches
    DROP CONSTRAINT IF EXISTS ontology_branches_status_check;

ALTER TABLE ontology_branches
    ADD CONSTRAINT ontology_branches_status_check
    CHECK (status IN (
        'open', 'merged', 'closed',
        'ACTIVE', 'MERGED', 'ABANDONED'
    ));
