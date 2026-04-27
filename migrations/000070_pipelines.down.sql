-- US-287 rollback.

DROP INDEX IF EXISTS pipelines_enabled_idx;
DROP INDEX IF EXISTS pipelines_created_by_idx;
DROP TABLE IF EXISTS pipelines;
