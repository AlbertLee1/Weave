-- Automation rules table
CREATE TABLE IF NOT EXISTS automation_rules (
    id          TEXT PRIMARY KEY,
    ontology_rid TEXT NOT NULL REFERENCES ontologies(rid),
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL CHECK (status IN ('active', 'paused', 'disabled')),
    trigger_type TEXT NOT NULL CHECK (trigger_type IN ('schedule', 'dataChange', 'manual')),
    trigger_config JSONB NOT NULL DEFAULT '{}',
    effects     JSONB NOT NULL DEFAULT '[]',
    created_by  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Automation executions table
CREATE TABLE IF NOT EXISTS automation_executions (
    id            TEXT PRIMARY KEY,
    rule_id       TEXT NOT NULL REFERENCES automation_rules(id) ON DELETE CASCADE,
    trigger_event JSONB NOT NULL DEFAULT '{}',
    started_at    TIMESTAMPTZ NOT NULL,
    completed_at  TIMESTAMPTZ,
    status        TEXT NOT NULL CHECK (status IN ('running', 'success', 'error', 'retrying')),
    error         TEXT NOT NULL DEFAULT '',
    retry_count   INT NOT NULL DEFAULT 0
);
