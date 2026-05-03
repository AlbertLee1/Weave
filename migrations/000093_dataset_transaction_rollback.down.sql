-- US-388 down: drop the rollback audit columns + supporting partial index.

DROP INDEX IF EXISTS idx_dataset_transactions_rolled_back;

ALTER TABLE dataset_transactions
    DROP COLUMN IF EXISTS rolled_back_to_tx_id,
    DROP COLUMN IF EXISTS rolled_back_at;
