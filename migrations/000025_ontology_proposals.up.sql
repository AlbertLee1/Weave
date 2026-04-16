-- US-117: Ontology proposals — merge proposals from branches.

CREATE TABLE IF NOT EXISTS ontology_proposals (
    id           TEXT PRIMARY KEY,
    branch_id    TEXT NOT NULL REFERENCES ontology_branches(id) ON DELETE CASCADE,
    ontology_rid TEXT NOT NULL REFERENCES ontologies(rid) ON DELETE CASCADE,
    title        TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected', 'merged')),
    author       TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS proposal_reviews (
    id          TEXT PRIMARY KEY,
    proposal_id TEXT NOT NULL REFERENCES ontology_proposals(id) ON DELETE CASCADE,
    reviewer    TEXT NOT NULL,
    decision    TEXT NOT NULL CHECK (decision IN ('approve', 'reject')),
    reason      TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
