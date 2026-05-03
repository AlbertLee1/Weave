-- US-379 down: drop dataset_transactions table and the back-reference on
-- object_history. The ALTER COLUMN DROP is conditional so a partial
-- migration roll-forward / roll-back is idempotent.

DROP INDEX IF EXISTS idx_object_history_tx_id;
ALTER TABLE object_history
    DROP COLUMN IF EXISTS tx_id;

DROP INDEX IF EXISTS idx_dataset_transactions_ontology_committed;
DROP TABLE IF EXISTS dataset_transactions;
