-- US-224 ObjectSet Versioned Snapshots: persistent table that freezes an
-- ObjectSet's materialised PK list at a moment in time so analysts can
-- compare the same set against future state. Definition is kept alongside
-- the PKs so a future "re-execute and diff" workflow has the full source
-- query without going back to the in-memory temporary store (whose entries
-- expire on TTL).
--
--   POST /api/v2/ontologies/{ont}/objectSets/{objectSetRid}/snapshot
--      → freezes the current Execute result into one row.
--   GET  /api/v2/ontologies/{ont}/objectSets/snapshots/{snapshotRid}
--      → loads the row's PrimaryKeys and returns each row from Bleve via
--        the standard WireObject envelope.
--
-- ontology_api_name lets GetSnapshot re-scope the live Bleve index without
-- a second OMS lookup; object_type is the API name of the materialised
-- ObjectType (Result.ObjectType) so the read path knows which index to hit.

CREATE TABLE IF NOT EXISTS object_set_snapshots (
    rid                 TEXT        PRIMARY KEY,
    ontology_api_name   TEXT        NOT NULL,
    object_type         TEXT        NOT NULL,
    definition          JSONB       NOT NULL,
    primary_keys        JSONB       NOT NULL,
    truncated           BOOLEAN     NOT NULL DEFAULT FALSE,
    created_by          TEXT        NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_object_set_snapshots_ontology
    ON object_set_snapshots (ontology_api_name, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_object_set_snapshots_created_by
    ON object_set_snapshots (created_by);
