-- Migration: add query_types table

CREATE TABLE query_types (
    rid          TEXT PRIMARY KEY,
    ontology_rid TEXT NOT NULL REFERENCES ontologies(rid),
    api_name     TEXT NOT NULL,
    display_name TEXT NOT NULL,
    description  TEXT,
    parameters   JSONB NOT NULL DEFAULT '[]',
    output       JSONB NOT NULL DEFAULT '{}',
    query        JSONB NOT NULL DEFAULT '{}',
    status       TEXT DEFAULT 'ACTIVE',
    created_at   TIMESTAMPTZ DEFAULT now(),
    UNIQUE(ontology_rid, api_name)
);

CREATE INDEX idx_query_types_ontology ON query_types(ontology_rid);
