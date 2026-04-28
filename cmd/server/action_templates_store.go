package main

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liyang/weave/pkg/actiontemplates"
)

// pgActionTemplatesStore satisfies actiontemplates.Store by persisting
// rows to the action_parameter_templates table (US-320). Lives in
// cmd/server/ rather than pkg/actiontemplates/ so the package stays
// free of any pgx import — same dep-direction trick as
// pgSavedSearchesStore (US-311).
type pgActionTemplatesStore struct {
	pool *pgxpool.Pool
}

func newPGActionTemplatesStore(pool *pgxpool.Pool) *pgActionTemplatesStore {
	return &pgActionTemplatesStore{pool: pool}
}

func isActionTemplateUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLSTATE 23505") ||
		strings.Contains(msg, "duplicate key value")
}

// parametersForWrite normalises the JSONB-bound payload — pgx encodes
// a nil json.RawMessage as the string "null", which the column will
// accept but breaks the "absent ⇒ {}" round-trip.
func parametersForWrite(params json.RawMessage) []byte {
	if len(params) == 0 {
		return []byte("{}")
	}
	return []byte(params)
}

func (s *pgActionTemplatesStore) Create(ctx context.Context, row *actiontemplates.Template) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO action_parameter_templates (id, name, ontology, action_type, created_by, shared, parameters)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		row.ID, row.Name, row.Ontology, row.ActionType, row.CreatedBy, row.Shared,
		parametersForWrite(row.Parameters),
	)
	if err != nil {
		if isActionTemplateUniqueViolation(err) {
			return actiontemplates.ErrNameConflict
		}
		return err
	}
	fresh, err := s.Get(ctx, row.ID, row.CreatedBy)
	if err != nil {
		return err
	}
	*row = *fresh
	return nil
}

func (s *pgActionTemplatesStore) Get(ctx context.Context, id, callerID string) (*actiontemplates.Template, error) {
	var row actiontemplates.Template
	var paramsBytes []byte
	err := s.pool.QueryRow(ctx,
		`SELECT id::text, name, ontology, action_type, created_by, shared,
		        COALESCE(parameters, '{}'::jsonb), created_at, updated_at
		 FROM action_parameter_templates
		 WHERE id = $1 AND (created_by = $2 OR shared = TRUE)`,
		id, callerID).
		Scan(&row.ID, &row.Name, &row.Ontology, &row.ActionType, &row.CreatedBy,
			&row.Shared, &paramsBytes, &row.CreatedAt, &row.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, actiontemplates.ErrNotFound
		}
		return nil, err
	}
	row.Parameters = json.RawMessage(paramsBytes)
	return &row, nil
}

func (s *pgActionTemplatesStore) List(ctx context.Context, callerID, ontology, actionType string) ([]*actiontemplates.Template, error) {
	args := []interface{}{callerID}
	clauses := []string{"(created_by = $1 OR shared = TRUE)"}
	if ontology != "" {
		args = append(args, ontology)
		clauses = append(clauses, "ontology = $"+strconv.Itoa(len(args)))
	}
	if actionType != "" {
		args = append(args, actionType)
		clauses = append(clauses, "action_type = $"+strconv.Itoa(len(args)))
	}
	q := `SELECT id::text, name, ontology, action_type, created_by, shared,
	             COALESCE(parameters, '{}'::jsonb), created_at, updated_at
	      FROM action_parameter_templates WHERE ` + strings.Join(clauses, " AND ") +
		` ORDER BY name ASC, created_by ASC`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*actiontemplates.Template
	for rows.Next() {
		var r actiontemplates.Template
		var paramsBytes []byte
		if err := rows.Scan(&r.ID, &r.Name, &r.Ontology, &r.ActionType, &r.CreatedBy,
			&r.Shared, &paramsBytes, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Parameters = json.RawMessage(paramsBytes)
		out = append(out, &r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *pgActionTemplatesStore) Update(ctx context.Context, id, ownerID string, upd actiontemplates.Update) error {
	args := []interface{}{}
	sets := []string{"updated_at = NOW()"}
	argN := 1
	if upd.Name != nil {
		sets = append(sets, "name = $"+strconv.Itoa(argN))
		args = append(args, *upd.Name)
		argN++
	}
	if upd.Parameters != nil {
		sets = append(sets, "parameters = $"+strconv.Itoa(argN))
		args = append(args, parametersForWrite(*upd.Parameters))
		argN++
	}
	if upd.Shared != nil {
		sets = append(sets, "shared = $"+strconv.Itoa(argN))
		args = append(args, *upd.Shared)
		argN++
	}
	args = append(args, id, ownerID)
	tag, err := s.pool.Exec(ctx,
		`UPDATE action_parameter_templates SET `+strings.Join(sets, ", ")+
			` WHERE id = $`+strconv.Itoa(argN)+
			` AND created_by = $`+strconv.Itoa(argN+1),
		args...)
	if err != nil {
		if isActionTemplateUniqueViolation(err) {
			return actiontemplates.ErrNameConflict
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return actiontemplates.ErrNotFound
	}
	return nil
}

func (s *pgActionTemplatesStore) Delete(ctx context.Context, id, ownerID string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM action_parameter_templates WHERE id = $1 AND created_by = $2`,
		id, ownerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return actiontemplates.ErrNotFound
	}
	return nil
}
