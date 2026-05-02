-- Reverse of 000084_function_versions_replay.up.sql.

DROP INDEX IF EXISTS function_executions_input_hash_idx;
DROP INDEX IF EXISTS function_executions_rid_version_idx;
DROP TABLE IF EXISTS function_executions;

ALTER TABLE action_types DROP COLUMN IF EXISTS function_version;

ALTER TABLE functions
    DROP COLUMN IF EXISTS published_at,
    DROP COLUMN IF EXISTS signature_hash,
    DROP COLUMN IF EXISTS code_hash;
