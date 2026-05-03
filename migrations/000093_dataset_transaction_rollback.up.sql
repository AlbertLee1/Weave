-- US-388: Dataset Transaction Chain — explicit transactions + rollback.
--
-- Adds an audit overlay to `dataset_transactions` so a `POST /datasets/
-- {rid}/rollback?to=tx-...` invocation can mark every transaction newer
-- than the target as rolled back without losing the chain shape itself.
-- The columns are nullable so legacy rows from US-379 stay valid; only
-- explicitly-rolled-back txs carry a non-NULL `rolled_back_at`.
--
-- `rolled_back_to_tx_id` is the audit pointer that records WHICH rollback
-- invocation flipped this row. A self-pointer is also valid for the
-- bookkeeping row a rollback writes to mark the new chain head (see the
-- `cmd/server` rollback handler).

ALTER TABLE dataset_transactions
    ADD COLUMN IF NOT EXISTS rolled_back_at      TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS rolled_back_to_tx_id TEXT;

CREATE INDEX IF NOT EXISTS idx_dataset_transactions_rolled_back
    ON dataset_transactions (ontology_api_name, rolled_back_at)
    WHERE rolled_back_at IS NOT NULL;
