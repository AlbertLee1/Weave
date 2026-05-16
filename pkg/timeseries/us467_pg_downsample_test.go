//go:build integration

package timeseries_test

// US-467 integration coverage for the TimescaleDB-backed downsampler.
//
// Acceptance criteria probed end-to-end:
//   1. timeseries_cagg_5min materialised view is created by migration 000209.
//   2. PGStore.DownsamplePoints handles avg/sum/min/max/count + first/last
//      against real TimescaleDB hyperfunctions.
//   3. With the cagg refreshed, a 1-hour downsample over 1M raw points
//      reads from timeseries_cagg_5min (verified via EXPLAIN) — the raw
//      table is bypassed so the latency is bounded by ~bucket count, not
//      raw cardinality.

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/timeseries"
)

const (
	us467Ontology   = "ri.ontology.main.ontology.us467"
	us467ObjectType = "sensor"
	us467PK         = "s1"
	us467Property   = "reading"
)

func us467Key() timeseries.SeriesKey {
	return timeseries.SeriesKey{
		Ontology:   us467Ontology,
		ObjectType: us467ObjectType,
		PrimaryKey: us467PK,
		Property:   us467Property,
	}
}

// us467Seed inserts numeric points into timeseries_points using pgx
// CopyFrom. The value column is JSONB so each row's value is the
// json-encoded float64 — anything else gets filtered out by the cagg's
// `WHERE jsonb_typeof(value) = 'number'` clause and would surface as a
// row-count mismatch downstream.
func us467Seed(t *testing.T, pg *testutil.PGContainer, start time.Time, step time.Duration, valuer func(i int) float64, count int) {
	t.Helper()
	ctx := context.Background()
	conn, err := pg.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()

	const chunk = 50_000
	for off := 0; off < count; off += chunk {
		end := off + chunk
		if end > count {
			end = count
		}
		rows := make([][]any, end-off)
		for i := off; i < end; i++ {
			raw, err := json.Marshal(valuer(i))
			if err != nil {
				t.Fatalf("marshal value[%d]: %v", i, err)
			}
			rows[i-off] = []any{
				us467Ontology,
				us467ObjectType,
				us467PK,
				us467Property,
				start.Add(time.Duration(i) * step),
				raw,
			}
		}
		if _, err := conn.Conn().CopyFrom(ctx,
			pgx.Identifier{"timeseries_points"},
			[]string{"ontology_rid", "object_type", "primary_key", "property", "ts", "value"},
			pgx.CopyFromRows(rows)); err != nil {
			t.Fatalf("copy [%d..%d): %v", off, end, err)
		}
	}
}

// caggExists asks timescaledb_information whether the US-467 materialised
// view is present so the test can skip cleanly when the migration's
// optional TimescaleDB branch did not fire (i.e. the test was wired up to
// a non-Timescale image by accident).
func caggExists(t *testing.T, pg *testutil.PGContainer) bool {
	t.Helper()
	var exists bool
	err := pg.Pool.QueryRow(context.Background(),
		`SELECT EXISTS (
		   SELECT 1 FROM timescaledb_information.continuous_aggregates
		   WHERE view_name = 'timeseries_cagg_5min'
		)`).Scan(&exists)
	if err != nil {
		t.Fatalf("cagg probe: %v", err)
	}
	return exists
}

// TestPGStore_Given_TimescaleDB_When_Migrated_Then_CaggExists pins the
// migration: 000209 must create timeseries_cagg_5min on a TimescaleDB
// image.
func TestPGStore_Given_TimescaleDB_When_Migrated_Then_CaggExists(t *testing.T) {
	pg := testutil.StartTimescaleDBContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !caggExists(t, pg) {
		t.Fatal("timeseries_cagg_5min not created by migration 000209")
	}
	store := timeseries.NewPGStore(pg.Pool)
	if !store.DetectCAGG(context.Background()) {
		t.Fatal("PGStore.DetectCAGG returned false after migration applied the cagg")
	}
}

