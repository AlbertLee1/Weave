package oms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// CreateComputedProperty inserts a computed_properties row, marshalling the
// aggregation spec as JSONB.
func (r *PGRepository) CreateComputedProperty(ctx context.Context, cp *ComputedProperty) error {
	if err := cp.Validate(); err != nil {
		return fmt.Errorf("create computed property: %w", err)
	}
	aggJSON, err := json.Marshal(cp.Aggregation)
	if err != nil {
		return fmt.Errorf("marshal aggregation: %w", err)
	}
	ttl := cp.CacheTTLSeconds
	if ttl == 0 {
		ttl = int(DefaultComputedPropertyCacheTTL.Seconds())
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO computed_properties (rid, object_type_rid, api_name, display_name, description,
		 source_link_rid, aggregation, cache_ttl_seconds)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		cp.RID, cp.ObjectTypeRID, cp.APIName, cp.DisplayName, cp.Description,
		cp.SourceLinkRID, aggJSON, ttl)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

// GetComputedProperty fetches a single computed_properties row by rid.
func (r *PGRepository) GetComputedProperty(ctx context.Context, rid string) (*ComputedProperty, error) {
	cp := &ComputedProperty{}
	var aggJSON []byte
	err := r.pool.QueryRow(ctx,
		`SELECT rid, object_type_rid, api_name, display_name, description,
		 source_link_rid, aggregation, cache_ttl_seconds, created_at
		 FROM computed_properties WHERE rid = $1`, rid).
		Scan(&cp.RID, &cp.ObjectTypeRID, &cp.APIName, &cp.DisplayName, &cp.Description,
			&cp.SourceLinkRID, &aggJSON, &cp.CacheTTLSeconds, &cp.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := json.Unmarshal(aggJSON, &cp.Aggregation); err != nil {
		return nil, fmt.Errorf("unmarshal aggregation for %q: %w", rid, err)
	}
	return cp, nil
}

// ListComputedProperties returns every computed property defined on the given
// ObjectType, ordered by api_name so tests observe a stable shape.
func (r *PGRepository) ListComputedProperties(ctx context.Context, objectTypeRID string) ([]ComputedProperty, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT rid, object_type_rid, api_name, display_name, description,
		 source_link_rid, aggregation, cache_ttl_seconds, created_at
		 FROM computed_properties WHERE object_type_rid = $1 ORDER BY api_name`, objectTypeRID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ComputedProperty
	for rows.Next() {
		var cp ComputedProperty
		var aggJSON []byte
		if err := rows.Scan(&cp.RID, &cp.ObjectTypeRID, &cp.APIName, &cp.DisplayName, &cp.Description,
			&cp.SourceLinkRID, &aggJSON, &cp.CacheTTLSeconds, &cp.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(aggJSON, &cp.Aggregation); err != nil {
			return nil, fmt.Errorf("unmarshal aggregation for %q: %w", cp.RID, err)
		}
		out = append(out, cp)
	}
	return out, nil
}

// UpdateComputedProperty updates the mutable fields of a computed property
// row. RID and object_type_rid are treated as immutable.
func (r *PGRepository) UpdateComputedProperty(ctx context.Context, cp *ComputedProperty) error {
	if err := cp.Validate(); err != nil {
		return fmt.Errorf("update computed property: %w", err)
	}
	aggJSON, err := json.Marshal(cp.Aggregation)
	if err != nil {
		return fmt.Errorf("marshal aggregation: %w", err)
	}
	ttl := cp.CacheTTLSeconds
	if ttl == 0 {
		ttl = int(DefaultComputedPropertyCacheTTL.Seconds())
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE computed_properties
		 SET api_name = $1, display_name = $2, description = $3,
		     source_link_rid = $4, aggregation = $5, cache_ttl_seconds = $6
		 WHERE rid = $7`,
		cp.APIName, cp.DisplayName, cp.Description,
		cp.SourceLinkRID, aggJSON, ttl, cp.RID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteComputedProperty removes a computed property row. Cache invalidation
// is the caller's responsibility — the Resolver exposes InvalidateAll for
// that purpose.
func (r *PGRepository) DeleteComputedProperty(ctx context.Context, rid string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM computed_properties WHERE rid = $1`, rid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
