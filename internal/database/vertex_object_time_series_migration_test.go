//go:build integration

package database_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
)

// TestVertexObjectTimeSeriesMigration_Given_FreshTimescaleDB_When_MigrateUp_Then_TableExists
// covers VTX-028 BDD #1 — the migration creates the object_time_series base
// table with all expected columns and a composite primary key.
func TestVertexObjectTimeSeriesMigration_Given_FreshTimescaleDB_When_MigrateUp_Then_TableExists(t *testing.T) {
	pg := testutil.StartTimescaleDBContainer(t)

	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migration up failed: %v", err)
	}

	if !pg.TableExists(t, "object_time_series") {
		t.Fatal("expected object_time_series table to exist")
	}

	ctx := context.Background()
	columns := map[string]string{
		"object_rid": "text",
		"property":   "text",
		"ts":         "timestamp with time zone",
		"value":      "double precision",
		"quality":    "smallint",
	}
	for col, dataType := range columns {
		var actual string
		err := pg.Pool.QueryRow(ctx, `
			SELECT data_type FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'object_time_series' AND column_name = $1`,
			col).Scan(&actual)
		if err != nil {
			t.Errorf("column %s missing: %v", col, err)
			continue
		}
		if actual != dataType {
			t.Errorf("column %s: expected type %q, got %q", col, dataType, actual)
		}
	}
}

// TestVertexObjectTimeSeriesMigration_Given_FreshTimescaleDB_When_MigrateUp_Then_HypertableRegistered
// covers VTX-028 BDD #1 — verify SELECT create_hypertable('object_time_series', 'ts').
func TestVertexObjectTimeSeriesMigration_Given_FreshTimescaleDB_When_MigrateUp_Then_HypertableRegistered(t *testing.T) {
	pg := testutil.StartTimescaleDBContainer(t)
	ctx := context.Background()

	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migration up failed: %v", err)
	}

	var hypertableName string
	err := pg.Pool.QueryRow(ctx, `
		SELECT hypertable_name FROM timescaledb_information.hypertables
		WHERE hypertable_name = 'object_time_series'`).Scan(&hypertableName)
	if err != nil {
		t.Fatalf("expected object_time_series to be a hypertable: %v", err)
	}
	if hypertableName != "object_time_series" {
		t.Errorf("got hypertable %q, want object_time_series", hypertableName)
	}
}

// TestVertexObjectTimeSeriesMigration_Given_FreshTimescaleDB_When_MigrateUp_Then_ContinuousAggregateExists
// covers VTX-028 BDD #3 — the cagg_5min continuous aggregate exists.
func TestVertexObjectTimeSeriesMigration_Given_FreshTimescaleDB_When_MigrateUp_Then_ContinuousAggregateExists(t *testing.T) {
	pg := testutil.StartTimescaleDBContainer(t)
	ctx := context.Background()

	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migration up failed: %v", err)
	}

	var name string
	err := pg.Pool.QueryRow(ctx, `
		SELECT view_name FROM timescaledb_information.continuous_aggregates
		WHERE view_name = 'cagg_5min'`).Scan(&name)
	if err != nil {
		t.Fatalf("expected continuous aggregate cagg_5min: %v", err)
	}
}

