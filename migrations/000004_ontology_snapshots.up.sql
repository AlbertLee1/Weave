CREATE TABLE IF NOT EXISTS ontology_snapshots (
    id           BIGSERIAL PRIMARY KEY,
    ontology_rid TEXT NOT NULL REFERENCES ontologies(rid),
    version      INTEGER NOT NULL,
    label        TEXT,
    description  TEXT,
    snapshot     JSONB NOT NULL,
    created_by   TEXT DEFAULT 'system',
    created_at   TIMESTAMPTZ DEFAULT now(),
    UNIQUE(ontology_rid, version)
);

ALTER TABLE ontologies ADD COLUMN IF NOT EXISTS current_version INTEGER DEFAULT 0;
