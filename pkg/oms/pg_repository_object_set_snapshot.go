package oms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// CreateObjectSetSnapshot inserts an object_set_snapshots row, marshalling
// PrimaryKeys as JSONB. Definition is taken as-is (already json.RawMessage)
// so callers control whether they store a canonical-form Definition or the
// raw client payload.
func (r *PGRepository) CreateObjectSetSnapshot(ctx context.Context, snap *ObjectSetSnapshot) error {
	if err := snap.Validate(); err != nil {
		return fmt.Errorf("create object set snapshot: %w", err)
	}
	pkJSON, err := json.Marshal(snap.PrimaryKeys)
	if err != nil {
		return fmt.Errorf("marshal primary keys: %w", err)
	}
	def := snap.Definition
	if len(def) == 0 {
		def = json.RawMessage("null")
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO object_set_snapshots (rid, ontology_api_name, object_type,
		 definition, primary_keys, truncated, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		snap.RID, snap.OntologyAPIName, snap.ObjectType,
		[]byte(def), pkJSON, snap.Truncated, snap.CreatedBy)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

// GetObjectSetSnapshot fetches a single object_set_snapshots row by rid.
// Returns ErrNotFound when no row matches so handlers can map cleanly to a
// 404 SnapshotNotFound response.
func (r *PGRepository) GetObjectSetSnapshot(ctx context.Context, rid string) (*ObjectSetSnapshot, error) {
	snap := &ObjectSetSnapshot{}
	var defJSON, pkJSON []byte
	err := r.pool.QueryRow(ctx,
		`SELECT rid, ontology_api_name, object_type, definition, primary_keys,
		 truncated, created_by, created_at
		 FROM object_set_snapshots WHERE rid = $1`, rid).
		Scan(&snap.RID, &snap.OntologyAPIName, &snap.ObjectType, &defJSON, &pkJSON,
			&snap.Truncated, &snap.CreatedBy, &snap.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	snap.Definition = json.RawMessage(defJSON)
	if len(pkJSON) > 0 {
		if err := json.Unmarshal(pkJSON, &snap.PrimaryKeys); err != nil {
			return nil, fmt.Errorf("unmarshal primary keys for %q: %w", rid, err)
		}
	}
	return snap, nil
}
