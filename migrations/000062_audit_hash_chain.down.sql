-- Reverse US-266 audit_events hash-chain columns.

DROP INDEX IF EXISTS idx_audit_events_ts_day;
DROP INDEX IF EXISTS idx_audit_events_chain_seq;

ALTER TABLE audit_events DROP COLUMN IF EXISTS entry_hash;
ALTER TABLE audit_events DROP COLUMN IF EXISTS prev_hash;
ALTER TABLE audit_events DROP COLUMN IF EXISTS chain_seq;
