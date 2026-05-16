package timeseries

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGStore is a PostgreSQL-backed Store. Points live in the
// timeseries_points table (see migrations/000016_timeseries.up.sql) with
// value stored as JSONB so numeric/string/struct payloads round-trip.
//
// US-467: when the database has TimescaleDB installed and the migration
// 000209 has populated the timeseries_cagg_5min continuous aggregate,
// DownsamplePoints reads from the cagg for >=5min-aligned avg/sum/min/
// max/count queries; smaller steps and first/last route to the raw
// table. The cagg state is sniffed lazily on first use and cached.
type PGStore struct {
	pool *pgxpool.Pool
	// cagg-availability cache. -1 = unknown, 0 = absent, 1 = present.
	caggKnown atomic.Int32
}

// NewPGStore wraps a pgx pool as a Store. The cagg-availability cache
// starts in the "unknown" state so the first DownsamplePoints or
// RefreshCAGG call probes timescaledb_information once; cmd/server may
// call DetectCAGG at boot to make the first request not pay that cost.
func NewPGStore(pool *pgxpool.Pool) *PGStore {
	s := &PGStore{pool: pool}
	s.caggKnown.Store(-1)
	return s
}

// Compile-time guarantees that *PGStore satisfies the read-side Store
// interface plus the optional Downsampler / CAGGRefresher capabilities
// US-467 layers on top.
var (
	_ Store         = (*PGStore)(nil)
	_ Downsampler   = (*PGStore)(nil)
	_ CAGGRefresher = (*PGStore)(nil)
)

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

// caggViewName is the materialized view created by migration 000209 over
// timeseries_points. Exported as a package constant so integration tests
// can EXPLAIN against the view name without reaching into pg_store.go.
const caggViewName = "timeseries_cagg_5min"

// caggBucketWidth is the time_bucket interval baked into the cagg view.
const caggBucketWidth = 5 * time.Minute

