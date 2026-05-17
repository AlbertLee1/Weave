package objectset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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
//
// US-365 fields:
//   - DefinitionHash    sha256 of the canonical JSON definition. Lets clients
//     compare two saved sets without re-marshalling.
//   - SnapshotAt        Monotonically-increasing snapshot transaction id
//     allocated from saved_object_sets_snapshot_seq.
//   - IsImmutable       Permanent (true) vs ephemeral 1h-TTL (false).
//   - FrozenObjectType / FrozenPrimaryKeys / FrozenTruncated
//     Materialised execution result captured at save time.
//     Populated when the caller wants byte-for-byte
//     identical re-loads regardless of subsequent edits.
type SavedObjectSet struct {
	ID                string
	OntologyAPIName   string
	Name              string
	Description       string
	Definition        json.RawMessage
	CreatedBy         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DefinitionHash    string
	SnapshotAt        int64
	IsImmutable       bool
	FrozenObjectType  string
	FrozenPrimaryKeys []string
	FrozenTruncated   bool
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
// saved_object_sets table from migration 000013, extended by 000082 with
// definition_hash / snapshot_at / is_immutable plus frozen_* columns.
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

// HashDefinition returns the sha256 of the canonical JSON representation of
// raw. Canonicalisation re-marshals through map[string]any so semantically
// identical definitions with different key ordering or whitespace produce the
// same hash. Returns the empty string when raw is empty.
func HashDefinition(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	canon, err := canonicalJSON(raw)
	if err != nil {
		// Definitions that fail to canonicalise still get a stable hash
		// over the original bytes so downstream code never sees an empty
		// hash on a non-empty definition.
		sum := sha256.Sum256(raw)
		return hex.EncodeToString(sum[:])
	}
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:])
}

// canonicalJSON re-marshals raw with sorted object keys so that two semantically
// equivalent definitions with different key orderings hash identically.
func canonicalJSON(raw json.RawMessage) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return marshalSorted(v)
}

func marshalSorted(v any) ([]byte, error) {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf := []byte{'{'}
		for i, k := range keys {
			if i > 0 {
				buf = append(buf, ',')
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			buf = append(buf, kb...)
			buf = append(buf, ':')
			vb, err := marshalSorted(x[k])
			if err != nil {
				return nil, err
			}
			buf = append(buf, vb...)
		}
		buf = append(buf, '}')
		return buf, nil
	case []any:
		buf := []byte{'['}
		for i, item := range x {
			if i > 0 {
				buf = append(buf, ',')
			}
			ib, err := marshalSorted(item)
			if err != nil {
				return nil, err
			}
			buf = append(buf, ib...)
		}
		buf = append(buf, ']')
		return buf, nil
	default:
		return json.Marshal(v)
	}
}

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
	rec.DefinitionHash = HashDefinition(rec.Definition)
	frozenPKsJSON, err := encodeFrozenPKs(rec.FrozenPrimaryKeys)
	if err != nil {
		return fmt.Errorf("encode frozen primary keys: %w", err)
	}
	row := s.pool.QueryRow(ctx,
		`INSERT INTO saved_object_sets
		    (ontology_api_name, name, description, definition, created_by,
		     definition_hash, snapshot_at, is_immutable,
		     frozen_object_type, frozen_primary_keys, frozen_truncated)
		 VALUES ($1, $2, NULLIF($3, ''), $4, NULLIF($5, ''),
		         $6, nextval('saved_object_sets_snapshot_seq'), $7,
		         NULLIF($8, ''), $9, $10)
		 RETURNING id, created_at, updated_at, snapshot_at`,
		rec.OntologyAPIName, rec.Name, rec.Description, []byte(rec.Definition), rec.CreatedBy,
		rec.DefinitionHash, rec.IsImmutable,
		rec.FrozenObjectType, frozenPKsJSON, rec.FrozenTruncated)
	if err := row.Scan(&rec.ID, &rec.CreatedAt, &rec.UpdatedAt, &rec.SnapshotAt); err != nil {
		return fmt.Errorf("insert saved object set: %w", err)
	}
	return nil
}

