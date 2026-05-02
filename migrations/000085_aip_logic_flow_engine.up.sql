-- US-372: AIP Logic Block 图引擎.
--
-- The aip_logic_flows table already keeps the DAG itself in two JSONB
-- columns (nodes / edges); we treat those AS the nodes_json / edges_json
-- the PRD calls for. The engine adds two flow-level defaults so callers
-- can stamp a fallback model + retry budget once instead of repeating
-- the same configuration on every node:
--   - fallback_model TEXT — default LLM model the executor switches to
--                            when a node's primary provider exhausts its
--                            retry budget. Empty string disables the
--                            fallback.
--   - max_retries    INT  — flow-level default retry attempts the
--                            executor applies when a node does not
--                            override config.retry.maxAttempts. 0 means
--                            "no retry" (single attempt). Cap is 8 to
--                            keep runaway flows bounded.
--
-- Per-node retry / iterate semantics ride on the existing nodes JSONB:
--   - {"type":"iterate", "config":{"forEach":"<state.path>",
--                                  "body":{...node spec...},
--                                  "max":100}}
--   - {"config":{"retry":{"maxAttempts":<n>,"backoffMs":<ms>}}}
-- so no schema change is required to express them.

ALTER TABLE aip_logic_flows
    ADD COLUMN IF NOT EXISTS fallback_model TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS max_retries    INTEGER NOT NULL DEFAULT 0;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'aip_logic_flows_max_retries_check') THEN
        ALTER TABLE aip_logic_flows
            ADD CONSTRAINT aip_logic_flows_max_retries_check
            CHECK (max_retries >= 0 AND max_retries <= 8);
    END IF;
END$$;
