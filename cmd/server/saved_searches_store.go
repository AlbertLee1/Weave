package main

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liyang/weave/pkg/savedsearches"
)

// pgSavedSearchesStore satisfies savedsearches.Store by persisting rows
// to the saved_searches table (US-311). Lives in cmd/server/ rather
// than pkg/savedsearches/ so the package stays free of any pgx import
// — same dep-direction trick as pgFeatureFlagsStore.
type pgSavedSearchesStore struct {
	pool *pgxpool.Pool
}

func newPGSavedSearchesStore(pool *pgxpool.Pool) *pgSavedSearchesStore {
	return &pgSavedSearchesStore{pool: pool}
}

func isSavedSearchUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLSTATE 23505") ||
		strings.Contains(msg, "duplicate key value")
}

// definitionForWrite normalises a JSONB-bound payload — pgx encodes a
// nil json.RawMessage as the string "null", which the column will
// happily accept but breaks the "absent ⇒ {}" round-trip. Mirrors the
// pkg/oms.normaliseSignatureForWrite pattern (US-216).
func definitionForWrite(def json.RawMessage) []byte {
	if len(def) == 0 {
		return []byte("{}")
	}
	return []byte(def)
}

func (s *pgSavedSearchesStore) Create(ctx context.Context, row *savedsearches.SavedSearch) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO saved_searches (id, name, ontology, object_type, created_by, definition)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		row.ID, row.Name, row.Ontology, row.ObjectType, row.CreatedBy,
		definitionForWrite(row.Definition),
	)
	if err != nil {
		if isSavedSearchUniqueViolation(err) {
			return savedsearches.ErrNameConflict
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

func (s *pgSavedSearchesStore) Get(ctx context.Context, id, createdBy string) (*savedsearches.SavedSearch, error) {
	var row savedsearches.SavedSearch
	var defBytes []byte
	err := s.pool.QueryRow(ctx,
		`SELECT id::text, name, ontology, object_type, created_by,
		        COALESCE(definition, '{}'::jsonb), created_at, updated_at
		 FROM saved_searches WHERE id = $1 AND created_by = $2`,
		id, createdBy).
		Scan(&row.ID, &row.Name, &row.Ontology, &row.ObjectType, &row.CreatedBy,
			&defBytes, &row.CreatedAt, &row.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, savedsearches.ErrNotFound
		}
		return nil, err
	}
	row.Definition = json.RawMessage(defBytes)
	return &row, nil
}

func (s *pgSavedSearchesStore) List(ctx context.Context, createdBy, ontology, objectType string) ([]*savedsearches.SavedSearch, error) {
	args := []interface{}{createdBy}
	clauses := []string{"created_by = $1"}
	if ontology != "" {
		args = append(args, ontology)
		clauses = append(clauses, "ontology = $"+strconv.Itoa(len(args)))
	}
	if objectType != "" {
		args = append(args, objectType)
		clauses = append(clauses, "object_type = $"+strconv.Itoa(len(args)))
	}
	q := `SELECT id::text, name, ontology, object_type, created_by,
	             COALESCE(definition, '{}'::jsonb), created_at, updated_at
	      FROM saved_searches WHERE ` + strings.Join(clauses, " AND ") +
		` ORDER BY name ASC`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*savedsearches.SavedSearch
	for rows.Next() {
		var r savedsearches.SavedSearch
		var defBytes []byte
		if err := rows.Scan(&r.ID, &r.Name, &r.Ontology, &r.ObjectType, &r.CreatedBy,
			&defBytes, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Definition = json.RawMessage(defBytes)
		out = append(out, &r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *pgSavedSearchesStore) Update(ctx context.Context, id, createdBy string, upd savedsearches.Update) error {
	args := []interface{}{}
	sets := []string{"updated_at = NOW()"}
	argN := 1
	if upd.Name != nil {
		sets = append(sets, "name = $"+strconv.Itoa(argN))
		args = append(args, *upd.Name)
		argN++
	}
	if upd.Definition != nil {
		sets = append(sets, "definition = $"+strconv.Itoa(argN))
		args = append(args, definitionForWrite(*upd.Definition))
		argN++
	}
	args = append(args, id, createdBy)
	tag, err := s.pool.Exec(ctx,
		`UPDATE saved_searches SET `+strings.Join(sets, ", ")+
			` WHERE id = $`+strconv.Itoa(argN)+
			` AND created_by = $`+strconv.Itoa(argN+1),
		args...)
	if err != nil {
		if isSavedSearchUniqueViolation(err) {
			return savedsearches.ErrNameConflict
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return savedsearches.ErrNotFound
	}
	return nil
}

func (s *pgSavedSearchesStore) Delete(ctx context.Context, id, createdBy string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM saved_searches WHERE id = $1 AND created_by = $2`,
		id, createdBy)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return savedsearches.ErrNotFound
	}
	return nil
}