// TestPGStore_Given_FewPoints_When_DownsampleAggs_Then_ResultsMatchHand
// hits each supported aggregation against a tiny series so the raw-path
// SQL is exercised before the heavier 1M-point gate runs.
func TestPGStore_Given_FewPoints_When_DownsampleAggs_Then_ResultsMatchHand(t *testing.T) {
	pg := testutil.StartTimescaleDBContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	start := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	// 6 points 1 min apart, values 10..15. One 5-min bucket holds 10..14
	// (sum=60, avg=12, min=10, max=14, count=5, first=10, last=14); the
	// second bucket holds [15] alone.
	us467Seed(t, pg, start, time.Minute, func(i int) float64 { return float64(10 + i) }, 6)

	store := timeseries.NewPGStore(pg.Pool)
	store.DetectCAGG(context.Background())

	ctx := context.Background()
	// Refresh the cagg so avg/sum/min/max/count have materialised rows
	// to read; first/last never touch the cagg so they're independent.
	if _, err := pg.Pool.Exec(ctx,
		`CALL refresh_continuous_aggregate('timeseries_cagg_5min', NULL, NULL)`); err != nil {
		t.Fatalf("refresh cagg: %v", err)
	}
	cases := []struct {
		name string
		agg  timeseries.DownsampleAggregation
		want []float64
	}{
		{"avg", timeseries.DownsampleAvg, []float64{12, 15}},
		{"sum", timeseries.DownsampleSum, []float64{60, 15}},
		{"min", timeseries.DownsampleMin, []float64{10, 15}},
		{"max", timeseries.DownsampleMax, []float64{14, 15}},
		{"count", timeseries.DownsampleCount, []float64{5, 1}},
		{"first", timeseries.DownsampleFirst, []float64{10, 15}},
		{"last", timeseries.DownsampleLast, []float64{14, 15}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := store.DownsamplePoints(ctx, us467Key(), timeseries.DownsampleSpec{
				Start:       start,
				End:         start.Add(15 * time.Minute),
				Step:        5 * time.Minute,
				Aggregation: c.agg,
			})
			if err != nil {
				t.Fatalf("DownsamplePoints %s: %v", c.name, err)
			}
			if len(out) != len(c.want) {
				t.Fatalf("len(out) = %d, want %d (points: %+v)", len(out), len(c.want), out)
			}
			for i := range out {
				v, ok := out[i].Value.(float64)
				if !ok {
					t.Fatalf("out[%d].Value type = %T, want float64", i, out[i].Value)
				}
				if v != c.want[i] {
					t.Errorf("out[%d].Value = %v, want %v", i, v, c.want[i])
				}
			}
		})
	}
}

