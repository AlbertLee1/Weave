-- US-379 Time-Travel Queries (Dataset transaction chain).
--
-- Records a one-row-per-EditBatch chain so a SDK caller can pin a load to
-- ?asOf=tx-<id>. The funnel consumer writes a row inside applyBatchWithHistory
-- after a successful index commit; parent_tx_id points at the previous
-- transaction for the same ontology (NULL for the first one). committed_at
-- mirrors the EditBatch.Timestamp so the ?asOf=tx-... lookup can resolve to
-- a concrete RFC3339 instant and reuse the existing US-223 history-snapshot
-- machinery.
--
-- tx_id has format "tx-<uuid>" so it stays parseable by the OSS asOf parser.
-- Numeric / generated tx ids would also work but the human-friendly prefix
-- is what the PRD acceptance criterion demands ("?asOf=tx-...").
--
-- ontology_api_name is denormalised onto the row (rather than ontology_rid)
-- so the funnel consumer — which already knows the API name from the
-- EditBatch.OntologyAPIName field — can record without a separate ontology
-- lookup. The /datasets/{rid}/history endpoint resolves the URL {rid} to an
-- API name via the existing OMS GetOntology path before querying.

CREATE TABLE IF NOT EXISTS dataset_transactions (
    tx_id              TEXT        PRIMARY KEY,
    parent_tx_id       TEXT        REFERENCES dataset_transactions(tx_id) ON DELETE SET NULL,
    ontology_api_name  TEXT        NOT NULL,
    committed_at       TIMESTAMPTZ NOT NULL,
    edits_count        INTEGER     NOT NULL DEFAULT 0,
    user_id            TEXT
);

CREATE INDEX IF NOT EXISTS idx_dataset_transactions_ontology_committed
    ON dataset_transactions (ontology_api_name, committed_at DESC, tx_id DESC);

-- tx_id back-reference on object_history. Lets ?asOf=tx-... resolve to the
-- precise [valid_from, valid_to) interval that was open when this tx
-- committed without an extra timestamp comparison. NULL for legacy rows.
ALTER TABLE object_history
    ADD COLUMN IF NOT EXISTS tx_id TEXT;

CREATE INDEX IF NOT EXISTS idx_object_history_tx_id
    ON object_history (tx_id) WHERE tx_id IS NOT NULL;
