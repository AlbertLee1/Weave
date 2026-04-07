-- Tier 3.4: Saved (persistent) ObjectSet definitions.
--
-- The temporary in-memory ObjectSet store (1h TTL) is appropriate for
-- ephemeral share-link payloads but not for definitions that users want
-- to keep, name, and reuse across sessions. This table durably stores a
-- serialized ObjectSet tree (the same JSON wire format consumed by the
-- /objectSets/loadObjects endpoint) keyed by (ontology_api_name, name).
--
-- definition is JSONB so the executor can re-parse it without an extra
-- decode step, and so admins can run ad-hoc SQL filters on the tree
-- structure without unmarshalling.

CREATE TABLE IF NOT EXISTS saved_object_sets (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ontology_api_name TEXT NOT NULL,
    name              TEXT NOT NULL,
    description       TEXT,
    definition        JSONB NOT NULL,
    created_by        TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (ontology_api_name, name)
);

CREATE INDEX idx_saved_object_sets_ontology ON saved_object_sets (ontology_api_name);
CREATE INDEX idx_saved_object_sets_created_by ON saved_object_sets (created_by);
