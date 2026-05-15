//go:build integration

package timeseries_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/timeseries"
)

// VTX-029: Time Series Service (Query API).
//
// The integration tests below seed the VTX-028 object_time_series hypertable
// with deterministic numeric points and exercise the new VertexService.Query
// path: window aggregation (AVG/MIN/MAX/SUM/LAST), scenario override, and
// the "missing_data" warning surface.

const (
	testObjectRID = "ri.ontology.main.object.JFK"
	testProperty  = "throughput"
)

// seedPoints writes 200 points spaced 60 s apart starting at start.
// value[i] = base + i so MIN/MAX/AVG/SUM/LAST are predictable.
func seedPoints(t *testing.T, pg *testutil.PGContainer, start time.Time, base, count int) {
	t.Helper()
	ctx := context.Background()
	conn, err := pg.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()

	rows := make([][]any, count)
	for i := 0; i < count; i++ {
		rows[i] = []any{
			testObjectRID,
			testProperty,
			start.Add(time.Duration(i) * time.Minute),
			float64(base + i),
			int16(1),
		}
	}
	if _, err := conn.Conn().CopyFrom(ctx,
		pgx.Identifier{"object_time_series"},
		[]string{"object_rid", "property", "ts", "value", "quality"},
		pgx.CopyFromRows(rows)); err != nil {
		t.Fatalf("copy: %v", err)
	}
}

// stubOverlay is a test scenario reader returning a configurable override.
type stubOverlay struct {
	scenarioID string
	objectRID  string
	property   string
	value      *float64
	notFound   bool
}

func (s stubOverlay) GetWindowedScalarOverride(_ context.Context, scenarioID, objectRID, property string) (*float64, error) {
	if s.notFound {
		return nil, timeseries.ErrScenarioNotFound
	}
	if scenarioID != s.scenarioID || objectRID != s.objectRID || property != s.property {
		return nil, nil
	}
	return s.value, nil
}