// DetectCAGG probes whether the timeseries_cagg_5min continuous aggregate
// exists in the connected database and caches the answer. Subsequent
// DownsamplePoints calls consult the cache instead of round-tripping
// timescaledb_information per query. cmd/server calls this once at boot
// after migrations run; tests that swap pools should invoke it again.
//
// The probe is best-effort: any error (including "table does not exist"
// on a plain Postgres database) is treated as "cagg absent". A nil pool
// or context cancellation also yields "absent" so degraded callers stay
// on the raw-table path without surfacing infrastructure failures.
func (s *PGStore) DetectCAGG(ctx context.Context) bool {
	if s == nil || s.pool == nil {
		s.caggKnown.Store(0)
		return false
	}
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (
		    SELECT 1 FROM timescaledb_information.continuous_aggregates
		    WHERE view_name = $1
		 )`, caggViewName).Scan(&exists)
	if err != nil {
		s.caggKnown.Store(0)
		return false
	}
	if exists {
		s.caggKnown.Store(1)
	} else {
		s.caggKnown.Store(0)
	}
	return exists
}

// caggReady returns true when the cagg is known to exist. Unknown / absent
// both fall through to the raw-table path.
func (s *PGStore) caggReady() bool {
	return s.caggKnown.Load() == 1
}

// useCAGG returns true when the spec is compatible with reading from the
// cagg: the cagg exists, the step is a positive multiple of 5 minutes
// (the cagg's bucket width), and the aggregation is one the cagg
// materialises (avg/sum/min/max/count). first/last are not in the cagg
// columns, so they always read raw.
func (s *PGStore) useCAGG(spec DownsampleSpec) bool {
	if !s.caggReady() {
		return false
	}
	if spec.Step < caggBucketWidth || spec.Step%caggBucketWidth != 0 {
		return false
	}
	switch spec.Aggregation {
	case DownsampleAvg, DownsampleSum, DownsampleMin, DownsampleMax, DownsampleCount:
		return true
	default:
		return false
	}
}

// DownsamplePoints satisfies Downsampler: server-side time_bucket
// aggregation against either the timeseries_cagg_5min continuous
// aggregate (for >=5min-aligned avg/sum/min/max/count) or the raw
// timeseries_points table (smaller steps, first/last, or no cagg).
//
// The returned slice is sorted by bucket ascending and empty when the
// series has no points in the requested window. Empty Start/End are
// interpreted as "from epoch" / "until now" so callers can probe the
// entire series without separately calling First/LastPoint.
func (s *PGStore) DownsamplePoints(ctx context.Context, key SeriesKey, spec DownsampleSpec) ([]Point, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	start, end := spec.Start, spec.End
	if start.IsZero() {
		start = time.Unix(0, 0).UTC()
	}
	if end.IsZero() {
		end = time.Now().UTC().Add(time.Hour)
	}
	if !end.After(start) {
		return []Point{}, nil
	}

	// Lazy-detect: probe once if we have not been told yet.
	if s.caggKnown.Load() == -1 {
		s.DetectCAGG(ctx)
	}

	if s.useCAGG(spec) {
		return s.downsampleFromCAGG(ctx, key, spec, start, end)
	}
	return s.downsampleFromRaw(ctx, key, spec, start, end)
}

// downsampleFromRaw runs time_bucket on the raw timeseries_points table.
// JSONB values are cast to float8 via (value::text)::float8; rows whose
// jsonb_typeof is not 'number' are filtered out so non-numeric series
// produce an empty downsample rather than a SQL cast failure.
//
// first/last are emulated via DISTINCT ON when TimescaleDB hyperfunctions
// are absent (i.e. core PG). Detection mirrors the cagg sniff: when
// caggReady() the hyperfunctions are assumed available.
func (s *PGStore) downsampleFromRaw(ctx context.Context, key SeriesKey, spec DownsampleSpec, start, end time.Time) ([]Point, error) {
	if spec.Aggregation == DownsampleFirst || spec.Aggregation == DownsampleLast {
		return s.downsampleFromRawFirstLast(ctx, key, spec, start, end)
	}
	aggExpr, err := rawAggExprFor(spec.Aggregation)
	if err != nil {
		return nil, err
	}
	interval := pgInterval(spec.Step)
	sql := fmt.Sprintf(
		`SELECT time_bucket($1::INTERVAL, ts) AS bucket, %s AS v
		   FROM timeseries_points
		  WHERE ontology_rid = $2 AND object_type = $3 AND primary_key = $4 AND property = $5
		    AND ts >= $6 AND ts < $7
		    AND jsonb_typeof(value) = 'number'
		  GROUP BY bucket
		  ORDER BY bucket ASC`, aggExpr)
	rows, err := s.pool.Query(ctx, sql,
		interval,
		key.Ontology, key.ObjectType, key.PrimaryKey, key.Property,
		start, end)
	if err != nil {
		return nil, fmt.Errorf("timeseries: downsample raw: %w", err)
	}
	defer rows.Close()
	return scanBucketRows(rows)
}

// downsampleFromRawFirstLast handles the first/last aggregations against
// the raw table. When TimescaleDB is present we use the `first(value, ts)`
// / `last(value, ts)` hyperfunctions; otherwise we fall back to the
// portable `DISTINCT ON (bucket) ... ORDER BY bucket, ts ASC/DESC`
// pattern which produces identical results for the two aggregations.
func (s *PGStore) downsampleFromRawFirstLast(ctx context.Context, key SeriesKey, spec DownsampleSpec, start, end time.Time) ([]Point, error) {
	interval := pgInterval(spec.Step)
	if s.caggReady() {
		// Hyperfunctions available (TimescaleDB installed).
		fn := "last"
		if spec.Aggregation == DownsampleFirst {
			fn = "first"
		}
		sql := fmt.Sprintf(
			`SELECT time_bucket($1::INTERVAL, ts) AS bucket,
			        %s((value::text)::float8, ts) AS v
			   FROM timeseries_points
			  WHERE ontology_rid = $2 AND object_type = $3 AND primary_key = $4 AND property = $5
			    AND ts >= $6 AND ts < $7
			    AND jsonb_typeof(value) = 'number'
			  GROUP BY bucket
			  ORDER BY bucket ASC`, fn)
		rows, err := s.pool.Query(ctx, sql,
			interval,
			key.Ontology, key.ObjectType, key.PrimaryKey, key.Property,
			start, end)
		if err != nil {
			return nil, fmt.Errorf("timeseries: downsample raw %s: %w", fn, err)
		}
		defer rows.Close()
		return scanBucketRows(rows)
	}
	// Portable fallback for plain Postgres: DISTINCT ON the per-bucket
	// extremum of ts. Direction is ASC for first, DESC for last.
	order := "DESC"
	if spec.Aggregation == DownsampleFirst {
		order = "ASC"
	}
	sql := fmt.Sprintf(
		`SELECT DISTINCT ON (bucket) bucket, (value::text)::float8 AS v
		   FROM (
		      SELECT time_bucket($1::INTERVAL, ts) AS bucket, ts, value
		        FROM timeseries_points
		       WHERE ontology_rid = $2 AND object_type = $3 AND primary_key = $4 AND property = $5
		         AND ts >= $6 AND ts < $7
		         AND jsonb_typeof(value) = 'number'
		   ) sub
		  ORDER BY bucket ASC, ts %s
		`, order)
	rows, err := s.pool.Query(ctx, sql,
		interval,
		key.Ontology, key.ObjectType, key.PrimaryKey, key.Property,
		start, end)
	if err != nil {
		return nil, fmt.Errorf("timeseries: downsample raw first/last fallback: %w", err)
	}
	defer rows.Close()
	return scanBucketRows(rows)
}

// downsampleFromCAGG re-aggregates the 5-min cagg up to the caller's
// requested step. AVG is exact when each 5-min bucket has the same
// count_value or uses SUM/COUNT to weight; we emit the weighted form so
// non-uniform sampling within the source window does not bias the
// returned mean (`SUM(sum_value) / NULLIF(SUM(count_value), 0)`).
func (s *PGStore) downsampleFromCAGG(ctx context.Context, key SeriesKey, spec DownsampleSpec, start, end time.Time) ([]Point, error) {
	aggExpr, err := caggAggExprFor(spec.Aggregation)
	if err != nil {
		return nil, err
	}
	interval := pgInterval(spec.Step)
	// Note: literal view name is bound to caggViewName, never a caller
	// string, so interpolating it directly is injection-safe.
	sql := fmt.Sprintf(
		`SELECT time_bucket($1::INTERVAL, bucket) AS rebucket, %s AS v
		   FROM %s
		  WHERE ontology_rid = $2 AND object_type = $3 AND primary_key = $4 AND property = $5
		    AND bucket >= $6 AND bucket < $7
		  GROUP BY rebucket
		  ORDER BY rebucket ASC`, aggExpr, caggViewName)
	rows, err := s.pool.Query(ctx, sql,
		interval,
		key.Ontology, key.ObjectType, key.PrimaryKey, key.Property,
		start, end)
	if err != nil {
		return nil, fmt.Errorf("timeseries: downsample cagg: %w", err)
	}
	defer rows.Close()
	return scanBucketRows(rows)
}

// rawAggExprFor returns the SQL aggregate expression for a single bucket
// pulled directly from the raw timeseries_points table. The expression
// always operates on (value::text)::float8 because value is JSONB.
func rawAggExprFor(agg DownsampleAggregation) (string, error) {
	switch agg {
	case DownsampleAvg:
		return "AVG((value::text)::float8)", nil
	case DownsampleSum:
		return "SUM((value::text)::float8)", nil
	case DownsampleMin:
		return "MIN((value::text)::float8)", nil
	case DownsampleMax:
		return "MAX((value::text)::float8)", nil
	case DownsampleCount:
		return "COUNT(value)::float8", nil
	default:
		return "", fmt.Errorf("timeseries: raw aggregation %q not supported here", agg)
	}
}

// caggAggExprFor returns the SQL aggregate expression to re-aggregate
// the 5-min cagg up to the caller's bucket. AVG uses weighted form so
// non-uniform sampling does not bias the mean.
func caggAggExprFor(agg DownsampleAggregation) (string, error) {
	switch agg {
	case DownsampleAvg:
		return "SUM(sum_value) / NULLIF(SUM(count_value), 0)", nil
	case DownsampleSum:
		return "SUM(sum_value)", nil
	case DownsampleMin:
		return "MIN(min_value)", nil
	case DownsampleMax:
		return "MAX(max_value)", nil
	case DownsampleCount:
		return "SUM(count_value)::float8", nil
	default:
		return "", fmt.Errorf("timeseries: cagg aggregation %q not materialised", agg)
	}
}

// scanBucketRows decodes a (bucket, value) result set into []Point. A
// SQL NULL value (e.g. an all-null average) is rendered as a missing
// point — the bucket is dropped — so downstream charts do not have to
// handle nil values.
func scanBucketRows(rows pgx.Rows) ([]Point, error) {
	out := make([]Point, 0)
	for rows.Next() {
		var t time.Time
		var v *float64
		if err := rows.Scan(&t, &v); err != nil {
			return nil, err
		}
		if v == nil {
			continue
		}
		out = append(out, Point{Time: t, Value: *v})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// CAGGRefresher is the minimal contract the cagg refresh loop needs from
// a Postgres-backed timeseries store. *PGStore satisfies it via
// RefreshCAGG; tests can plug a fake to exercise the loop driver without
// booting PostgreSQL. US-467.
type CAGGRefresher interface {
	RefreshCAGG(ctx context.Context) error
}

// RefreshCAGG triggers a refresh of the timeseries_cagg_5min continuous
// aggregate. Idempotent and safe to call concurrently with pg_cron's own
// schedule; TimescaleDB serialises overlapping refreshes internally.
//
// The (NULL, NULL) window arguments refresh every materialisable range
// — equivalent to "refresh all the data". Production deployments that
// only need to catch up the most recent hour should call
// `refresh_continuous_aggregate('timeseries_cagg_5min', NOW() - INTERVAL
// '1 hour', NOW())` directly via the pool.
//
// When the cagg does not exist (plain Postgres) RefreshCAGG is a no-op
// and returns nil so the refresh loop stays harmless in degraded mode.
func (s *PGStore) RefreshCAGG(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return nil
	}
	if s.caggKnown.Load() == -1 {
		s.DetectCAGG(ctx)
	}
	if !s.caggReady() {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`CALL refresh_continuous_aggregate($1, NULL, NULL)`, caggViewName)
	if err != nil {
		return fmt.Errorf("timeseries: refresh_continuous_aggregate: %w", err)
	}
	return nil
}

// RunCAGGRefreshLoop drives refresher.RefreshCAGG on a fixed interval
// until ctx is cancelled. onRefresh (optional) fires after each
// successful refresh; onError (optional) receives transient errors so
// the loop survives PG hiccups. A nil refresher or non-positive interval
// is a no-op so degraded-mode boot (no PG pool, no TimescaleDB) is safe.
//
// The intended caller is cmd/server: spawn a goroutine on startup,
// `RunCAGGRefreshLoop(rootCtx, pgStore, 5*time.Minute, ...)`, and let
// context cancellation on graceful shutdown stop the loop. The loop is a
// fallback for environments without pg_cron — when pg_cron is installed
// migration 000209 schedules `*/5 * * * *` refreshes server-side and
// this goroutine becomes a redundant safety net (idempotent). US-467.
func RunCAGGRefreshLoop(ctx context.Context, refresher CAGGRefresher, interval time.Duration, onRefresh func(), onError func(error)) {
	if refresher == nil || interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := refresher.RefreshCAGG(ctx); err != nil {
				if onError != nil {
					onError(err)
				}
				continue
			}
			if onRefresh != nil {
				onRefresh()
			}
		}
	}
}
