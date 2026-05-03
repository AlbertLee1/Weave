-- US-377 Lineage column-level: derived column-edge graph from
-- datasource_bindings. One row per (src_dataset, src_column, dst_property)
-- triplet, refreshed in lockstep with the parent binding (binding_rid). The
-- table is *not* append-only — Replace-by-binding is the canonical write
-- path so a binding update yields a clean overwrite of its derived edges.
--
-- id                     BIGSERIAL — stable ordering handle.
-- binding_rid            owning datasource_binding RID; the source-of-truth
--                        for cascading deletes when the binding goes away.
-- src_dataset_rid        upstream dataset RID (mirrors DatasourceBinding.DatasetRID).
-- src_column             column name in the upstream dataset.
-- dst_object_type_rid    downstream ObjectType RID.
-- dst_property_rid       downstream Property RID — primary lookup key for
--                        GET /api/v2/lineage/property/{rid}.
-- dst_property_api_name  denormalised property api_name for human-readable
--                        wire shape and reverse-impact responses.
-- ts                     when the edge was derived (defaults to NOW so
--                        callers that omit it stay time-source agnostic).

CREATE TABLE IF NOT EXISTS lineage_column_edges (
    id                    BIGSERIAL PRIMARY KEY,
    binding_rid           TEXT NOT NULL,
    src_dataset_rid       TEXT NOT NULL,
    src_column            TEXT NOT NULL,
    dst_object_type_rid   TEXT NOT NULL,
    dst_property_rid      TEXT NOT NULL,
    dst_property_api_name TEXT NOT NULL,
    ts                    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS lineage_column_edges_dst_property_idx
    ON lineage_column_edges(dst_property_rid);
CREATE INDEX IF NOT EXISTS lineage_column_edges_src_idx
    ON lineage_column_edges(src_dataset_rid, src_column);
CREATE INDEX IF NOT EXISTS lineage_column_edges_binding_idx
    ON lineage_column_edges(binding_rid);
