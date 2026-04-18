package oms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// CreateInterfaceMethod inserts a single interface_methods row. Params and
// returns are marshalled into JSONB at the boundary so the Go model stays
// typed and the persistence shape remains arbitrary JSON (lets future
// iterations extend the return/param payload without a migration).
func (r *PGRepository) CreateInterfaceMethod(ctx context.Context, im *InterfaceMethod) error {
	if err := im.Validate(); err != nil {
		return fmt.Errorf("create interface method: %w", err)
	}
	params, err := json.Marshal(im.Params)
	if err != nil {
		return fmt.Errorf("marshal params: %w", err)
	}
	if len(im.Params) == 0 {
		params = []byte(`[]`)
	}
	returns, err := json.Marshal(im.Returns)
	if err != nil {
		return fmt.Errorf("marshal returns: %w", err)
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO interface_methods
		 (rid, interface_rid, name, params, returns, description)
		 VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))`,
		im.RID, im.InterfaceRID, im.Name, params, returns, im.Description)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

// GetInterfaceMethod fetches a single interface_methods row by rid.
func (r *PGRepository) GetInterfaceMethod(ctx context.Context, rid string) (*InterfaceMethod, error) {
	im := &InterfaceMethod{}
	var params, returns []byte
	err := r.pool.QueryRow(ctx,
		`SELECT rid, interface_rid, name, params, returns, COALESCE(description, ''), created_at
		 FROM interface_methods WHERE rid = $1`, rid).
		Scan(&im.RID, &im.InterfaceRID, &im.Name, &params, &returns, &im.Description, &im.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := decodeInterfaceMethodJSONB(im, params, returns); err != nil {
		return nil, err
	}
	return im, nil
}

// ListInterfaceMethods returns every method declared on the given Interface,
// ordered by name for deterministic test observations.
func (r *PGRepository) ListInterfaceMethods(ctx context.Context, interfaceRID string) ([]InterfaceMethod, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT rid, interface_rid, name, params, returns, COALESCE(description, ''), created_at
		 FROM interface_methods WHERE interface_rid = $1 ORDER BY name`, interfaceRID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []InterfaceMethod
	for rows.Next() {
		var im InterfaceMethod
		var params, returns []byte
		if err := rows.Scan(&im.RID, &im.InterfaceRID, &im.Name, &params, &returns, &im.Description, &im.CreatedAt); err != nil {
			return nil, err
		}
		if err := decodeInterfaceMethodJSONB(&im, params, returns); err != nil {
			return nil, err
		}
		out = append(out, im)
	}
	return out, rows.Err()
}

// UpdateInterfaceMethod rewrites the mutable fields of an interface_methods
// row. RID and interface_rid are immutable.
func (r *PGRepository) UpdateInterfaceMethod(ctx context.Context, im *InterfaceMethod) error {
	if err := im.Validate(); err != nil {
		return fmt.Errorf("update interface method: %w", err)
	}
	params, err := json.Marshal(im.Params)
	if err != nil {
		return fmt.Errorf("marshal params: %w", err)
	}
	if len(im.Params) == 0 {
		params = []byte(`[]`)
	}
	returns, err := json.Marshal(im.Returns)
	if err != nil {
		return fmt.Errorf("marshal returns: %w", err)
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE interface_methods
		 SET name = $1, params = $2, returns = $3, description = NULLIF($4, '')
		 WHERE rid = $5`,
		im.Name, params, returns, im.Description, im.RID)
	if err != nil {
		return wrapPGError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteInterfaceMethod removes an interface_methods row. ActionTypes with
// `implements_method_rid` pointing at the deleted method retain their
// pointer (it goes stale) — a sweep/cleanup admin pass is the caller's job,
// analogous to how link_edges survives link_property deletion.
func (r *PGRepository) DeleteInterfaceMethod(ctx context.Context, rid string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM interface_methods WHERE rid = $1`, rid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func decodeInterfaceMethodJSONB(im *InterfaceMethod, params, returns []byte) error {
	if len(params) > 0 {
		if err := json.Unmarshal(params, &im.Params); err != nil {
			return fmt.Errorf("unmarshal params: %w", err)
		}
	}
	if len(returns) > 0 && string(returns) != "null" {
		if err := json.Unmarshal(returns, &im.Returns); err != nil {
			return fmt.Errorf("unmarshal returns: %w", err)
		}
	}
	return nil
}
