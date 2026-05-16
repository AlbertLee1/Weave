//go:build integration

package geotemporal_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/geotemporal"
)

// pgFixture starts a Postgres container, applies all migrations, and returns
// a fresh PgStore bound to the container's pool. Tests get a fully migrated
// schema including the new migrations/000205_geotemporal_values.up.sql.
func pgFixture(t *testing.T) (*geotemporal.PgStore, func() *geotemporal.PgStore) {
	t.Helper()
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	if !pg.TableExists(t, "geotemporal_values") {
		t.Fatal("migration 000205 did not create geotemporal_values table")
	}
	store := geotemporal.NewPgStore(pg.Pool)
	// reopen returns a fresh PgStore over the same pool to simulate a
	// process restart without losing the underlying DB state.
	reopen := func() *geotemporal.PgStore { return geotemporal.NewPgStore(pg.Pool) }
	return store, reopen
}

func point(lng, lat float64) map[string]interface{} {
	return map[string]interface{}{
		"type":        "Point",
		"coordinates": []interface{}{lng, lat},
	}
}

func TestPgStore_Given_EmptySeries_When_LatestValue_Then_ErrNoValues(t *testing.T) {
	store, _ := pgFixture(t)
	ctx := context.Background()
	key := geotemporal.SeriesKey{Ontology: "ri.ontology.main.ontology.demo", ObjectType: "vehicle", PrimaryKey: "v1", Property: "track"}

	if _, err := store.LatestValue(ctx, key); !errors.Is(err, geotemporal.ErrNoValues) {
		t.Errorf("LatestValue: err = %v, want ErrNoValues", err)
	}
	values, err := store.StreamHistoricValues(ctx, key)
	if err != nil {
		t.Errorf("StreamHistoricValues: unexpected err %v", err)
	}
	if len(values) != 0 {
		t.Errorf("StreamHistoricValues: got %d values, want 0", len(values))
	}
}

func TestPgStore_Given_OutOfOrderAppends_When_Read_Then_SortedAscAndLatestCorrect(t *testing.T) {
	store, _ := pgFixture(t)
	ctx := context.Background()
	key := geotemporal.SeriesKey{Ontology: "ri.ontology.main.ontology.demo", ObjectType: "vehicle", PrimaryKey: "v1", Property: "track"}

	t1, _ := time.Parse(time.RFC3339, "2026-04-01T00:00:00Z")
	t2, _ := time.Parse(time.RFC3339, "2026-04-02T00:00:00Z")
	t3, _ := time.Parse(time.RFC3339, "2026-04-03T00:00:00Z")

	for _, v := range []geotemporal.Value{
		{Time: t2, Position: point(-122.42, 37.78)},
		{Time: t1, Position: point(-122.41, 37.77)},
		{Time: t3, Position: point(-122.43, 37.79)},
	} {
		if err := store.AppendValue(ctx, key, v); err != nil {
			t.Fatalf("AppendValue %v: %v", v.Time, err)
		}
	}

	latest, err := store.LatestValue(ctx, key)
	if err != nil {
		t.Fatalf("LatestValue: %v", err)
	}
	if !latest.Time.Equal(t3) {
		t.Errorf("latest.Time = %v, want %v", latest.Time, t3)
	}

	values, err := store.StreamHistoricValues(ctx, key)
	if err != nil {
		t.Fatalf("StreamHistoricValues: %v", err)
	}
	if len(values) != 3 {
		t.Fatalf("got %d values, want 3", len(values))
	}
	if !values[0].Time.Equal(t1) || !values[1].Time.Equal(t2) || !values[2].Time.Equal(t3) {
		t.Errorf("values not sorted asc: %+v", values)
	}
}

func TestPgStore_Given_AppendsThenReopen_When_Read_Then_DataSurvivesRestart(t *testing.T) {
	store, reopen := pgFixture(t)
	ctx := context.Background()
	key := geotemporal.SeriesKey{Ontology: "ri.ontology.main.ontology.demo", ObjectType: "vehicle", PrimaryKey: "v1", Property: "track"}

	base, _ := time.Parse(time.RFC3339, "2026-04-01T00:00:00Z")
	for i := 0; i < 5; i++ {
		v := geotemporal.Value{Time: base.Add(time.Duration(i) * time.Hour), Position: point(-122.0-float64(i)*0.01, 37.0+float64(i)*0.01)}
		if err := store.AppendValue(ctx, key, v); err != nil {
			t.Fatalf("AppendValue: %v", err)
		}
	}

	// Drop the original handle and rebuild — same DB state, fresh struct.
	store = nil
	reborn := reopen()

	got, err := reborn.StreamHistoricValues(ctx, key)
	if err != nil {
		t.Fatalf("StreamHistoricValues after reopen: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("after reopen: got %d points, want 5", len(got))
	}
	for i, v := range got {
		if !v.Time.Equal(base.Add(time.Duration(i) * time.Hour)) {
			t.Errorf("point %d time = %v, want %v", i, v.Time, base.Add(time.Duration(i)*time.Hour))
		}
		pos, ok := v.Position.(map[string]interface{})
		if !ok {
			t.Fatalf("point %d position is %T, want map", i, v.Position)
		}
		if pos["type"] != "Point" {
			t.Errorf("point %d position.type = %v, want \"Point\"", i, pos["type"])
		}
	}

	latest, err := reborn.LatestValue(ctx, key)
	if err != nil {
		t.Fatalf("LatestValue after reopen: %v", err)
	}
	if !latest.Time.Equal(base.Add(4 * time.Hour)) {
		t.Errorf("after reopen LatestValue.Time = %v, want %v", latest.Time, base.Add(4*time.Hour))
	}
}

func TestPgStore_Given_MultipleSeries_When_Read_Then_KeysIsolated(t *testing.T) {
	store, _ := pgFixture(t)
	ctx := context.Background()

	keyA := geotemporal.SeriesKey{Ontology: "ri.ontology.main.ontology.demo", ObjectType: "vehicle", PrimaryKey: "vA", Property: "track"}
	keyB := keyA
	keyB.PrimaryKey = "vB"

	t1, _ := time.Parse(time.RFC3339, "2026-04-01T00:00:00Z")
	if err := store.AppendValue(ctx, keyA, geotemporal.Value{Time: t1, Position: point(-122, 37)}); err != nil {
		t.Fatalf("AppendValue keyA: %v", err)
	}

	if _, err := store.LatestValue(ctx, keyB); !errors.Is(err, geotemporal.ErrNoValues) {
		t.Errorf("keyB should be empty, got err=%v", err)
	}
	valsA, err := store.StreamHistoricValues(ctx, keyA)
	if err != nil {
		t.Fatalf("StreamHistoricValues keyA: %v", err)
	}
	if len(valsA) != 1 {
		t.Errorf("keyA values = %d, want 1", len(valsA))
	}
}
