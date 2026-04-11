-- Reverse US-044 ontology_rid additions.

DROP INDEX IF EXISTS idx_object_embeddings_ontology;
ALTER TABLE object_embeddings DROP COLUMN IF EXISTS ontology_rid;

DROP INDEX IF EXISTS idx_object_history_ontology;
ALTER TABLE object_history DROP COLUMN IF EXISTS ontology_rid;

DROP INDEX IF EXISTS idx_link_edges_ontology;
ALTER TABLE link_edges DROP COLUMN IF EXISTS ontology_rid;
