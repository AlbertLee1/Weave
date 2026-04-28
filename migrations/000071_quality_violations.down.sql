-- US-296 rollback.

DROP INDEX IF EXISTS quality_violations_rule_idx;
DROP INDEX IF EXISTS quality_violations_pipeline_run_idx;
DROP TABLE IF EXISTS quality_violations;
