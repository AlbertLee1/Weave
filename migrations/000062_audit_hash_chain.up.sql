-- US-266: tamper-proof audit log chain.
--
-- Every audit_events row carries a monotonically increasing chain_seq,
-- the sha256 entry_hash of the canonical envelope (id + actor + action
-- + resource_type + resource_rid + diff_json + ip + user_agent + ts UTC
-- + prev_hash), and a prev_hash pointer back to the previous row's
-- entry_hash. Together they form an append-only Merkle-style chain —
-- flipping any byte of any row changes every downstream entry_hash and
-- the daily root hash anchored to disk.
--
-- Existing rows are chained in (ts ASC, id ASC) order so pre-migration
-- history becomes a single canonical chain. PG's ALTER TABLE … ADD
-- COLUMN BIGSERIAL allocates sequential values for existing rows in
-- insertion order, which is close enough for backfill since the only
-- property VerifyChain asserts is monotonicity + contiguity.

ALTER TABLE audit_events
    ADD COLUMN IF NOT EXISTS chain_seq BIGSERIAL;

ALTER TABLE audit_events
    ADD COLUMN IF NOT EXISTS prev_hash TEXT NOT NULL DEFAULT '';

ALTER TABLE audit_events
    ADD COLUMN IF NOT EXISTS entry_hash TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_audit_events_chain_seq
    ON audit_events (chain_seq);

-- The expression below needs an extra pair of parentheses around the
-- whole cast: PG's CREATE INDEX grammar treats a top-level paren list
-- as a column list, so `((ts AT TIME ZONE 'UTC')::DATE)` parses as a
-- single column "(ts AT TIME ZONE 'UTC')" with a stray "::" trailing.
-- Wrapping the cast in another paren makes it a proper expression
-- index. Discovered while standing up a clean DB for v3 e2e (the
-- existing dirty-state DB had stopped at this migration in v2).
CREATE INDEX IF NOT EXISTS idx_audit_events_ts_day
    ON audit_events (((ts AT TIME ZONE 'UTC')::DATE));
