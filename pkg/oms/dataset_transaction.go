package oms

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// DatasetTransactionIDPrefix is the canonical prefix for tx_id values. The
// OSS asOf parser keys off this prefix to disambiguate between an RFC3339
// timestamp (US-223) and a transaction-id lookup (US-379), so producers must
// always emit the prefix and consumers must never strip it.
const DatasetTransactionIDPrefix = "tx-"

// DatasetTransaction is one row of the dataset_transactions table (US-379).
// Each successful EditBatch processed by the funnel consumer yields one row;
// ParentTxID points at the previous transaction recorded for the same
// OntologyAPIName, NULL for the first one. The chain is per-ontology so a
// /datasets/{rid}/history call can walk it back to the genesis tx without
// crossing ontology boundaries.
//
// CommittedAt mirrors the EditBatch.Timestamp so an ?asOf=tx-... query on
// the OSS load handler can resolve to a concrete RFC3339 instant and reuse
// the existing US-223 history-snapshot scan against the [valid_from,
// valid_to) interval. EditsCount is purely informational — it lets the
// /history endpoint surface a per-row "how many objects did this tx touch"
// without re-counting object_history.
type DatasetTransaction struct {
	TxID            string    `json:"txId"`
	ParentTxID      string    `json:"parentTxId,omitempty"`
	OntologyAPIName string    `json:"ontologyApiName"`
	CommittedAt     time.Time `json:"committedAt"`
	EditsCount      int       `json:"editsCount"`
	UserID          string    `json:"userId,omitempty"`
}

// Validate rejects rows the persistence layer could not interpret. Called
// by RecordDatasetTransaction before the INSERT so a malformed tx_id (no
// "tx-" prefix, empty ontology) trips at the boundary instead of silently
// landing a row the asOf parser cannot find.
func (t DatasetTransaction) Validate() error {
	if t.TxID == "" {
		return fmt.Errorf("dataset transaction requires txId")
	}
	if !strings.HasPrefix(t.TxID, DatasetTransactionIDPrefix) {
		return fmt.Errorf("dataset transaction txId must start with %q (got %q)",
			DatasetTransactionIDPrefix, t.TxID)
	}
	if t.OntologyAPIName == "" {
		return fmt.Errorf("dataset transaction requires ontologyApiName")
	}
	if t.CommittedAt.IsZero() {
		return fmt.Errorf("dataset transaction requires committedAt")
	}
	return nil
}

// DatasetTransactionStore is the narrow read/write surface the funnel
// consumer + /datasets/{rid}/history handler depend on. Kept outside
// Repository for the same reason as ColumnLineageStore / SagaStore — the
// many mock repos in the test tree would otherwise need stub methods for
// a row type they do not exercise.
//
// LatestForOntology returns the most-recent committed transaction for the
// given OntologyAPIName, or (nil, nil) when none exist. Used by the funnel
// consumer to resolve ParentTxID before INSERT so the chain stays linear
// without an extra round-trip on every batch.
//
// ListByOntology returns rows for the ontology in committed_at-DESC order
// up to limit (0 = no limit). limit is for response-size sanity rather
// than pagination; the chain is short enough in single-machine deployments
// that a simple cap suffices.
type DatasetTransactionStore interface {
	RecordDatasetTransaction(ctx context.Context, tx *DatasetTransaction) error
	GetDatasetTransaction(ctx context.Context, txID string) (*DatasetTransaction, error)
	LatestForOntology(ctx context.Context, ontologyAPIName string) (*DatasetTransaction, error)
	ListByOntology(ctx context.Context, ontologyAPIName string, limit int) ([]DatasetTransaction, error)
}
