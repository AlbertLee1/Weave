package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liyang/weave/pkg/cellsec"
	"github.com/liyang/weave/pkg/masking"
)

// pgCellMaskStore implements cellsec.Store over the cell_masks table.
// Lives in cmd/server/ so pkg/cellsec stays free of pgx and its tests run
// with an in-memory store. Same shape as pgColumnMaskStore.
type pgCellMaskStore struct {
	pool *pgxpool.Pool
}

func newPGCellMaskStore(pool *pgxpool.Pool) *pgCellMaskStore {
	return &pgCellMaskStore{pool: pool}
}

func (s *pgCellMaskStore) Create(ctx context.Context, m *cellsec.CellMask) error {
	if err := m.Validate(); err != nil {
		return err
	}
	appliesJSON, err := json.Marshal(m.AppliesTo)
	if err != nil {
		return fmt.Errorf("cellsec: encode appliesTo: %w", err)
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO cell_masks
		   (rid, object_type_rid, primary_key, property_api_name, mask_rule, applies_to, description, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		m.RID, m.ObjectTypeRID, m.PrimaryKey, m.PropertyAPIName, string(m.MaskRule), appliesJSON, m.Description, m.CreatedBy,
	)
	return err
}

func (s *pgCellMaskStore) Get(ctx context.Context, rid string) (*cellsec.CellMask, error) {
	return s.scanOne(ctx,
		`SELECT rid, object_type_rid, primary_key, property_api_name, mask_rule, applies_to, description, created_by, created_at, updated_at
		 FROM cell_masks WHERE rid = $1`, rid)
}

func (s *pgCellMaskStore) List(ctx context.Context) ([]*cellsec.CellMask, error) {
	return s.scanMany(ctx,
		`SELECT rid, object_type_rid, primary_key, property_api_name, mask_rule, applies_to, description, created_by, created_at, updated_at
		 FROM cell_masks ORDER BY created_at ASC`)
}

func (s *pgCellMaskStore) ListByObjectType(ctx context.Context, objectTypeRID string) ([]*cellsec.CellMask, error) {
	return s.scanMany(ctx,
		`SELECT rid, object_type_rid, primary_key, property_api_name, mask_rule, applies_to, description, created_by, created_at, updated_at
		 FROM cell_masks WHERE object_type_rid = $1 ORDER BY created_at ASC`, objectTypeRID)
}

func (s *pgCellMaskStore) Update(ctx context.Context, rid string, upd cellsec.CellMaskUpdate) (*cellsec.CellMask, error) {
	args := []interface{}{}
	sets := []string{"updated_at = NOW()"}
	argN := 1
	if upd.MaskRule != nil {
		sets = append(sets, "mask_rule = $"+strconv.Itoa(argN))
		args = append(args, string(*upd.MaskRule))
		argN++
	}
	if upd.AppliesTo != nil {
		blob, err := json.Marshal(*upd.AppliesTo)
		if err != nil {
			return nil, fmt.Errorf("cellsec: encode appliesTo: %w", err)
		}
		sets = append(sets, "applies_to = $"+strconv.Itoa(argN))
		args = append(args, blob)
		argN++
	}
	if upd.Description != nil {
		sets = append(sets, "description = $"+strconv.Itoa(argN))
		args = append(args, *upd.Description)
		argN++
	}
	args = append(args, rid)
	tag, err := s.pool.Exec(ctx,
		`UPDATE cell_masks SET `+strings.Join(sets, ", ")+` WHERE rid = $`+strconv.Itoa(argN),
		args...)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, cellsec.ErrNotFound
	}
	return s.Get(ctx, rid)
}

func (s *pgCellMaskStore) Delete(ctx context.Context, rid string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM cell_masks WHERE rid = $1`, rid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return cellsec.ErrNotFound
	}
	return nil
}

func (s *pgCellMaskStore) scanOne(ctx context.Context, sql string, args ...interface{}) (*cellsec.CellMask, error) {
	row := s.pool.QueryRow(ctx, sql, args...)
	var (
		m          cellsec.CellMask
		rule       string
		appliesRaw []byte
		createdAt  time.Time
		updatedAt  time.Time
	)
	err := row.Scan(&m.RID, &m.ObjectTypeRID, &m.PrimaryKey, &m.PropertyAPIName, &rule, &appliesRaw, &m.Description, &m.CreatedBy, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, cellsec.ErrNotFound
		}
		return nil, err
	}
	m.MaskRule = masking.MaskRule(rule)
	if err := json.Unmarshal(appliesRaw, &m.AppliesTo); err != nil {
		return nil, fmt.Errorf("cellsec: decode appliesTo: %w", err)
	}
	m.CreatedAt = createdAt
	m.UpdatedAt = updatedAt
	return &m, nil
}

func (s *pgCellMaskStore) scanMany(ctx context.Context, sql string, args ...interface{}) ([]*cellsec.CellMask, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*cellsec.CellMask
	for rows.Next() {
		var (
			m          cellsec.CellMask
			rule       string
			appliesRaw []byte
			createdAt  time.Time
			updatedAt  time.Time
		)
		if err := rows.Scan(&m.RID, &m.ObjectTypeRID, &m.PrimaryKey, &m.PropertyAPIName, &rule, &appliesRaw, &m.Description, &m.CreatedBy, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		m.MaskRule = masking.MaskRule(rule)
		if err := json.Unmarshal(appliesRaw, &m.AppliesTo); err != nil {
			return nil, fmt.Errorf("cellsec: decode appliesTo: %w", err)
		}
		m.CreatedAt = createdAt
		m.UpdatedAt = updatedAt
		out = append(out, &m)
	}
	return out, rows.Err()
}
