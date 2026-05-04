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
// rows to the action_parameter_templates table. Lives in cmd/server/
// rather than pkg/actiontemplates/ so the package stays free of any
// pgx import — same dep-direction trick as pgSavedSearchesStore
// (US-311). US-427 added the scope column; the store keeps the legacy
// `shared` boolean in lock-step on every write so any v1 SDK reader
// still on the boolean dimension keeps observing PUBLIC ⇔ shared=TRUE.
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

// scopeForWrite normalises the scope on write. Empty defaults to
// PRIVATE; the CHECK constraint guards anything else, but we filter
// at the Go layer too so the error message is friendlier.
func scopeForWrite(scope string) string {
	switch scope {
	case actiontemplates.ScopeTeam, actiontemplates.ScopePublic:
		return scope
	default:
		return actiontemplates.ScopePrivate
	}
}

const actionTemplateSelectColumns = `id::text, name, ontology, action_type, created_by,
    COALESCE(scope, 'PRIVATE') AS scope, shared,
    COALESCE(parameters, '{}'::jsonb), created_at, updated_at`

func scanTemplate(row pgx.Row) (*actiontemplates.Template, error) {
	var t actiontemplates.Template
	var paramsBytes []byte
	if err := row.Scan(&t.ID, &t.Name, &t.Ontology, &t.ActionType, &t.CreatedBy,
		&t.Scope, &t.Shared, &paramsBytes, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	t.Parameters = json.RawMessage(paramsBytes)
	// Defensive: rebuild Shared from Scope so an out-of-band UPDATE
	// that only touched one column never leaks an inconsistent pair.
	t.Shared = actiontemplates.SharedFromScope(t.Scope)
	return &t, nil
}

func (s *pgActionTemplatesStore) Create(ctx context.Context, row *actiontemplates.Template) error {
	scope := scopeForWrite(row.Scope)
	shared := actiontemplates.SharedFromScope(scope)
	_, err := s.pool.Exec(ctx,
		`INSERT INTO action_parameter_templates
		    (id, name, ontology, action_type, created_by, scope, shared, parameters)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		row.ID, row.Name, row.Ontology, row.ActionType, row.CreatedBy, scope, shared,
		parametersForWrite(row.Parameters),
	)
	if err != nil {
		if isActionTemplateUniqueViolation(err) {
			return actiontemplates.ErrNameConflict
		}
		return err
	}
	fresh, err := s.getInternal(ctx, row.ID)
	if err != nil {
		return err
	}
	*row = *fresh
	return nil
}

// getInternal reads a row by id WITHOUT applying Visibility filters —
// used internally by Create after insert (the row is always visible to
// its creator) and by Update before returning the post-update shape.
func (s *pgActionTemplatesStore) getInternal(ctx context.Context, id string) (*actiontemplates.Template, error) {
	q := `SELECT ` + actionTemplateSelectColumns + ` FROM action_parameter_templates WHERE id = $1`
	row, err := scanTemplate(s.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, actiontemplates.ErrNotFound
		}
		return nil, err
	}
	return row, nil
}

func (s *pgActionTemplatesStore) Get(ctx context.Context, id string, vis actiontemplates.Visibility) (*actiontemplates.Template, error) {
	args := []interface{}{id, vis.CallerID}
	clauses := []string{
		"id = $1",
		"(created_by = $2 OR scope = 'PUBLIC'",
	}
	if len(vis.Teammates) > 0 {
		args = append(args, vis.Teammates)
		clauses[1] += " OR (scope = 'TEAM' AND created_by = ANY($3))"
	}
	clauses[1] += ")"
	q := `SELECT ` + actionTemplateSelectColumns +
		` FROM action_parameter_templates WHERE ` + strings.Join(clauses, " AND ")
	row, err := scanTemplate(s.pool.QueryRow(ctx, q, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, actiontemplates.ErrNotFound
		}
		return nil, err
	}
	return row, nil
}

func (s *pgActionTemplatesStore) List(ctx context.Context, vis actiontemplates.Visibility, ontology, actionType string) ([]*actiontemplates.Template, error) {
	args := []interface{}{vis.CallerID}
	visClause := "(created_by = $1 OR scope = 'PUBLIC'"
	if len(vis.Teammates) > 0 {
		args = append(args, vis.Teammates)
		visClause += " OR (scope = 'TEAM' AND created_by = ANY($" + strconv.Itoa(len(args)) + "))"
	}
	visClause += ")"
	clauses := []string{visClause}
	if ontology != "" {
		args = append(args, ontology)
		clauses = append(clauses, "ontology = $"+strconv.Itoa(len(args)))
	}
	if actionType != "" {
		args = append(args, actionType)
		clauses = append(clauses, "action_type = $"+strconv.Itoa(len(args)))
	}
	q := `SELECT ` + actionTemplateSelectColumns +
		` FROM action_parameter_templates WHERE ` + strings.Join(clauses, " AND ") +
		` ORDER BY name ASC, created_by ASC`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*actiontemplates.Template
	for rows.Next() {
		r, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
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
	// Resolve target scope from either explicit scope or legacy
	// shared boolean. If both are absent, leave columns alone.
	var newScope *string
	if upd.Scope != nil {
		s := scopeForWrite(*upd.Scope)
		newScope = &s
	} else if upd.Shared != nil {
		s := actiontemplates.ScopeFromShared(*upd.Shared)
		newScope = &s
	}
	if newScope != nil {
		sets = append(sets, "scope = $"+strconv.Itoa(argN))
		args = append(args, *newScope)
		argN++
		sets = append(sets, "shared = $"+strconv.Itoa(argN))
		args = append(args, actiontemplates.SharedFromScope(*newScope))
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
