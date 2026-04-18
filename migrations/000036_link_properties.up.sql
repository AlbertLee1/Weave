-- US-210: Link Properties (M2M edge properties). A LinkType may now declare
-- typed properties for its edges (e.g. membership.role on a user<->group
-- link). The metadata lives here; per-edge values continue to ride on
-- link_edges.edge_properties (JSONB) which was provisioned in migration
-- 000006 — no duplicate column churn needed.
--
-- Shape mirrors the existing `properties` table:
--   rid              TEXT PRIMARY KEY
--   link_type_rid    TEXT → link_types(rid) ON DELETE CASCADE
--   api_name         TEXT          (the JSON key used in edge_properties)
--   display_name     TEXT
--   description      TEXT          (NULL-able; see pg_repository NULLIF pattern)
--   base_type        TEXT          (one of pkg/types BaseType constants)
--   type_config      JSONB         (constraints, union variants, struct fields, etc.)
--   is_array         BOOLEAN
--   is_nullable      BOOLEAN
--   created_at       TIMESTAMPTZ DEFAULT now()
--
-- A UNIQUE(link_type_rid, api_name) guarantees every edge-property name is
-- unique within a LinkType — the JSONB key would clash otherwise.

CREATE TABLE IF NOT EXISTS link_properties (
    rid           TEXT PRIMARY KEY,
    link_type_rid TEXT NOT NULL REFERENCES link_types(rid) ON DELETE CASCADE,
    api_name      TEXT NOT NULL,
    display_name  TEXT NOT NULL DEFAULT '',
    description   TEXT,
    base_type     TEXT NOT NULL,
    type_config   JSONB,
    is_array      BOOLEAN NOT NULL DEFAULT FALSE,
    is_nullable   BOOLEAN NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (link_type_rid, api_name)
);

CREATE INDEX IF NOT EXISTS link_properties_link_type_rid_idx
    ON link_properties (link_type_rid);
