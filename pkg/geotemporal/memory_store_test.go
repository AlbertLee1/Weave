package geotemporal_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/geotemporal"
)

func testKey() geotemporal.SeriesKey {
	return geotemporal.SeriesKey{
		Ontology:   "ri.ontology.main.ontology.demo",
		ObjectType: "vehicle",
		PrimaryKey: "v1",
		Property:   "track",
	}
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	tt, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time: %v", err)
	}
	return tt
}

func geoPoint(lng, lat float64) map[string]interface{} {
	return map[string]interface{}{
		"type":        "Point",
		"coordinates": []interface{}{lng, lat},
	}
}

func TestMemoryStore_EmptySeries(t *testing.T) {
	store := geotemporal.NewMemoryStore()
	ctx := context.Background()

	if _, err := store.LatestValue(ctx, testKey()); !errors.Is(err, geotemporal.ErrNoValues) {
		t.Errorf("LatestValue on empty series: err = %v, want ErrNoValues", err)
	}
	values, err := store.StreamHistoricValues(ctx, testKey())
	if err != nil {
		t.Errorf("StreamHistoricValues on empty series: unexpected err = %v", err)
	}
	if len(values) != 0 {
		t.Errorf("StreamHistoricValues on empty series: got %d values, want 0", len(values))
	}
}

func TestMemoryStore_AppendAndRead(t *testing.T) {
	store := geotemporal.NewMemoryStore()
	ctx := context.Background()
	key := testKey()

	t1 := mustTime(t, "2026-04-01T00:00:00Z")
	t2 := mustTime(t, "2026-04-02T00:00:00Z")
	t3 := mustTime(t, "2026-04-03T00:00:00Z")

	// Append out-of-order to exercise the sort.
	for _, v := range []geotemporal.Value{
		{Time: t2, Position: geoPoint(-122.42, 37.78)},
		{Time: t1, Position: geoPoint(-122.41, 37.77)},
		{Time: t3, Position: geoPoint(-122.43, 37.79)},
	} {
		if err := store.AppendValue(ctx, key, v); err != nil {
			t.Fatalf("AppendValue: %v", err)
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
		t.Errorf("values not sorted ascending: %v", values)
	}
}

func TestMemoryStore_MultipleSeriesIsolated(t *testing.T) {
	store := geotemporal.NewMemoryStore()
	ctx := context.Background()

	keyA := testKey()
	keyB := testKey()
	keyB.PrimaryKey = "v2"

	t1 := mustTime(t, "2026-04-01T00:00:00Z")
	if err := store.AppendValue(ctx, keyA, geotemporal.Value{Time: t1, Position: geoPoint(-122.0, 37.0)}); err != nil {
		t.Fatalf("AppendValue A: %v", err)
	}

	if _, err := store.LatestValue(ctx, keyB); !errors.Is(err, geotemporal.ErrNoValues) {
		t.Errorf("keyB should still be empty, got err=%v", err)
	}
	valuesA, err := store.StreamHistoricValues(ctx, keyA)
	if err != nil {
		t.Fatalf("StreamHistoricValues keyA: %v", err)
	}
	if len(valuesA) != 1 {
		t.Errorf("keyA values = %d, want 1", len(valuesA))
	}
}
