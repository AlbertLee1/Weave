-- US-372 down migration: drop fallback_model / max_retries from aip_logic_flows.

ALTER TABLE aip_logic_flows
    DROP CONSTRAINT IF EXISTS aip_logic_flows_max_retries_check;

ALTER TABLE aip_logic_flows
    DROP COLUMN IF EXISTS fallback_model,
    DROP COLUMN IF EXISTS max_retries;
