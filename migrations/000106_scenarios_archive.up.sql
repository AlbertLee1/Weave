-- VTX-116 — Scenarios archive table.
--
-- After 90 days a scenario in status='applied' or 'failed' is moved into
-- this table with its full payload gzipped. The original row is removed
-- from `scenarios` so the working set stays small; reads come back
-- through scenarios.LoadArchived which transparently decompresses.

CREATE TABLE IF NOT EXISTS scenarios_archive (
    scenario_rid TEXT PRIMARY KEY,
    case_study_rid TEXT NOT NULL,
    name TEXT NOT NULL,
    parent_ontology_commit TEXT NOT NULL,
    status TEXT NOT NULL,
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    archived_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Gzipped JSON payload: { "edits": [...], "overrides": [...] }
    compressed_payload BYTEA NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_scenarios_archive_case_study
    ON scenarios_archive (case_study_rid);

CREATE INDEX IF NOT EXISTS idx_scenarios_archive_archived_at
    ON scenarios_archive (archived_at);
