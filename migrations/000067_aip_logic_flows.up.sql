-- US-281 AIP Logic 后端: persistent DAG-shaped LLM workflows.
-- A flow is a graph of nodes (llm | tool | if | output) connected by
-- edges. Nodes are stored as a JSONB array on the flow row; edges are
-- stored as a separate JSONB array so the executor can topologically
-- order nodes without pivoting twice through the same blob. Flow runs
-- (one row per execution) capture inputs / outputs / status / error
-- for audit and post-hoc debugging.

CREATE TABLE IF NOT EXISTS aip_logic_flows (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL DEFAULT '',
    description  TEXT NOT NULL DEFAULT '',
    nodes        JSONB NOT NULL DEFAULT '[]'::jsonb,
    edges        JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_by   TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS aip_logic_flows_created_by_idx
    ON aip_logic_flows(created_by, created_at DESC);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'aip_logic_flows_id_format') THEN
        ALTER TABLE aip_logic_flows
            ADD CONSTRAINT aip_logic_flows_id_format
            CHECK (id ~ '^[A-Za-z0-9._-]{1,128}$');
    END IF;
END$$;

CREATE TABLE IF NOT EXISTS aip_logic_flow_runs (
    id           BIGSERIAL PRIMARY KEY,
    flow_id      TEXT NOT NULL REFERENCES aip_logic_flows(id) ON DELETE CASCADE,
    status       TEXT NOT NULL DEFAULT 'success',
    input        JSONB NOT NULL DEFAULT '{}'::jsonb,
    output       JSONB NOT NULL DEFAULT '{}'::jsonb,
    trace        JSONB NOT NULL DEFAULT '[]'::jsonb,
    error_message TEXT NOT NULL DEFAULT '',
    created_by   TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS aip_logic_flow_runs_flow_idx
    ON aip_logic_flow_runs(flow_id, id DESC);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'aip_logic_flow_runs_status_check') THEN
        ALTER TABLE aip_logic_flow_runs
            ADD CONSTRAINT aip_logic_flow_runs_status_check
            CHECK (status IN ('success', 'failed'));
    END IF;
END$$;
