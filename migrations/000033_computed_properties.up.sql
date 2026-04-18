-- US-202: Computed Properties (aggregation-based). Introduces a new table
-- capturing per-ObjectType computed properties whose value is materialised
-- lazily at query time by aggregating across a link. Each row declares:
--
--   source_link_rid   - the link type whose linked-objects set is aggregated.
--   aggregation       - JSONB {"type":"count|sum|avg|min|max","field":"..."}.
--                       `field` is required for numeric metrics; ignored for
--                       count.
--   cache_ttl_seconds - TTL for the in-memory value cache (default 60s).
--
-- The resolver (pkg/oss/computed) caches results by (object_type, pk,
-- api_name) for this many seconds so a hot read path does not re-walk the
-- link and re-scan Bleve on every request.

CREATE TABLE IF NOT EXISTS computed_properties (
    rid                TEXT PRIMARY KEY,
    object_type_rid    TEXT NOT NULL REFERENCES object_types(rid) ON DELETE CASCADE,
    api_name           TEXT NOT NULL,
    display_name       TEXT NOT NULL DEFAULT '',
    description        TEXT NOT NULL DEFAULT '',
    source_link_rid    TEXT NOT NULL REFERENCES link_types(rid) ON DELETE CASCADE,
    aggregation        JSONB NOT NULL,
    cache_ttl_seconds  INTEGER NOT NULL DEFAULT 60,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (object_type_rid, api_name)
);

CREATE INDEX IF NOT EXISTS computed_properties_object_type_rid_idx
    ON computed_properties(object_type_rid);
