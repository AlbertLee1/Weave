-- US-112: Ontology branches and branch changes for schema branching.

CREATE TABLE IF NOT EXISTS ontology_branches (
    id          TEXT PRIMARY KEY,
    ontology_rid TEXT NOT NULL REFERENCES ontologies(rid) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    base_version BIGINT NOT NULL DEFAULT 0,
    status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'merged', 'closed')),
    created_by  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (ontology_rid, name)
);

CREATE TABLE IF NOT EXISTS ontology_branch_changes (
    id          TEXT PRIMARY KEY,
    branch_id   TEXT NOT NULL REFERENCES ontology_branches(id) ON DELETE CASCADE,
    change_type TEXT NOT NULL CHECK (change_type IN ('ADDED', 'MODIFIED', 'DELETED')),
    entity_type TEXT NOT NULL,
    entity_rid  TEXT NOT NULL,
    before_state JSONB,
    after_state  JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