// TestVertexTimeSeries_Given_HypertableSeeded_When_QueryAvg5Min_Then_BucketSeries
// covers VTX-029 BDD #1.
func TestVertexTimeSeries_Given_HypertableSeeded_When_QueryAvg5Min_Then_BucketSeries(t *testing.T) {
	pg := testutil.StartTimescaleDBContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	start := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	seedPoints(t, pg, start, 100, 30) // 30 minutes of data

	svc := timeseries.NewVertexService(pg.Pool)
	res, err := svc.Query(context.Background(), timeseries.VertexQuery{
		ObjectRID: testObjectRID,
		Property:  testProperty,
		From:      start,
		To:        start.Add(30 * time.Minute),
		Agg:       timeseries.AggAvg,
		Bucket:    5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(res.Points) != 6 {
		t.Fatalf("expected 6 buckets, got %d", len(res.Points))
	}
	// First 5-min bucket covers i=0..4 → avg(100..104) = 102
	if got, want := res.Points[0].Value, 102.0; got != want {
		t.Errorf("bucket[0].value = %v, want %v", got, want)
	}
	if got, want := res.Points[5].Value, 127.0; got != want {
		t.Errorf("bucket[5].value = %v, want %v", got, want)
	}
}

// TestVertexTimeSeries_Given_HypertableSeeded_When_QueryAggVariants_Then_MatchSQL
// covers VTX-029 BDD #2 (AVG/MIN/MAX/SUM/LAST).
func TestVertexTimeSeries_Given_HypertableSeeded_When_QueryAggVariants_Then_MatchSQL(t *testing.T) {
	pg := testutil.StartTimescaleDBContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	start := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	seedPoints(t, pg, start, 100, 5) // 5 points: values 100..104, ts +0..+4min

	svc := timeseries.NewVertexService(pg.Pool)
	cases := []struct {
		agg  timeseries.Agg
		want float64
	}{
		{timeseries.AggAvg, 102},
		{timeseries.AggMin, 100},
		{timeseries.AggMax, 104},
		{timeseries.AggSum, 510},
		{timeseries.AggLast, 104},
	}
	for _, c := range cases {
		t.Run(string(c.agg), func(t *testing.T) {
			res, err := svc.Query(context.Background(), timeseries.VertexQuery{
				ObjectRID: testObjectRID,
				Property:  testProperty,
				From:      start,
				To:        start.Add(5 * time.Minute),
				Agg:       c.agg,
				Bucket:    5 * time.Minute,
			})
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			if len(res.Points) != 1 {
				t.Fatalf("expected 1 bucket, got %d", len(res.Points))
			}
			if got := res.Points[0].Value; got != c.want {
				t.Errorf("%s = %v, want %v", c.agg, got, c.want)
			}
		})
	}
}

// TestVertexTimeSeries_Given_ScenarioOverride_When_Query_Then_OverrideScalarReplacesBucket
// covers VTX-029 BDD #3.
func TestVertexTimeSeries_Given_ScenarioOverride_When_Query_Then_OverrideScalarReplacesBucket(t *testing.T) {
	pg := testutil.StartTimescaleDBContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	start := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	seedPoints(t, pg, start, 100, 10) // base AVG would be ~104.5 over 10 buckets-of-1

	overrideValue := 999.0
	overlay := stubOverlay{
		scenarioID: "ri.vertex.main.scenario.s1",
		objectRID:  testObjectRID,
		property:   testProperty,
		value:      &overrideValue,
	}
	svc := timeseries.NewVertexService(pg.Pool, timeseries.WithScenarioOverlay(overlay))
	res, err := svc.Query(context.Background(), timeseries.VertexQuery{
		ObjectRID:  testObjectRID,
		Property:   testProperty,
		From:       start,
		To:         start.Add(10 * time.Minute),
		Agg:        timeseries.AggAvg,
		Bucket:     5 * time.Minute,
		ScenarioID: "ri.vertex.main.scenario.s1",
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(res.Points) != 2 {
		t.Fatalf("expected 2 buckets, got %d", len(res.Points))
	}
	for i, p := range res.Points {
		if p.Value != overrideValue {
			t.Errorf("bucket[%d].value = %v, want override %v", i, p.Value, overrideValue)
		}
	}
}

// TestVertexTimeSeries_Given_StaleData_When_Query_Then_MissingDataWarning
// covers VTX-029 BDD #4.
func TestVertexTimeSeries_Given_StaleData_When_Query_Then_MissingDataWarning(t *testing.T) {
	pg := testutil.StartTimescaleDBContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	start := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	seedPoints(t, pg, start, 100, 5) // 5 points within [start, start+5min)
	queryTo := start.Add(48 * time.Hour) // far beyond last point

	svc := timeseries.NewVertexService(pg.Pool,
		timeseries.WithMissingDataWarningHours(6))
	res, err := svc.Query(context.Background(), timeseries.VertexQuery{
		ObjectRID: testObjectRID,
		Property:  testProperty,
		From:      start,
		To:        queryTo,
		Agg:       timeseries.AggAvg,
		Bucket:    5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if res.Warning != "missing_data" {
		t.Errorf("expected warning=missing_data, got %q", res.Warning)
	}
	if res.LastObservedAt == nil {
		t.Fatal("expected LastObservedAt to be set")
	}
	// Last seeded point ts = start + 4 min
	want := start.Add(4 * time.Minute)
	if !res.LastObservedAt.Equal(want) {
		t.Errorf("LastObservedAt = %v, want %v", *res.LastObservedAt, want)
	}
}

// TestVertexTimeSeries_Given_RecentData_When_Query_Then_NoWarning
// covers the inverse of BDD #4 — fresh data must NOT trip the warning.
func TestVertexTimeSeries_Given_RecentData_When_Query_Then_NoWarning(t *testing.T) {
	pg := testutil.StartTimescaleDBContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	start := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	seedPoints(t, pg, start, 100, 10) // 10 minutes of data

	svc := timeseries.NewVertexService(pg.Pool,
		timeseries.WithMissingDataWarningHours(24))
	res, err := svc.Query(context.Background(), timeseries.VertexQuery{
		ObjectRID: testObjectRID,
		Property:  testProperty,
		From:      start,
		To:        start.Add(10 * time.Minute),
		Agg:       timeseries.AggAvg,
		Bucket:    5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if res.Warning != "" {
		t.Errorf("expected no warning, got %q", res.Warning)
	}
}
