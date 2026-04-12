-- US-089: Functions table for storing JavaScript source code as first-class
-- ontology resources, backing the Goja embedded function runtime.

CREATE TABLE IF NOT EXISTS functions (
    rid          TEXT PRIMARY KEY,
    ontology_rid TEXT NOT NULL REFERENCES ontologies(rid),
    name         TEXT NOT NULL,
    version      INTEGER NOT NULL DEFAULT 1,
    source_code  TEXT NOT NULL,
    created_by   TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ DEFAULT now(),
    UNIQUE(ontology_rid, name)
);

CREATE INDEX IF NOT EXISTS idx_functions_ontology ON functions(ontology_rid);
