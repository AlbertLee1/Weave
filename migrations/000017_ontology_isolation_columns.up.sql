-- US-044: Multi-ontology true isolation.
--
-- Adds an ontology_rid column to every per-ontology table that previously
-- only carried object_type_rid (or link_type_rid). The denormalized column
-- is defense-in-depth on the SQL side: queries can scope to a single
-- ontology without joining through the object_types/link_types tables, and
-- a misconfigured handler that drops the ontology filter would still be
-- caught by row-level constraints.
--
-- saved_object_sets already carries ontology_api_name (see migration
-- 000013) and is intentionally NOT modified here. markings is global by
-- design (the marking definitions are shared across ontologies; the
-- per-user grant table user_markings is also global).
--
-- Indexes are added so per-ontology lookups stay O(log n).

ALTER TABLE link_edges
    ADD COLUMN IF NOT EXISTS ontology_rid TEXT;

CREATE INDEX IF NOT EXISTS idx_link_edges_ontology
    ON link_edges (ontology_rid);

ALTER TABLE object_history
    ADD COLUMN IF NOT EXISTS ontology_rid TEXT;

CREATE INDEX IF NOT EXISTS idx_object_history_ontology
    ON object_history (ontology_rid, object_type_rid, primary_key);

ALTER TABLE object_embeddings
    ADD COLUMN IF NOT EXISTS ontology_rid TEXT;

CREATE INDEX IF NOT EXISTS idx_object_embeddings_ontology
    ON object_embeddings (ontology_rid);
