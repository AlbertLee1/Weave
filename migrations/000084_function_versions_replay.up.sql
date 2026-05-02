-- US-370: Function 确定性重放 + 版本绑定.
--
-- The existing `functions` table is already keyed by (ontology_rid, name,
-- version) — every row is effectively a published version. We treat it as
-- the function_versions table the PRD calls for and bolt three columns onto
-- it so the registry can recognise a published build by its content hash:
--   - code_hash      — sha256 of the source code at publish time
--   - signature_hash — sha256 of the canonical-JSON signature (sorts keys,
--                      `{}` for absent contracts)
--   - published_at   — NOT NULL DEFAULT now() so legacy rows backfill cleanly
--
-- action_types gains a function_version column so an Action that references
-- a Function captures the exact semver it was bound to (PRD: "非 latest").
-- An Action with function_rid set but function_version blank is interpreted
-- as "latest" and the executor is free to resolve.
--
-- function_executions persists the input + output hash for every Function
-- invocation so the replay endpoint can compare a fresh run against the
-- historical hash and emit WEAVE_FUNCTION_NONDETERMINISTIC if they diverge.

ALTER TABLE functions
    ADD COLUMN IF NOT EXISTS code_hash TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS signature_hash TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS published_at TIMESTAMPTZ NOT NULL DEFAULT now();

ALTER TABLE action_types
    ADD COLUMN IF NOT EXISTS function_version TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS function_executions (
    execution_id    TEXT PRIMARY KEY,
    function_rid    TEXT NOT NULL,
    function_name   TEXT NOT NULL DEFAULT '',
    function_version TEXT NOT NULL,
    ontology_rid    TEXT NOT NULL DEFAULT '',
    input_hash      TEXT NOT NULL,
    output_hash     TEXT NOT NULL,
    input_json      JSONB NOT NULL DEFAULT '{}'::jsonb,
    output_json     JSONB NOT NULL DEFAULT 'null'::jsonb,
    error_message   TEXT NOT NULL DEFAULT '',
    requested_by    TEXT NOT NULL DEFAULT '',
    is_replay       BOOLEAN NOT NULL DEFAULT FALSE,
    replay_of       TEXT NOT NULL DEFAULT '',
    executed_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS function_executions_rid_version_idx
    ON function_executions(function_rid, function_version);

CREATE INDEX IF NOT EXISTS function_executions_input_hash_idx
    ON function_executions(function_rid, function_version, input_hash);
