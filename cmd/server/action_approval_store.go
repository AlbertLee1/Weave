package main

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liyang/weave/pkg/actions"
	"github.com/liyang/weave/pkg/oms"
)

// pgActionApprovalStore satisfies actions.ActionApprovalStore by persisting
// approval rows to the action_approvals table (US-242). Lives in cmd/server/
// so the dependency direction stays clean — pkg/oms cannot import pkg/actions
// (actions already imports oms). Same shape as pgActionJobStore /
// pgEdgePropertiesResolver.
type pgActionApprovalStore struct {
	pool *pgxpool.Pool
}

func newPGActionApprovalStore(pool *pgxpool.Pool) *pgActionApprovalStore {
	return &pgActionApprovalStore{pool: pool}
}

func (s *pgActionApprovalStore) CreateActionApproval(ctx context.Context, a *actions.ActionApproval) error {
	approvers, err := json.Marshal(a.Approvers)
	if err != nil {
		return err
	}
	if len(approvers) == 0 {
		approvers = []byte("[]")
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO action_approvals
		   (id, action_type_rid, ontology_api_name, action_type, parameters, approvers,
		    status, requested_by, reviewed_by, reason)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		a.ID, a.ActionTypeRID, a.OntologyAPIName, a.ActionType,
		coerceJSON(a.Parameters), approvers, a.Status,
		a.RequestedBy, a.ReviewedBy, a.Reason,
	)
	return err
}

func (s *pgActionApprovalStore) GetActionApproval(ctx context.Context, id string) (*actions.ActionApproval, error) {
	var a actions.ActionApproval
	var params, approvers []byte
	err := s.pool.QueryRow(ctx,
		`SELECT id, action_type_rid, ontology_api_name, action_type,
		        COALESCE(parameters, '{}'::jsonb),
		        COALESCE(approvers, '[]'::jsonb),
		        status, requested_by, reviewed_by, reason, created_at, updated_at
		 FROM action_approvals WHERE id = $1`, id).
		Scan(&a.ID, &a.ActionTypeRID, &a.OntologyAPIName, &a.ActionType,
			&params, &approvers, &a.Status,
			&a.RequestedBy, &a.ReviewedBy, &a.Reason,
			&a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oms.ErrNotFound
		}
		return nil, err
	}
	if len(params) > 0 && string(params) != "{}" {
		a.Parameters = params
	}
	if len(approvers) > 0 {
		var out []string
		if err := json.Unmarshal(approvers, &out); err == nil && len(out) > 0 {
			a.Approvers = out
		}
	}
	return &a, nil
}

func (s *pgActionApprovalStore) UpdateActionApproval(ctx context.Context, id string, upd actions.ActionApprovalUpdate) error {
	args := []interface{}{}
	sets := []string{"updated_at = NOW()"}
	argN := 1
	if upd.Status != "" {
		sets = append(sets, "status = $"+strconv.Itoa(argN))
		args = append(args, upd.Status)
		argN++
	}
	if upd.ReviewedBy != nil {
		sets = append(sets, "reviewed_by = $"+strconv.Itoa(argN))
		args = append(args, *upd.ReviewedBy)
		argN++
	}
	if upd.Reason != nil {
		sets = append(sets, "reason = $"+strconv.Itoa(argN))
		args = append(args, *upd.Reason)
		argN++
	}
	args = append(args, id)
	tag, err := s.pool.Exec(ctx,
		`UPDATE action_approvals SET `+strings.Join(sets, ", ")+` WHERE id = $`+strconv.Itoa(argN),
		args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return oms.ErrNotFound
	}
	return nil
}