// TestVertexObjectTimeSeriesMigration_Given_FreshTimescaleDB_When_Insert100k_Then_UnderFiveSeconds
// covers VTX-028 BDD #2 — 100k point insert ≤ 5 s. Uses COPY which is the
// recommended bulk-ingest path for TimescaleDB.
func TestVertexObjectTimeSeriesMigration_Given_FreshTimescaleDB_When_Insert100k_Then_UnderFiveSeconds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping perf benchmark in -short mode")
	}
	pg := testutil.StartTimescaleDBContainer(t)
	ctx := context.Background()

	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migration up failed: %v", err)
	}

	conn, err := pg.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire conn: %v", err)
	}
	defer conn.Release()

	const n = 100000
	rows := make([][]any, n)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		rows[i] = []any{
			"ri.ontology.main.object.JFK",
			"throughput",
			start.Add(time.Duration(i) * time.Second),
			float64(100 + i%1000),
			int16(1),
		}
	}

	t0 := time.Now()
	copyCount, err := conn.Conn().CopyFrom(ctx,
		pgx.Identifier{"object_time_series"},
		[]string{"object_rid", "property", "ts", "value", "quality"},
		pgx.CopyFromRows(rows))
	elapsed := time.Since(t0)
	if err != nil {
		t.Fatalf("copy failed: %v", err)
	}
	if copyCount != int64(n) {
		t.Errorf("expected %d rows copied, got %d", n, copyCount)
	}
	if elapsed > 5*time.Second {
		t.Errorf("insert of %d rows took %v; want ≤ 5s", n, elapsed)
	}
	t.Logf("inserted %d rows in %v (%.0f rows/sec)", n, elapsed, float64(n)/elapsed.Seconds())
}

// TestVertexObjectTimeSeriesMigration_Given_100kPoints_When_QueryCagg_Then_UnderFiftyMs
// covers VTX-028 BDD #3 — SELECT AVG via cagg_5min ≤ 50 ms.
func TestVertexObjectTimeSeriesMigration_Given_100kPoints_When_QueryCagg_Then_UnderFiftyMs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping perf benchmark in -short mode")
	}
	pg := testutil.StartTimescaleDBContainer(t)
	ctx := context.Background()

	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migration up failed: %v", err)
	}

	// Seed 100k points.
	conn, err := pg.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire conn: %v", err)
	}
	const n = 100000
	rows := make([][]any, n)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		rows[i] = []any{
			"ri.ontology.main.object.JFK",
			"throughput",
			start.Add(time.Duration(i) * time.Second),
			float64(100 + i%1000),
			int16(1),
		}
	}
	if _, err := conn.Conn().CopyFrom(ctx,
		pgx.Identifier{"object_time_series"},
		[]string{"object_rid", "property", "ts", "value", "quality"},
		pgx.CopyFromRows(rows)); err != nil {
		conn.Release()
		t.Fatalf("copy failed: %v", err)
	}
	conn.Release()

	// Force-refresh the continuous aggregate so the cagg has rows to read.
	if _, err := pg.Pool.Exec(ctx,
		fmt.Sprintf(`CALL refresh_continuous_aggregate('cagg_5min', '%s', '%s')`,
			start.Format("2006-01-02 15:04:05"),
			start.Add(time.Duration(n+1)*time.Second).Format("2006-01-02 15:04:05"))); err != nil {
		// Some TimescaleDB versions need NULL bounds; retry.
		if !strings.Contains(err.Error(), "out of range") {
			t.Fatalf("refresh cagg failed: %v", err)
		}
	}

	// Warm up
	for i := 0; i < 3; i++ {
		_, _ = pg.Pool.Exec(ctx, `SELECT bucket, avg_value FROM cagg_5min WHERE object_rid = $1 AND property = $2 LIMIT 100`,
			"ri.ontology.main.object.JFK", "throughput")
	}

	t0 := time.Now()
	rowsResult, err := pg.Pool.Query(ctx,
		`SELECT bucket, avg_value FROM cagg_5min WHERE object_rid = $1 AND property = $2 ORDER BY bucket`,
		"ri.ontology.main.object.JFK", "throughput")
	if err != nil {
		t.Fatalf("cagg query failed: %v", err)
	}
	count := 0
	for rowsResult.Next() {
		var bucket time.Time
		var avg float64
		if err := rowsResult.Scan(&bucket, &avg); err != nil {
			rowsResult.Close()
			t.Fatalf("scan failed: %v", err)
		}
		count++
	}
	rowsResult.Close()
	elapsed := time.Since(t0)

	if count == 0 {
		t.Error("expected cagg_5min to contain at least one bucket")
	}
	if elapsed > 50*time.Millisecond {
		t.Errorf("cagg AVG query took %v over %d points; want ≤ 50ms", elapsed, count)
	}
	t.Logf("read %d 5-min buckets in %v from cagg_5min", count, elapsed)
}
