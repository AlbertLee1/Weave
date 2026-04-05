DROP TABLE IF EXISTS ontology_snapshots;

ALTER TABLE ontologies DROP COLUMN IF EXISTS current_version;