func (s *PGSavedStore) Get(ctx context.Context, id string) (*SavedObjectSet, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, ontology_api_name, name, COALESCE(description, ''),
		        definition, COALESCE(created_by, ''), created_at, updated_at,
		        COALESCE(definition_hash, ''), COALESCE(snapshot_at, 0),
		        COALESCE(is_immutable, TRUE),
		        COALESCE(frozen_object_type, ''),
		        COALESCE(frozen_primary_keys, '[]'::jsonb),
		        COALESCE(frozen_truncated, FALSE)
		 FROM saved_object_sets WHERE id = $1`, id)
	return scanSavedSet(row)
}

func (s *PGSavedStore) GetByName(ctx context.Context, ontologyAPIName, name string) (*SavedObjectSet, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, ontology_api_name, name, COALESCE(description, ''),
		        definition, COALESCE(created_by, ''), created_at, updated_at,
		        COALESCE(definition_hash, ''), COALESCE(snapshot_at, 0),
		        COALESCE(is_immutable, TRUE),
		        COALESCE(frozen_object_type, ''),
		        COALESCE(frozen_primary_keys, '[]'::jsonb),
		        COALESCE(frozen_truncated, FALSE)
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
		        definition, COALESCE(created_by, ''), created_at, updated_at,
		        COALESCE(definition_hash, ''), COALESCE(snapshot_at, 0),
		        COALESCE(is_immutable, TRUE),
		        COALESCE(frozen_object_type, ''),
		        COALESCE(frozen_primary_keys, '[]'::jsonb),
		        COALESCE(frozen_truncated, FALSE)
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
		var defJSON, frozenJSON []byte
		if err := rows.Scan(
			&rec.ID, &rec.OntologyAPIName, &rec.Name, &rec.Description,
			&defJSON, &rec.CreatedBy, &rec.CreatedAt, &rec.UpdatedAt,
			&rec.DefinitionHash, &rec.SnapshotAt, &rec.IsImmutable,
			&rec.FrozenObjectType, &frozenJSON, &rec.FrozenTruncated,
		); err != nil {
			return nil, fmt.Errorf("scan saved object set: %w", err)
		}
		rec.Definition = json.RawMessage(defJSON)
		if pks, err := decodeFrozenPKs(frozenJSON); err == nil {
			rec.FrozenPrimaryKeys = pks
		}
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
	rec.DefinitionHash = HashDefinition(rec.Definition)
	frozenPKsJSON, err := encodeFrozenPKs(rec.FrozenPrimaryKeys)
	if err != nil {
		return fmt.Errorf("encode frozen primary keys: %w", err)
	}
	row := s.pool.QueryRow(ctx,
		`UPDATE saved_object_sets
		    SET name = $2,
		        description = NULLIF($3, ''),
		        definition = $4,
		        updated_at = NOW(),
		        definition_hash = $5,
		        snapshot_at = nextval('saved_object_sets_snapshot_seq'),
		        is_immutable = $6,
		        frozen_object_type = NULLIF($7, ''),
		        frozen_primary_keys = $8,
		        frozen_truncated = $9
		  WHERE id = $1
		  RETURNING updated_at, snapshot_at`,
		rec.ID, rec.Name, rec.Description, []byte(rec.Definition),
		rec.DefinitionHash, rec.IsImmutable,
		rec.FrozenObjectType, frozenPKsJSON, rec.FrozenTruncated)
	if err := row.Scan(&rec.UpdatedAt, &rec.SnapshotAt); err != nil {
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

// ReapExpired deletes ephemeral (is_immutable=false) saved object sets older
// than ttl and returns the number of rows removed. Immutable rows are always
// retained, regardless of age. The cmd/server bootstrap calls this on a
// 1-minute ticker with ttl=1h to fulfil the US-365 retention policy.
func (s *PGSavedStore) ReapExpired(ctx context.Context, ttl time.Duration) (int64, error) {
	if ttl <= 0 {
		return 0, nil
	}
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM saved_object_sets
		  WHERE is_immutable = FALSE
		    AND created_at < NOW() - make_interval(secs => $1)`,
		ttl.Seconds())
	if err != nil {
		return 0, fmt.Errorf("reap saved object sets: %w", err)
	}
	return tag.RowsAffected(), nil
}

