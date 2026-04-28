-- US-296 Pipeline data quality: persist rows that failed a quality
-- rule so operators can audit data hygiene over time. The rule DSL
-- itself (notNull / range / unique / regex / foreign_key) lives in
-- pkg/pipeline/quality; this migration only carries the violations
-- table the runner writes into.
--
-- id           random violation identifier (uuid hex from the writer)
-- pipeline_id  pipeline that produced the row (empty when the
--              checker is invoked outside a pipeline context)
-- run_id       per-execution id; empty for ad-hoc Check calls
-- node_name    transform / output node that produced the row
-- rule_name    operator-assigned rule name (unique within a ruleset)
-- rule_type    one of notNull / range / unique / regex / foreign_key
-- field        target column the rule applied to (empty for
--              row-level rules a future story may add)
-- row_index    0-based row position inside the run's row stream;
--              BIGINT so multi-million-row pipelines round-trip
-- row_key      optional traceability handle (e.g. a primary key the
--              upstream connector emitted alongside the row)
-- reason       short human-readable failure description
-- value        canonical string form of the failing value (empty
--              when the rule failed because the value was absent)
-- detected_at  insert-time stamp; defaults so callers can omit it

CREATE TABLE IF NOT EXISTS quality_violations (
    id           TEXT PRIMARY KEY,
    pipeline_id  TEXT NOT NULL DEFAULT '',
    run_id       TEXT NOT NULL DEFAULT '',
    node_name    TEXT NOT NULL DEFAULT '',
    rule_name    TEXT NOT NULL,
    rule_type    TEXT NOT NULL,
    field        TEXT NOT NULL DEFAULT '',
    row_index    BIGINT NOT NULL DEFAULT 0,
    row_key      TEXT NOT NULL DEFAULT '',
    reason       TEXT NOT NULL DEFAULT '',
    value        TEXT NOT NULL DEFAULT '',
    detected_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS quality_violations_pipeline_run_idx
    ON quality_violations(pipeline_id, run_id, detected_at DESC);

CREATE INDEX IF NOT EXISTS quality_violations_rule_idx
    ON quality_violations(rule_name, detected_at DESC);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'quality_violations_rule_type_check') THEN
        ALTER TABLE quality_violations
            ADD CONSTRAINT quality_violations_rule_type_check
            CHECK (rule_type IN ('notNull', 'range', 'unique', 'regex', 'foreign_key'));
    END IF;
END$$;
