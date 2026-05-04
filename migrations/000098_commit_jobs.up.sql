-- US-417: Per-commit CI job records for the Function code repository.
-- Each row captures the lint + test pipeline outcome for one commit on a
-- specific Function's bare git repo. The Marketplace / FunctionDiff UI
-- reads `status` to render the ✅/❌ badge next to the commit hash.
CREATE TABLE IF NOT EXISTS commit_jobs (
    id            BIGSERIAL PRIMARY KEY,
    function_rid  TEXT NOT NULL,
    commit_sha    TEXT NOT NULL,
    status        TEXT NOT NULL CHECK (status IN ('queued','running','success','failure','skipped')),
    lint_output   TEXT NOT NULL DEFAULT '',
    test_output   TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    started_at    TIMESTAMPTZ,
    finished_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT commit_jobs_function_commit_unique UNIQUE (function_rid, commit_sha)
);

CREATE INDEX IF NOT EXISTS idx_commit_jobs_function_created
    ON commit_jobs (function_rid, created_at DESC);