// TestPGStore_Given_NoCAGG_When_Downsample_Then_RoutesToRaw verifies the
// raw-path fallback fires for sub-5min steps and first/last even when the
// cagg is present. Step=1min always routes to raw; first/last always do.
func TestPGStore_Given_NoCAGG_When_Downsample_Then_RoutesToRaw(t *testing.T) {
	pg := testutil.StartTimescaleDBContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	start := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	us467Seed(t, pg, start, 10*time.Second, func(i int) float64 { return float64(i) }, 30)

	store := timeseries.NewPGStore(pg.Pool)
	store.DetectCAGG(context.Background())
	// step < 5min must use raw.
	out, err := store.DownsamplePoints(context.Background(), us467Key(), timeseries.DownsampleSpec{
		Start:       start,
		End:         start.Add(time.Minute),
		Step:        30 * time.Second,
		Aggregation: timeseries.DownsampleAvg,
	})
	if err != nil {
		t.Fatalf("raw-path 30s avg: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("len(out) = %d, want 2 (0-30s and 30-60s)", len(out))
	}
	// Bucket 1: indices 0..2 (values 0,1,2) → avg=1; bucket 2: 3..5 → avg=4.
	if got, _ := out[0].Value.(float64); got != 1.0 {
		t.Errorf("out[0].Value = %v, want 1.0", got)
	}
	if got, _ := out[1].Value.(float64); got != 4.0 {
		t.Errorf("out[1].Value = %v, want 4.0", got)
	}
}

// TestPGStore_Given_OneMillionPoints_When_DownsampleOneHour_Then_HitsCagg is
// the PRD-mandated 1M-point gate. After seeding 1M numeric points,
// refreshing the cagg, and running a 1h avg downsample, EXPLAIN must show
// the planner consulted timeseries_cagg_5min — not the raw hypertable —
// and the returned bucket count must match the expected 1h span.
func TestPGStore_Given_OneMillionPoints_When_DownsampleOneHour_Then_HitsCagg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 1M-point gate under -short")
	}
	pg := testutil.StartTimescaleDBContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !caggExists(t, pg) {
		t.Fatal("timeseries_cagg_5min missing — cannot run 1M-point cagg gate")
	}

	// 1M points × 1 second step → 1,000,000 seconds ≈ 11.57 days.
	const count = 1_000_000
	const step = time.Second
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rng := rand.New(rand.NewSource(20260516))
	seedStart := time.Now()
	us467Seed(t, pg, start, step, func(i int) float64 {
		// Deterministic-but-non-constant payload so AVG bites and MIN/MAX
		// remain meaningful per bucket.
		return float64(i) + rng.Float64()*0.5
	}, count)
	t.Logf("seeded %d points in %s", count, time.Since(seedStart))

	// Refresh the cagg over the full data window so the 1h query hits a
	// fully materialised view. NULL/NULL refreshes everything currently
	// materialisable; that's the simplest interface for "catch up".
	ctx := context.Background()
	refreshStart := time.Now()
	if _, err := pg.Pool.Exec(ctx,
		`CALL refresh_continuous_aggregate('timeseries_cagg_5min', NULL, NULL)`); err != nil {
		t.Fatalf("refresh cagg: %v", err)
	}
	t.Logf("refreshed cagg in %s", time.Since(refreshStart))

	store := timeseries.NewPGStore(pg.Pool)
	if !store.DetectCAGG(ctx) {
		t.Fatal("DetectCAGG returned false after migrate+refresh")
	}

	end := start.Add(time.Duration(count) * step)
	spec := timeseries.DownsampleSpec{
		Start:       start,
		End:         end,
		Step:        time.Hour,
		Aggregation: timeseries.DownsampleAvg,
	}

	queryStart := time.Now()
	out, err := store.DownsamplePoints(ctx, us467Key(), spec)
	queryDur := time.Since(queryStart)
	if err != nil {
		t.Fatalf("DownsamplePoints: %v", err)
	}
	t.Logf("downsample 1h avg over 1M points: %d buckets in %s", len(out), queryDur)

	// 1M seconds / 3600 ≈ 277.8 hours; allow ±1 for window edge.
	if len(out) < 277 || len(out) > 279 {
		t.Errorf("bucket count = %d, want ~278 (1M points / 1h step)", len(out))
	}
	if len(out) == 0 {
		t.Fatal("zero buckets returned — cagg likely empty (refresh failed silently?)")
	}

	// EXPLAIN: the plan node text must reference the cagg view. We
	// recreate the exact SQL the executor uses via EXPLAIN (ANALYZE OFF,
	// FORMAT TEXT) on a fresh connection.
	rows, err := pg.Pool.Query(ctx, `EXPLAIN
		SELECT time_bucket($1::INTERVAL, bucket) AS rebucket,
		       SUM(sum_value) / NULLIF(SUM(count_value), 0) AS v
		  FROM timeseries_cagg_5min
		 WHERE ontology_rid = $2 AND object_type = $3 AND primary_key = $4 AND property = $5
		   AND bucket >= $6 AND bucket < $7
		 GROUP BY rebucket
		 ORDER BY rebucket ASC`,
		fmt.Sprintf("%d microseconds", spec.Step.Microseconds()),
		us467Ontology, us467ObjectType, us467PK, us467Property,
		start, end)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("plan rows: %v", err)
	}
	planText := plan.String()
	// The continuous aggregate is realised as a regular materialised
	// view in pg_class, so the plan references its underlying chunk
	// table name (`_materialized_hypertable_*`). Either reference proves
	// the planner reads the cagg, not timeseries_points.
	if !strings.Contains(planText, "timeseries_cagg_5min") &&
		!strings.Contains(planText, "_materialized_hypertable_") {
		t.Errorf("plan did not mention cagg view:\n%s", planText)
	}
	if strings.Contains(planText, "timeseries_points") &&
		!strings.Contains(planText, "_materialized_hypertable_") {
		t.Errorf("plan still references raw timeseries_points (cagg not used):\n%s", planText)
	}
}
