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

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/rls"
)

// pgRowPolicyStore implements rls.Store over the row_policies table.
// Lives in cmd/server/ (not pkg/rls) so pkg/rls stays free of pgx and its
// tests can run with an in-memory store. Same shape as pgActionJobStore /
// interface_method_dispatcher.
type pgRowPolicyStore struct {
	pool *pgxpool.Pool
}

func newPGRowPolicyStore(pool *pgxpool.Pool) *pgRowPolicyStore {
	return &pgRowPolicyStore{pool: pool}
}

func (s *pgRowPolicyStore) Create(ctx context.Context, p *rls.RowPolicy) error {
	if err := p.Validate(); err != nil {
		return err
	}
	appliesJSON, err := json.Marshal(p.AppliesTo)
	if err != nil {
		return fmt.Errorf("rls: encode appliesTo: %w", err)
	}
	predicate := coerceRLSPredicate(p.Predicate)
	_, err = s.pool.Exec(ctx,
		`INSERT INTO row_policies
		   (rid, object_type_rid, predicate, cel_expression, applies_to, description, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		p.RID, p.ObjectTypeRID, predicate, p.CELExpression, appliesJSON, p.Description, p.CreatedBy,
	)
	return err
}

func (s *pgRowPolicyStore) Get(ctx context.Context, rid string) (*rls.RowPolicy, error) {
	return s.scanOne(ctx,
		`SELECT rid, object_type_rid, predicate, cel_expression, applies_to, description, created_by, created_at, updated_at
		 FROM row_policies WHERE rid = $1`, rid)
}

func (s *pgRowPolicyStore) List(ctx context.Context) ([]*rls.RowPolicy, error) {
	return s.scanMany(ctx,
		`SELECT rid, object_type_rid, predicate, cel_expression, applies_to, description, created_by, created_at, updated_at
		 FROM row_policies ORDER BY created_at ASC`)
}

func (s *pgRowPolicyStore) ListByObjectType(ctx context.Context, objectTypeRID string) ([]*rls.RowPolicy, error) {
	return s.scanMany(ctx,
		`SELECT rid, object_type_rid, predicate, cel_expression, applies_to, description, created_by, created_at, updated_at
		 FROM row_policies WHERE object_type_rid = $1 ORDER BY created_at ASC`, objectTypeRID)
}

func (s *pgRowPolicyStore) Update(ctx context.Context, rid string, upd rls.RowPolicyUpdate) (*rls.RowPolicy, error) {
	args := []interface{}{}
	sets := []string{"updated_at = NOW()"}
	argN := 1
	if upd.Predicate != nil {
		sets = append(sets, "predicate = $"+strconv.Itoa(argN))
		args = append(args, coerceRLSPredicate(*upd.Predicate))
		argN++
	}
	if upd.CELExpression != nil {
		sets = append(sets, "cel_expression = $"+strconv.Itoa(argN))
		args = append(args, *upd.CELExpression)
		argN++
	}
	if upd.AppliesTo != nil {
		blob, err := json.Marshal(*upd.AppliesTo)
		if err != nil {
			return nil, fmt.Errorf("rls: encode appliesTo: %w", err)
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
		`UPDATE row_policies SET `+strings.Join(sets, ", ")+` WHERE rid = $`+strconv.Itoa(argN),
		args...)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, rls.ErrNotFound
	}
	return s.Get(ctx, rid)
}

func (s *pgRowPolicyStore) Delete(ctx context.Context, rid string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM row_policies WHERE rid = $1`, rid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return rls.ErrNotFound
	}
	return nil
}

func (s *pgRowPolicyStore) scanOne(ctx context.Context, sql string, args ...interface{}) (*rls.RowPolicy, error) {
	row := s.pool.QueryRow(ctx, sql, args...)
	var (
		p          rls.RowPolicy
		predicate  []byte
		celExpr    string
		appliesRaw []byte
		createdAt  time.Time
		updatedAt  time.Time
	)
	err := row.Scan(&p.RID, &p.ObjectTypeRID, &predicate, &celExpr, &appliesRaw, &p.Description, &p.CreatedBy, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, rls.ErrNotFound
		}
		return nil, err
	}
	p.Predicate = nullablePredicate(predicate)
	p.CELExpression = celExpr
	if err := json.Unmarshal(appliesRaw, &p.AppliesTo); err != nil {
		return nil, fmt.Errorf("rls: decode appliesTo: %w", err)
	}
	p.CreatedAt = createdAt
	p.UpdatedAt = updatedAt
	return &p, nil
}

func (s *pgRowPolicyStore) scanMany(ctx context.Context, sql string, args ...interface{}) ([]*rls.RowPolicy, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*rls.RowPolicy
	for rows.Next() {
		var (
			p          rls.RowPolicy
			predicate  []byte
			celExpr    string
			appliesRaw []byte
			createdAt  time.Time
			updatedAt  time.Time
		)
		if err := rows.Scan(&p.RID, &p.ObjectTypeRID, &predicate, &celExpr, &appliesRaw, &p.Description, &p.CreatedBy, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		p.Predicate = nullablePredicate(predicate)
		p.CELExpression = celExpr
		if err := json.Unmarshal(appliesRaw, &p.AppliesTo); err != nil {
			return nil, fmt.Errorf("rls: decode appliesTo: %w", err)
		}
		p.CreatedAt = createdAt
		p.UpdatedAt = updatedAt
		out = append(out, &p)
	}
	return out, rows.Err()
}

// coerceRLSPredicate substitutes nil for the wire-side "no predicate" case so
// the JSONB column can stay NULL when only a CEL gate is supplied. Pre-US-487
// callers always passed a non-empty WhereClause; CEL-only callers send nothing.
// Returning untyped nil lets pgx encode an SQL NULL — a literal []byte("null")
// would re-trip the json decoder on the read path with "unexpected end of JSON
// input" once we filter CEL-only policies out of the Bleve compile lane.
func coerceRLSPredicate(b []byte) []byte {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	return b
}

// nullablePredicate normalises the read-side: a NULL row column lands as a
// nil byte slice, and the legacy DEFAULT '{}'::jsonb shape from rows that
// were created before the CEL migration is treated as "no predicate". Either
// shape becomes a nil json.RawMessage on the RowPolicy struct, which the
// engine's CEL-aware Compile path skips cleanly.
func nullablePredicate(b []byte) []byte {
	if len(b) == 0 || string(b) == "null" || string(b) == "{}" {
		return nil
	}
	return b
}

// groupLookupFromRepo adapts auth.GroupRepository to rls.GroupMembershipLookup.
// The engine only needs "userID → group names"; the full group repo surface
// is unnecessary, so we wrap in a narrow type rather than type-asserting.
type groupLookupFromRepo struct {
	repo auth.GroupRepository
}

func newGroupLookupFromRepo(repo auth.GroupRepository) *groupLookupFromRepo {
	return &groupLookupFromRepo{repo: repo}
}

func (g *groupLookupFromRepo) UserGroups(ctx context.Context, userID string) ([]string, error) {
	if g == nil || g.repo == nil || userID == "" {
		return nil, nil
	}
	return g.repo.ListUserGroups(ctx, userID)
}