// SavedSetReaper is the minimal contract the reaper loop needs from a saved
// ObjectSet store: drop expired ephemeral rows older than ttl and return the
// count removed. *PGSavedStore satisfies it via its real DELETE; tests can
// plug a fake to exercise the loop driver without booting Postgres.
type SavedSetReaper interface {
	ReapExpired(ctx context.Context, ttl time.Duration) (int64, error)
}

// RunSavedSetReaperLoop drives reaper.ReapExpired on a fixed interval until
// ctx is cancelled. onReap (optional) is invoked after each successful sweep
// with the number of rows deleted so the caller can publish a metric. Errors
// are passed through onError (also optional) so a transient PG hiccup does
// not kill the loop. A nil reaper / non-positive interval / non-positive ttl
// is a no-op so degraded-mode boot (no PG pool) is safe.
//
// The intended caller is cmd/server: spawn a goroutine on startup,
// `RunSavedSetReaperLoop(rootCtx, store, 5*time.Minute, time.Hour, ...)`,
// and let context cancellation on graceful shutdown stop the loop. US-462.
func RunSavedSetReaperLoop(ctx context.Context, reaper SavedSetReaper, interval, ttl time.Duration, onReap func(int64), onError func(error)) {
	if reaper == nil || interval <= 0 || ttl <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := reaper.ReapExpired(ctx, ttl)
			if err != nil {
				if onError != nil {
					onError(err)
				}
				continue
			}
			if onReap != nil {
				onReap(n)
			}
		}
	}
}

// RunReaperLoop is the method-bound shim retained for callers that hold a
// *PGSavedStore directly; new wiring should prefer RunSavedSetReaperLoop
// (US-462) so a fake reaper can drive the loop in unit tests.
func (s *PGSavedStore) RunReaperLoop(ctx context.Context, interval, ttl time.Duration, onReap func(int64), onError func(error)) {
	RunSavedSetReaperLoop(ctx, s, interval, ttl, onReap, onError)
}

// scanSavedSet pulls a single row into a SavedObjectSet, normalising the
// pgx-specific "no rows" error into the package-level sentinel.
func scanSavedSet(row pgx.Row) (*SavedObjectSet, error) {
	var rec SavedObjectSet
	var defJSON, frozenJSON []byte
	err := row.Scan(
		&rec.ID, &rec.OntologyAPIName, &rec.Name, &rec.Description,
		&defJSON, &rec.CreatedBy, &rec.CreatedAt, &rec.UpdatedAt,
		&rec.DefinitionHash, &rec.SnapshotAt, &rec.IsImmutable,
		&rec.FrozenObjectType, &frozenJSON, &rec.FrozenTruncated,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSavedSetNotFound
		}
		return nil, fmt.Errorf("scan saved object set: %w", err)
	}
	rec.Definition = json.RawMessage(defJSON)
	if pks, err := decodeFrozenPKs(frozenJSON); err == nil {
		rec.FrozenPrimaryKeys = pks
	}
	return &rec, nil
}

// encodeFrozenPKs marshals the materialised PK list, defaulting to an empty
// JSON array when the caller did not freeze membership. Storing `[]` instead
// of NULL keeps every read path symmetric with no extra branch.
func encodeFrozenPKs(pks []string) ([]byte, error) {
	if pks == nil {
		return []byte("[]"), nil
	}
	return json.Marshal(pks)
}

// decodeFrozenPKs unmarshals frozen_primary_keys back into a string slice. An
// empty / null payload yields a nil slice.
func decodeFrozenPKs(raw []byte) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var pks []string
	if err := json.Unmarshal(raw, &pks); err != nil {
		return nil, err
	}
	return pks, nil
}
