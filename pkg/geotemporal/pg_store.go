package geotemporal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PgStore is a PostgreSQL-backed Store. Rows live in the geotemporal_values
// table (see migrations/000205_geotemporal_values.up.sql) with position
// stored as JSONB so GeoJSON Point payloads round-trip without PostGIS.
//
// PgStore is safe for concurrent use; pgxpool.Pool handles connection
// multiplexing.
type PgStore struct {
	pool *pgxpool.Pool
}

// NewPgStore wraps a pgx pool as a Store. The pool must already point at a
// database where migration 000205 has been applied.
func NewPgStore(pool *pgxpool.Pool) *PgStore {
	return &PgStore{pool: pool}
}

// AppendValue inserts one (time, position) row. Repeated appends at the
// same timestamp upsert the position so callers can replay batches without
// duplicating rows.
func (s *PgStore) AppendValue(ctx context.Context, key SeriesKey, v Value) error {
	positionJSON, err := json.Marshal(v.Position)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO geotemporal_values
		    (ontology, object_type, primary_key, property, ts, position)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (ontology, object_type, primary_key, property, ts)
		 DO UPDATE SET position = EXCLUDED.position`,
		key.Ontology, key.ObjectType, key.PrimaryKey, key.Property, v.Time, positionJSON)
	return err
}

// LatestValue returns the most recent value for the series, or ErrNoValues
// when the series has no rows.
func (s *PgStore) LatestValue(ctx context.Context, key SeriesKey) (*Value, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT ts, position FROM geotemporal_values
		 WHERE ontology=$1 AND object_type=$2 AND primary_key=$3 AND property=$4
		 ORDER BY ts DESC LIMIT 1`,
		key.Ontology, key.ObjectType, key.PrimaryKey, key.Property)
	var v Value
	var raw []byte
	if err := row.Scan(&v.Time, &raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoValues
		}
		return nil, err
	}
	if err := json.Unmarshal(raw, &v.Position); err != nil {
		return nil, err
	}
	return &v, nil
}

// StreamHistoricValues returns every value for the series ordered by time
// ascending. An empty series returns an empty (non-nil) slice and no error,
// matching MemoryStore's contract.
func (s *PgStore) StreamHistoricValues(ctx context.Context, key SeriesKey) ([]Value, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT ts, position FROM geotemporal_values
		 WHERE ontology=$1 AND object_type=$2 AND primary_key=$3 AND property=$4
		 ORDER BY ts ASC`,
		key.Ontology, key.ObjectType, key.PrimaryKey, key.Property)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Value{}
	for rows.Next() {
		var v Value
		var raw []byte
		if err := rows.Scan(&v.Time, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &v.Position); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// QueryBBoxRange returns rows that match both the bbox AND the time range.
// The bbox predicate is expressed as a pair of float8 BETWEEN clauses on
// the longitude/latitude functional indexes added by migration 000208, so
// the planner can BitmapAnd them with the series-key + ts index instead of
// scanning every JSONB payload. Time bounds default to "unbounded on this
// side" when set to the zero time.
//
// Results are ordered by ts ASC. An empty match returns an empty (non-nil)
// slice, never ErrNoValues — that contract is reserved for the LatestValue
// endpoint where "no row" is a semantically distinct outcome.
func (s *PgStore) QueryBBoxRange(ctx context.Context, key SeriesKey, bbox BBox, tr TimeRange) ([]Value, error) {
	var b strings.Builder
	args := []any{key.Ontology, key.ObjectType, key.PrimaryKey, key.Property,
		bbox.MinLng, bbox.MaxLng, bbox.MinLat, bbox.MaxLat}
	b.WriteString(
		`SELECT ts, position FROM geotemporal_values
		 WHERE ontology=$1 AND object_type=$2 AND primary_key=$3 AND property=$4
		   AND (position->'coordinates'->>0)::float8 BETWEEN $5 AND $6
		   AND (position->'coordinates'->>1)::float8 BETWEEN $7 AND $8`)
	if !tr.Start.IsZero() {
		args = append(args, tr.Start)
		fmt.Fprintf(&b, " AND ts >= $%d", len(args))
	}
	if !tr.End.IsZero() {
		args = append(args, tr.End)
		fmt.Fprintf(&b, " AND ts <= $%d", len(args))
	}
	b.WriteString(" ORDER BY ts ASC")

	rows, err := s.pool.Query(ctx, b.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Value{}
	for rows.Next() {
		var v Value
		var raw []byte
		if err := rows.Scan(&v.Time, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &v.Position); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Compile-time guarantee that *PgStore satisfies the Store interface.
var _ Store = (*PgStore)(nil)

// Compile-time guarantee that *PgStore also satisfies the spatial+temporal
// querier capability the US-466 PG-persistence story requires.
var _ SpatialTemporalQuerier = (*PgStore)(nil)
