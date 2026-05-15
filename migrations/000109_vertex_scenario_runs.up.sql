-- VTX-057 Scenario Execution Service: per-Scenario workflow run with
-- checkpoint state for crash recovery.
--
-- Each row captures the lifecycle of a single Run: pending → running →
-- (succeeded | failed | canceled). The `state` JSONB column is the
-- restartable checkpoint — it stores which activities already finished
-- and the per-activity attempt counter, so a worker that crashed
-- mid-run can resume from `state.completed` instead of re-executing
-- successful activities. The application layer (pkg/vertex/scenarioruns)
-- writes a fresh checkpoint after every activity transition.

CREATE TABLE IF NOT EXISTS scenario_runs (
    rid           TEXT PRIMARY KEY,
    scenario_rid  TEXT NOT NULL REFERENCES scenarios(rid) ON DELETE CASCADE,
    status        TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'canceled')),
    error         TEXT NOT NULL DEFAULT '',
    state         JSONB NOT NULL DEFAULT '{}'::jsonb,
    started_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS scenario_runs_scenario_idx
    ON scenario_runs(scenario_rid);

CREATE INDEX IF NOT EXISTS scenario_runs_resumable_idx
    ON scenario_runs(status)
    WHERE status IN ('pending', 'running');
