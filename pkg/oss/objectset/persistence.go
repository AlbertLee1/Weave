package objectset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrSavedSetNotFound is returned by SavedStore implementations when the
// requested record does not exist. Callers can wrap this with errors.Is to
// distinguish "missing" from "lookup failed for some other reason".
var ErrSavedSetNotFound = errors.New("saved object set not found")

// SavedObjectSet is the persistent counterpart of an in-memory ObjectSet
// definition. The Definition is stored as raw JSON so the underlying tree
// can be re-parsed by the executor without an extra round-trip through the
// typed Definition struct (which simplifies migrations and lets the table
// outlive any single in-memory schema version).
type SavedObjectSet struct {
	ID              string
	OntologyAPIName string
	Name            string
	Description     string
	Definition      json.RawMessage
	CreatedBy       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// SavedStore is the abstract repository for persistent ObjectSets. The
// in-memory Store keeps temporary share-link payloads with a TTL; SavedStore
// keeps named, durable definitions that survive process restarts.
type SavedStore interface {
	Create(ctx context.Context, s *SavedObjectSet) error
	Get(ctx context.Context, id string) (*SavedObjectSet, error)
	GetByName(ctx context.Context, ontologyAPIName, name string) (*SavedObjectSet, error)
	List(ctx context.Context, ontologyAPIName string, limit int) ([]SavedObjectSet, error)
	Update(ctx context.Context, s *SavedObjectSet) error
	Delete(ctx context.Context, id string) error
}

// PGSavedStore is a Postgres-backed SavedStore. It maps directly to the
// saved_object_sets table from migration 000013.
type PGSavedStore struct {
	pool *pgxpool.Pool
}

// NewPGSavedStore wraps a pgx pool as a SavedStore.
func NewPGSavedStore(pool *pgxpool.Pool) *PGSavedStore {
	return &PGSavedStore{pool: pool}
}

// listDefaultLimit is the cap applied when callers request List with
// limit <= 0. Keeps the response bounded without forcing every caller to
// pick a number.
const listDefaultLimit = 100

// listMaxLimit is the upper bound applied to all List calls regardless of
// what the caller asked for, mirroring the history endpoint's clamp.
const listMaxLimit = 500

func (s *PGSavedStore) Create(ctx context.Context, rec *SavedObjectSet) error {
	if rec == nil {
		return errors.New("saved object set: record required")
	}
	if rec.OntologyAPIName == "" {
		return errors.New("saved object set: ontologyApiName required")
	}
	if rec.Name == "" {
		return errors.New("saved object set: name required")
	}
	if len(rec.Definition) == 0 {
		return errors.New("saved object set: definition required")
	}
	row := s.pool.QueryRow(ctx,
		`INSERT INTO saved_object_sets
		    (ontology_api_name, name, description, definition, created_by)
		 VALUES ($1, $2, NULLIF($3, ''), $4, NULLIF($5, ''))
		 RETURNING id, created_at, updated_at`,
		rec.OntologyAPIName, rec.Name, rec.Description, []byte(rec.Definition), rec.CreatedBy)
	if err := row.Scan(&rec.ID, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
		return fmt.Errorf("insert saved object set: %w", err)
	}
	return nil
}

func (s *PGSavedStore) Get(ctx context.Context, id string) (*SavedObjectSet, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, ontology_api_name, name, COALESCE(description, ''),
		        definition, COALESCE(created_by, ''), created_at, updated_at
		 FROM saved_object_sets WHERE id = $1`, id)
	return scanSavedSet(row)
}

func (s *PGSavedStore) GetByName(ctx context.Context, ontologyAPIName, name string) (*SavedObjectSet, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, ontology_api_name, name, COALESCE(description, ''),
		        definition, COALESCE(created_by, ''), created_at, updated_at
		 FROM saved_object_sets WHERE ontology_api_name = $1 AND name = $2`,
		ontologyAPIName, name)
	return scanSavedSet(row)
}

func (s *PGSavedStore) List(ctx context.Context, ontologyAPIName string, limit int) ([]SavedObjectSet, error) {
	if limit <= 0 {
		limit = listDefaultLimit
	}
	if limit > listMaxLimit {
		limit = listMaxLimit
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, ontology_api_name, name, COALESCE(description, ''),
		        definition, COALESCE(created_by, ''), created_at, updated_at
		 FROM saved_object_sets
		 WHERE ontology_api_name = $1
		 ORDER BY created_at DESC
		 LIMIT $2`,
		ontologyAPIName, limit)
	if err != nil {
		return nil, fmt.Errorf("list saved object sets: %w", err)
	}
	defer rows.Close()

	out := make([]SavedObjectSet, 0)
	for rows.Next() {
		var rec SavedObjectSet
		var defJSON []byte
		if err := rows.Scan(
			&rec.ID, &rec.OntologyAPIName, &rec.Name, &rec.Description,
			&defJSON, &rec.CreatedBy, &rec.CreatedAt, &rec.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan saved object set: %w", err)
		}
		rec.Definition = json.RawMessage(defJSON)
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate saved object sets: %w", err)
	}
	return out, nil
}

func (s *PGSavedStore) Update(ctx context.Context, rec *SavedObjectSet) error {
	if rec == nil || rec.ID == "" {
		return errors.New("saved object set: id required for update")
	}
	if len(rec.Definition) == 0 {
		return errors.New("saved object set: definition required")
	}
	row := s.pool.QueryRow(ctx,
		`UPDATE saved_object_sets
		    SET name = $2,
		        description = NULLIF($3, ''),
		        definition = $4,
		        updated_at = NOW()
		  WHERE id = $1
		  RETURNING updated_at`,
		rec.ID, rec.Name, rec.Description, []byte(rec.Definition))
	if err := row.Scan(&rec.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSavedSetNotFound
		}
		return fmt.Errorf("update saved object set: %w", err)
	}
	return nil
}

func (s *PGSavedStore) Delete(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM saved_object_sets WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete saved object set: %w", err)
	}
	// DELETE is idempotent: a missing row is not an error so the HTTP
	// layer can safely retry.
	return nil
}

// scanSavedSet pulls a single row into a SavedObjectSet, normalising the
// pgx-specific "no rows" error into the package-level sentinel.
func scanSavedSet(row pgx.Row) (*SavedObjectSet, error) {
	var rec SavedObjectSet
	var defJSON []byte
	err := row.Scan(
		&rec.ID, &rec.OntologyAPIName, &rec.Name, &rec.Description,
		&defJSON, &rec.CreatedBy, &rec.CreatedAt, &rec.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSavedSetNotFound
		}
		return nil, fmt.Errorf("scan saved object set: %w", err)
	}
	rec.Definition = json.RawMessage(defJSON)
	return &rec, nil
}
