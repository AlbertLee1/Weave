package oms

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// CreateLinkProperty inserts a link_properties row. Description uses the
// NULLIF($N, '') pattern so blank strings round-trip as proper NULLs; the
// model keeps a plain `string` (no sql.NullString).
func (r *PGRepository) CreateLinkProperty(ctx context.Context, lp *LinkProperty) error {
	if err := lp.Validate(); err != nil {
		return fmt.Errorf("create link property: %w", err)
	}
	var typeConfig interface{}
	if len(lp.TypeConfig) > 0 {
		typeConfig = []byte(lp.TypeConfig)
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO link_properties
		 (rid, link_type_rid, api_name, display_name, description, base_type, type_config, is_array, is_nullable)
		 VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7, $8, $9)`,
		lp.RID, lp.LinkTypeRID, lp.APIName, lp.DisplayName, lp.Description,
		lp.BaseType, typeConfig, lp.IsArray, lp.IsNullable)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

// GetLinkProperty fetches a single link_properties row by rid.
func (r *PGRepository) GetLinkProperty(ctx context.Context, rid string) (*LinkProperty, error) {
	lp := &LinkProperty{}
	var typeConfig []byte
	err := r.pool.QueryRow(ctx,
		`SELECT rid, link_type_rid, api_name, display_name, COALESCE(description, ''),
		 base_type, type_config, is_array, is_nullable, created_at
		 FROM link_properties WHERE rid = $1`, rid).
		Scan(&lp.RID, &lp.LinkTypeRID, &lp.APIName, &lp.DisplayName, &lp.Description,
			&lp.BaseType, &typeConfig, &lp.IsArray, &lp.IsNullable, &lp.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if len(typeConfig) > 0 {
		lp.TypeConfig = typeConfig
	}
	return lp, nil
}

// ListLinkProperties returns every link property declared on the given
// LinkType, ordered by api_name for deterministic test observations.
func (r *PGRepository) ListLinkProperties(ctx context.Context, linkTypeRID string) ([]LinkProperty, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT rid, link_type_rid, api_name, display_name, COALESCE(description, ''),
		 base_type, type_config, is_array, is_nullable, created_at
		 FROM link_properties WHERE link_type_rid = $1 ORDER BY api_name`, linkTypeRID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LinkProperty
	for rows.Next() {
		var lp LinkProperty
		var typeConfig []byte
		if err := rows.Scan(&lp.RID, &lp.LinkTypeRID, &lp.APIName, &lp.DisplayName, &lp.Description,
			&lp.BaseType, &typeConfig, &lp.IsArray, &lp.IsNullable, &lp.CreatedAt); err != nil {
			return nil, err
		}
		if len(typeConfig) > 0 {
			lp.TypeConfig = typeConfig
		}
		out = append(out, lp)
	}
	return out, rows.Err()
}

// UpdateLinkProperty updates the mutable fields of a link property. RID and
// link_type_rid are immutable.
func (r *PGRepository) UpdateLinkProperty(ctx context.Context, lp *LinkProperty) error {
	if err := lp.Validate(); err != nil {
		return fmt.Errorf("update link property: %w", err)
	}
	var typeConfig interface{}
	if len(lp.TypeConfig) > 0 {
		typeConfig = []byte(lp.TypeConfig)
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE link_properties
		 SET api_name = $1, display_name = $2, description = NULLIF($3, ''),
		     base_type = $4, type_config = $5, is_array = $6, is_nullable = $7
		 WHERE rid = $8`,
		lp.APIName, lp.DisplayName, lp.Description,
		lp.BaseType, typeConfig, lp.IsArray, lp.IsNullable, lp.RID)
	if err != nil {
		return wrapPGError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteLinkProperty removes a link property row. Edge values under this
// apiName that exist in link_edges.edge_properties are left intact — the
// caller may garbage-collect them explicitly if desired.
func (r *PGRepository) DeleteLinkProperty(ctx context.Context, rid string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM link_properties WHERE rid = $1`, rid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
