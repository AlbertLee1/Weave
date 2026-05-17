//go:build integration

package geotemporal_test

import (
	"context"
	"encoding/json"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/geotemporal"
)

// pgSpatialFixture is the spatial-test sibling of pgFixture: it provides
// the freshly-migrated Postgres container plus a *PgStore handle and the
// pool itself (for the COPY-bulk insert path used by the 1M test).
func pgSpatialFixture(t testing.TB) (*geotemporal.PgStore, *testutil.PGContainer) {
	t.Helper()
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	return geotemporal.NewPgStore(pg.Pool), pg
}

func us466SpatialKey() geotemporal.SeriesKey {
	return geotemporal.SeriesKey{
		Ontology:   "ri.ontology.main.ontology.demo",
		ObjectType: "vehicle",
		PrimaryKey: "v1",
		Property:   "track",
	}
}

func TestPgStore_Given_MixedPoints_When_QueryBBoxRange_Then_OnlyBBoxAndTimeMatchesReturned(t *testing.T) {
	store, _ := pgSpatialFixture(t)
	ctx := context.Background()
	key := us466SpatialKey()

	base, _ := time.Parse(time.RFC3339, "2026-04-01T00:00:00Z")
	seeds := []geotemporal.Value{
		{Time: base.Add(1 * time.Hour), Position: point(-122.40, 37.78)},  // in
		{Time: base.Add(2 * time.Hour), Position: point(-122.39, 37.77)},  // in
		{Time: base.Add(10 * 24 * time.Hour), Position: point(-122.41, 37.79)},
		{Time: base.Add(3 * time.Hour), Position: point(-100.00, 40.00)},
		{Time: base.Add(20 * 24 * time.Hour), Position: point(0.0, 0.0)},
	}
	for _, v := range seeds {
		if err := store.AppendValue(ctx, key, v); err != nil {
			t.Fatalf("AppendValue: %v", err)
		}
	}

	bbox := geotemporal.BBox{MinLng: -122.5, MinLat: 37.5, MaxLng: -122.0, MaxLat: 38.0}
	tr := geotemporal.TimeRange{Start: base, End: base.Add(24 * time.Hour)}
	got, err := store.QueryBBoxRange(ctx, key, bbox, tr)
	if err != nil {
		t.Fatalf("QueryBBoxRange: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d values, want 2", len(got))
	}
	if !got[0].Time.Equal(base.Add(1*time.Hour)) || !got[1].Time.Equal(base.Add(2*time.Hour)) {
		t.Errorf("values not sorted ASC or wrong set: %+v", got)
	}
}

