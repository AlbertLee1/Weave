package oms

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// RecordDatasetTransaction inserts a dataset_transactions row. Validates
// the input first so a malformed tx_id (no "tx-" prefix) trips at the
// boundary instead of landing a row the asOf parser cannot find.
func (r *PGRepository) RecordDatasetTransaction(ctx context.Context, tx *DatasetTransaction) error {
	if err := tx.Validate(); err != nil {
		return fmt.Errorf("record dataset transaction: %w", err)
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO dataset_transactions
		   (tx_id, parent_tx_id, ontology_api_name, committed_at, edits_count, user_id)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (tx_id) DO NOTHING`,
		tx.TxID, nilIfEmpty(tx.ParentTxID), tx.OntologyAPIName,
		tx.CommittedAt, tx.EditsCount, nilIfEmpty(tx.UserID))
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

// GetDatasetTransaction fetches a single dataset_transactions row by tx_id.
// Returns ErrNotFound when no row matches so handlers can map cleanly to
// a 404 envelope. The OSS asOf parser uses this to translate ?asOf=tx-...
// into a concrete CommittedAt timestamp before delegating to the existing
// US-223 history-snapshot scan.
func (r *PGRepository) GetDatasetTransaction(ctx context.Context, txID string) (*DatasetTransaction, error) {
	tx := &DatasetTransaction{}
	var parent *string
	var userID *string
	err := r.pool.QueryRow(ctx,
		`SELECT tx_id, parent_tx_id, ontology_api_name, committed_at, edits_count, user_id
		   FROM dataset_transactions
		  WHERE tx_id = $1`, txID).
		Scan(&tx.TxID, &parent, &tx.OntologyAPIName, &tx.CommittedAt, &tx.EditsCount, &userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if parent != nil {
		tx.ParentTxID = *parent
	}
	if userID != nil {
		tx.UserID = *userID
	}
	return tx, nil
}

// LatestForOntology returns the most-recent committed transaction for the
// given OntologyAPIName, or (nil, nil) when none exist. Sorted by
// committed_at DESC with a tx_id DESC tiebreaker so two batches that share
// a millisecond timestamp still produce a stable chain.
func (r *PGRepository) LatestForOntology(ctx context.Context, ontologyAPIName string) (*DatasetTransaction, error) {
	tx := &DatasetTransaction{}
	var parent *string
	var userID *string
	err := r.pool.QueryRow(ctx,
		`SELECT tx_id, parent_tx_id, ontology_api_name, committed_at, edits_count, user_id
		   FROM dataset_transactions
		  WHERE ontology_api_name = $1
		  ORDER BY committed_at DESC, tx_id DESC
		  LIMIT 1`, ontologyAPIName).
		Scan(&tx.TxID, &parent, &tx.OntologyAPIName, &tx.CommittedAt, &tx.EditsCount, &userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if parent != nil {
		tx.ParentTxID = *parent
	}
	if userID != nil {
		tx.UserID = *userID
	}
	return tx, nil
}

// ListByOntology returns rows for the ontology in committed_at-DESC order,
// newest first. limit <= 0 means "no limit"; in single-machine deployments
// the chain stays short enough that a simple cap suffices and pagination
// is unnecessary.
func (r *PGRepository) ListByOntology(ctx context.Context, ontologyAPIName string, limit int) ([]DatasetTransaction, error) {
	q := `SELECT tx_id, parent_tx_id, ontology_api_name, committed_at, edits_count, user_id
		    FROM dataset_transactions
		   WHERE ontology_api_name = $1
		   ORDER BY committed_at DESC, tx_id DESC`
	args := []interface{}{ontologyAPIName}
	if limit > 0 {
		q += " LIMIT $2"
		args = append(args, limit)
	}
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DatasetTransaction
	for rows.Next() {
		var tx DatasetTransaction
		var parent *string
		var userID *string
		if err := rows.Scan(&tx.TxID, &parent, &tx.OntologyAPIName,
			&tx.CommittedAt, &tx.EditsCount, &userID); err != nil {
			return nil, err
		}
		if parent != nil {
			tx.ParentTxID = *parent
		}
		if userID != nil {
			tx.UserID = *userID
		}
		out = append(out, tx)
	}
	return out, rows.Err()
}
