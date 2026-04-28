-- US-299 Lineage 元数据模型: directed provenance graph linking upstream sources
-- (action-log / pipeline-run / dataset / ...) to downstream objects so the
-- platform can answer "where did this object come from?". Append-only — every
-- write path inserts one edge per affected object; rows are NEVER updated in
-- place (a re-write mints a fresh row).
--
-- id              BIGSERIAL — monotonic ordering handle for cursor-pagination.
-- upstream_rid    canonical RID of the source — e.g.
--                 ri.actions.main.action-log.<id> for action-driven writes,
--                 ri.pipelines.main.run.<id> for pipeline-driven writes,
--                 ri.datasets.main.dataset.<id> for ingest writes.
-- downstream_rid  canonical RID of the affected object — mirrors the
--                 oms.ObjectLineageRID format so callers can join against
--                 ri.phonograph2-objects.main.object.<objectType>.<pk>.
-- operation       free-form discriminator (CREATE / MODIFY / DELETE / ...).
--                 No CHECK on the column — keeping it loose lets future
--                 operations land without a paired migration.
-- ts              when the edge was recorded (defaults to NOW so callers
--                 that omit it still produce a well-ordered history).

CREATE TABLE IF NOT EXISTS lineage_edges (
    id              BIGSERIAL PRIMARY KEY,
    upstream_rid    TEXT NOT NULL,
    downstream_rid  TEXT NOT NULL,
    operation       TEXT NOT NULL DEFAULT '',
    ts              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS lineage_edges_downstream_idx
    ON lineage_edges(downstream_rid, ts DESC);
CREATE INDEX IF NOT EXISTS lineage_edges_upstream_idx
    ON lineage_edges(upstream_rid, ts DESC);