func TestPgStore_Given_ZeroTimeBounds_When_QueryBBoxRange_Then_UnboundedOnThatSide(t *testing.T) {
	store, _ := pgSpatialFixture(t)
	ctx := context.Background()
	key := us466SpatialKey()

	t1, _ := time.Parse(time.RFC3339, "2020-01-01T00:00:00Z")
	t2, _ := time.Parse(time.RFC3339, "2030-01-01T00:00:00Z")
	for _, v := range []geotemporal.Value{
		{Time: t1, Position: point(-122.4, 37.7)},
		{Time: t2, Position: point(-122.4, 37.7)},
	} {
		if err := store.AppendValue(ctx, key, v); err != nil {
			t.Fatalf("AppendValue: %v", err)
		}
	}
	bbox := geotemporal.BBox{MinLng: -180, MinLat: -90, MaxLng: 180, MaxLat: 90}

	got, err := store.QueryBBoxRange(ctx, key, bbox, geotemporal.TimeRange{})
	if err != nil {
		t.Fatalf("QueryBBoxRange unbounded: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("unbounded: got %d, want 2", len(got))
	}

	got, err = store.QueryBBoxRange(ctx, key, bbox, geotemporal.TimeRange{Start: t1.Add(time.Hour)})
	if err != nil {
		t.Fatalf("QueryBBoxRange Start-only: %v", err)
	}
	if len(got) != 1 || !got[0].Time.Equal(t2) {
		t.Errorf("Start-only: got %+v, want [%v]", got, t2)
	}

	got, err = store.QueryBBoxRange(ctx, key, bbox, geotemporal.TimeRange{End: t1.Add(time.Hour)})
	if err != nil {
		t.Fatalf("QueryBBoxRange End-only: %v", err)
	}
	if len(got) != 1 || !got[0].Time.Equal(t1) {
		t.Errorf("End-only: got %+v, want [%v]", got, t1)
	}
}

// TestPgStore_Given_TsBtreeAndSpatialIndexes_When_Inspected_Then_Present
// pins down migration 000208's acceptance criteria — the standalone btree
// on ts plus the lng/lat functional expression indexes have to exist after
// migrations run, and they must target the geotemporal_values table.
func TestPgStore_Given_TsBtreeAndSpatialIndexes_When_Inspected_Then_Present(t *testing.T) {
	_, pg := pgSpatialFixture(t)
	ctx := context.Background()

	wantIndexes := []string{
		"idx_geotemporal_values_ts",
		"idx_geotemporal_values_lng",
		"idx_geotemporal_values_lat",
	}
	for _, name := range wantIndexes {
		var exists bool
		err := pg.Pool.QueryRow(ctx,
			`SELECT EXISTS (
			    SELECT 1 FROM pg_indexes
			     WHERE schemaname='public' AND tablename='geotemporal_values' AND indexname=$1
			 )`, name).Scan(&exists)
		if err != nil {
			t.Fatalf("lookup %s: %v", name, err)
		}
		if !exists {
			t.Errorf("index %s missing after migrations", name)
		}
	}
}

// TestPgStore_Given_OneMillionPoints_When_BBoxAndTimeFilter_Then_P99Under100ms
// is the US-466 hard PRD gate: 1M (ts, position) rows in one series, the
// bbox+time double filter must complete with P99 under 100ms. We seed via
// pgx CopyFrom so the test stays under the integration suite's wall-clock
// budget; the latency assertion measures only the steady-state query path.
func TestPgStore_Given_OneMillionPoints_When_BBoxAndTimeFilter_Then_P99Under100ms(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 1M-point P99 gate in -short mode")
	}
	store, pg := pgSpatialFixture(t)
	ctx := context.Background()
	key := us466SpatialKey()

	const n = 1_000_000
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Deterministic-but-uniform spread: ts marches forward one minute per
	// row, while position is drawn from a seeded PRNG. The earlier grid
	// shape had longitude cycling every 1000 rows and latitude advancing
	// every 1000 rows, which collapsed the spatial axis to ~1 row per
	// (lng, lat) bucket and made any 24h x 20x10° bbox return zero rows
	// — the planner could happily ignore the spatial index and still
	// "pass" the 100ms gate. Uniform random positions force the planner
	// to use both indexes to keep latency under budget.
	seedRng := rand.New(rand.NewSource(20260516))
	rows := make([][]any, n)
	for i := 0; i < n; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		lng := -180.0 + seedRng.Float64()*360.0
		lat := -90.0 + seedRng.Float64()*180.0
		posJSON, _ := json.Marshal(map[string]any{
			"type":        "Point",
			"coordinates": []float64{lng, lat},
		})
		rows[i] = []any{key.Ontology, key.ObjectType, key.PrimaryKey, key.Property, ts, posJSON}
	}

	tStart := time.Now()
	copied, err := pg.Pool.CopyFrom(ctx,
		pgx.Identifier{"geotemporal_values"},
		[]string{"ontology", "object_type", "primary_key", "property", "ts", "position"},
		pgx.CopyFromRows(rows))
	if err != nil {
		t.Fatalf("bulk copy: %v", err)
	}
	t.Logf("seeded %d rows via COPY in %s", copied, time.Since(tStart))
	if _, err := pg.Pool.Exec(ctx, "ANALYZE geotemporal_values"); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	// Steady-state warmup so the planner / shared buffers are primed before
	// we start measuring. One pass through a 7-day x 20x10° window the
	// P99 samples will use.
	warmupBBox := geotemporal.BBox{MinLng: -10, MinLat: -5, MaxLng: 10, MaxLat: 5}
	warmupTR := geotemporal.TimeRange{Start: base.Add(24 * time.Hour), End: base.Add(8 * 24 * time.Hour)}
	if _, err := store.QueryBBoxRange(ctx, key, warmupBBox, warmupTR); err != nil {
		t.Fatalf("warmup query: %v", err)
	}

	iters := 25
	samples := make([]time.Duration, iters)
	totalRows := 0
	rng := rand.New(rand.NewSource(20260517))
	for i := 0; i < iters; i++ {
		// Pick a random 7-day window inside the seed range and a 20x10°
		// bbox somewhere in [-180,180)x[-90,90). Over uniform 1M points the
		// expected hit count is ~31 rows per query, big enough that the
		// JSONB decode path is exercised but small enough that scan cost
		// stays in the index-scan regime.
		windowStart := base.Add(time.Duration(rng.Intn(int(n)-10080)) * time.Minute)
		tr := geotemporal.TimeRange{Start: windowStart, End: windowStart.Add(7 * 24 * time.Hour)}
		lngLo := -180.0 + rng.Float64()*340.0
		latLo := -90.0 + rng.Float64()*170.0
		bbox := geotemporal.BBox{MinLng: lngLo, MinLat: latLo, MaxLng: lngLo + 20, MaxLat: latLo + 10}

		t0 := time.Now()
		got, err := store.QueryBBoxRange(ctx, key, bbox, tr)
		if err != nil {
			t.Fatalf("QueryBBoxRange iter %d: %v", i, err)
		}
		samples[i] = time.Since(t0)
		totalRows += len(got)
	}
	if totalRows == 0 {
		t.Fatalf("all %d queries returned 0 rows — seed/bbox shape is degenerate, the latency gate is meaningless", iters)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	idx := int(float64(iters)*0.99) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= iters {
		idx = iters - 1
	}
	p99 := samples[idx]
	budget := 100 * time.Millisecond
	if p99 > budget {
		t.Fatalf("1M-point bbox+time P99 = %s exceeds US-466 budget %s (samples=%v)", p99, budget, samples)
	}
	t.Logf("1M-point bbox+time P99 = %s (budget %s; min=%s max=%s; rows-over-%d-iters=%d)",
		p99, budget, samples[0], samples[iters-1], iters, totalRows)
}
