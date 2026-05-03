package oms

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// defaultColumnLineageListLimit caps the per-query row count when
// callers omit (or pass a non-positive) limit so a runaway list call
// cannot drag the whole table back. 200 mirrors defaultLineageListLimit.
const defaultColumnLineageListLimit = 200

// ReplaceColumnLineageForBinding atomically clears every edge owned by
// bindingRID and inserts the supplied set inside a single transaction so
// concurrent reads never observe a partial replacement. Each inserted
// row's ID + Timestamp are back-filled.
func (r *PGRepository) ReplaceColumnLineageForBinding(ctx context.Context, bindingRID string, edges []ColumnLineageEdge) error {
	if bindingRID == "" {
		return errors.New("oms: bindingRID is required for ReplaceColumnLineageForBinding")
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`DELETE FROM lineage_column_edges WHERE binding_rid = $1`, bindingRID); err != nil {
		return fmt.Errorf("oms: clear column lineage for binding: %w", err)
	}

	for i := range edges {
		e := &edges[i]
		// Stamp the binding RID from the explicit parameter so callers can
		// not mix owners by accident.
		e.BindingRID = bindingRID
		var id int64
		var ts = e.Timestamp
		if !ts.IsZero() {
			err = tx.QueryRow(ctx,
				`INSERT INTO lineage_column_edges (
					binding_rid, src_dataset_rid, src_column,
					dst_object_type_rid, dst_property_rid, dst_property_api_name, ts)
				 VALUES ($1, $2, $3, $4, $5, $6, $7)
				 RETURNING id, ts`,
				e.BindingRID, e.SrcDatasetRID, e.SrcColumn,
				e.DstObjectTypeRID, e.DstPropertyRID, e.DstPropertyAPIName, ts).
				Scan(&id, &ts)
		} else {
			err = tx.QueryRow(ctx,
				`INSERT INTO lineage_column_edges (
					binding_rid, src_dataset_rid, src_column,
					dst_object_type_rid, dst_property_rid, dst_property_api_name)
				 VALUES ($1, $2, $3, $4, $5, $6)
				 RETURNING id, ts`,
				e.BindingRID, e.SrcDatasetRID, e.SrcColumn,
				e.DstObjectTypeRID, e.DstPropertyRID, e.DstPropertyAPIName).
				Scan(&id, &ts)
		}
		if err != nil {
			return fmt.Errorf("oms: insert column lineage edge: %w", err)
		}
		e.ID = id
		e.Timestamp = ts
	}
	return tx.Commit(ctx)
}

// DeleteColumnLineageForBinding removes every edge owned by bindingRID
// and returns the number of rows actually removed. A binding that never
// produced edges yields (0, nil) — never an error.
func (r *PGRepository) DeleteColumnLineageForBinding(ctx context.Context, bindingRID string) (int64, error) {
	if bindingRID == "" {
		return 0, errors.New("oms: bindingRID is required for DeleteColumnLineageForBinding")
	}
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM lineage_column_edges WHERE binding_rid = $1`, bindingRID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ListUpstreamColumnLineageForProperty returns the most recent up-to
// limit edges whose dst_property_rid matches. Newest-first by ts (then
// id as tiebreaker for stable ordering inside a single TIMESTAMPTZ tick).
func (r *PGRepository) ListUpstreamColumnLineageForProperty(ctx context.Context, propertyRID string, limit int) ([]ColumnLineageEdge, error) {
	if limit <= 0 {
		limit = defaultColumnLineageListLimit
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, binding_rid, src_dataset_rid, src_column,
		        dst_object_type_rid, dst_property_rid, dst_property_api_name, ts
		 FROM lineage_column_edges
		 WHERE dst_property_rid = $1
		 ORDER BY ts DESC, id DESC
		 LIMIT $2`,
		propertyRID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanColumnLineageEdges(rows)
}

// ListDownstreamColumnLineageForDatasetColumn is the reverse-impact
// path — given a (dataset_rid, column) pair, list every downstream
// property that derives from it. Newest-first by ts.
func (r *PGRepository) ListDownstreamColumnLineageForDatasetColumn(ctx context.Context, datasetRID, column string, limit int) ([]ColumnLineageEdge, error) {
	if limit <= 0 {
		limit = defaultColumnLineageListLimit
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, binding_rid, src_dataset_rid, src_column,
		        dst_object_type_rid, dst_property_rid, dst_property_api_name, ts
		 FROM lineage_column_edges
		 WHERE src_dataset_rid = $1 AND src_column = $2
		 ORDER BY ts DESC, id DESC
		 LIMIT $3`,
		datasetRID, column, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanColumnLineageEdges(rows)
}

// scanColumnLineageEdges drains a *pgx.Rows into a []ColumnLineageEdge.
// Mirrors scanLineageEdges' interface-friendly shape so a future test
// substitutable variant stays cheap.
func scanColumnLineageEdges(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]ColumnLineageEdge, error) {
	var out []ColumnLineageEdge
	for rows.Next() {
		var e ColumnLineageEdge
		if err := rows.Scan(
			&e.ID, &e.BindingRID, &e.SrcDatasetRID, &e.SrcColumn,
			&e.DstObjectTypeRID, &e.DstPropertyRID, &e.DstPropertyAPIName, &e.Timestamp,
		); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
