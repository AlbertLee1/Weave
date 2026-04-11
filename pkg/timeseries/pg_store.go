package timeseries

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGStore is a PostgreSQL-backed Store. Points live in the
// timeseries_points table (see migrations/000016_timeseries.up.sql) with
// value stored as JSONB so numeric/string/struct payloads round-trip.
type PGStore struct {
	pool *pgxpool.Pool
}

// NewPGStore wraps a pgx pool as a Store.
func NewPGStore(pool *pgxpool.Pool) *PGStore {
	return &PGStore{pool: pool}
}

// AppendPoint inserts one row. Rows collide on the composite PK
// (series, ts) so repeated appends at identical timestamps are upsert-like.
func (s *PGStore) AppendPoint(ctx context.Context, key SeriesKey, p Point) error {
	valueJSON, err := json.Marshal(p.Value)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO timeseries_points
		    (ontology_rid, object_type, primary_key, property, ts, value)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (ontology_rid, object_type, primary_key, property, ts)
		 DO UPDATE SET value = EXCLUDED.value`,
		key.Ontology, key.ObjectType, key.PrimaryKey, key.Property, p.Time, valueJSON)
	return err
}

// FirstPoint returns the earliest point, or ErrNoPoints.
func (s *PGStore) FirstPoint(ctx context.Context, key SeriesKey) (*Point, error) {
	return s.singlePoint(ctx, key, "ASC")
}

// LastPoint returns the latest point, or ErrNoPoints.
func (s *PGStore) LastPoint(ctx context.Context, key SeriesKey) (*Point, error) {
	return s.singlePoint(ctx, key, "DESC")
}

func (s *PGStore) singlePoint(ctx context.Context, key SeriesKey, order string) (*Point, error) {
	// order is a compile-time constant drawn from {"ASC", "DESC"} by the
	// caller — safe to interpolate.
	sql := `SELECT ts, value FROM timeseries_points
	        WHERE ontology_rid=$1 AND object_type=$2 AND primary_key=$3 AND property=$4
	        ORDER BY ts ` + order + ` LIMIT 1`
	row := s.pool.QueryRow(ctx, sql, key.Ontology, key.ObjectType, key.PrimaryKey, key.Property)
	var p Point
	var raw []byte
	if err := row.Scan(&p.Time, &raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoPoints
		}
		return nil, err
	}
	if err := json.Unmarshal(raw, &p.Value); err != nil {
		return nil, err
	}
	return &p, nil
}

// StreamPoints returns every point for the series in ascending order.
func (s *PGStore) StreamPoints(ctx context.Context, key SeriesKey) ([]Point, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT ts, value FROM timeseries_points
		 WHERE ontology_rid=$1 AND object_type=$2 AND primary_key=$3 AND property=$4
		 ORDER BY ts ASC`,
		key.Ontology, key.ObjectType, key.PrimaryKey, key.Property)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Point
	for rows.Next() {
		var p Point
		var raw []byte
		if err := rows.Scan(&p.Time, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &p.Value); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []Point{}
	}
	return out, nil
}
