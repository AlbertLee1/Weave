package timeseries_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/timeseries"
)

func testKey() timeseries.SeriesKey {
	return timeseries.SeriesKey{
		Ontology:   "ri.ontology.main.ontology.demo",
		ObjectType: "sensor",
		PrimaryKey: "s1",
		Property:   "temperature",
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

func TestMemoryStore_EmptySeries(t *testing.T) {
	store := timeseries.NewMemoryStore()
	ctx := context.Background()

	if _, err := store.FirstPoint(ctx, testKey()); !errors.Is(err, timeseries.ErrNoPoints) {
		t.Errorf("FirstPoint on empty series: err = %v, want ErrNoPoints", err)
	}
	if _, err := store.LastPoint(ctx, testKey()); !errors.Is(err, timeseries.ErrNoPoints) {
		t.Errorf("LastPoint on empty series: err = %v, want ErrNoPoints", err)
	}
	points, err := store.StreamPoints(ctx, testKey())
	if err != nil {
		t.Errorf("StreamPoints on empty series: unexpected err = %v", err)
	}
	if len(points) != 0 {
		t.Errorf("StreamPoints on empty series: got %d points, want 0", len(points))
	}
}

func TestMemoryStore_AppendAndRead(t *testing.T) {
	store := timeseries.NewMemoryStore()
	ctx := context.Background()
	key := testKey()

	t1 := mustTime(t, "2026-04-01T00:00:00Z")
	t2 := mustTime(t, "2026-04-02T00:00:00Z")
	t3 := mustTime(t, "2026-04-03T00:00:00Z")

	// Append out-of-order to exercise the sort.
	for _, p := range []timeseries.Point{
		{Time: t2, Value: 22.5},
		{Time: t1, Value: 21.0},
		{Time: t3, Value: 23.5},
	} {
		if err := store.AppendPoint(ctx, key, p); err != nil {
			t.Fatalf("AppendPoint: %v", err)
		}
	}

	first, err := store.FirstPoint(ctx, key)
	if err != nil {
		t.Fatalf("FirstPoint: %v", err)
	}
	if !first.Time.Equal(t1) {
		t.Errorf("first.Time = %v, want %v", first.Time, t1)
	}
	if first.Value != 21.0 {
		t.Errorf("first.Value = %v, want 21.0", first.Value)
	}

	last, err := store.LastPoint(ctx, key)
	if err != nil {
		t.Fatalf("LastPoint: %v", err)
	}
	if !last.Time.Equal(t3) {
		t.Errorf("last.Time = %v, want %v", last.Time, t3)
	}
	if last.Value != 23.5 {
		t.Errorf("last.Value = %v, want 23.5", last.Value)
	}

	points, err := store.StreamPoints(ctx, key)
	if err != nil {
		t.Fatalf("StreamPoints: %v", err)
	}
	if len(points) != 3 {
		t.Fatalf("got %d points, want 3", len(points))
	}
	if !points[0].Time.Equal(t1) || !points[1].Time.Equal(t2) || !points[2].Time.Equal(t3) {
		t.Errorf("points not sorted ascending: %v", points)
	}
}

func TestMemoryStore_MultipleSeriesIsolated(t *testing.T) {
	store := timeseries.NewMemoryStore()
	ctx := context.Background()

	keyA := testKey()
	keyB := testKey()
	keyB.PrimaryKey = "s2"

	t1 := mustTime(t, "2026-04-01T00:00:00Z")
	if err := store.AppendPoint(ctx, keyA, timeseries.Point{Time: t1, Value: 10.0}); err != nil {
		t.Fatalf("AppendPoint A: %v", err)
	}

	if _, err := store.FirstPoint(ctx, keyB); !errors.Is(err, timeseries.ErrNoPoints) {
		t.Errorf("keyB should still be empty, got err=%v", err)
	}
	pointsA, err := store.StreamPoints(ctx, keyA)
	if err != nil {
		t.Fatalf("StreamPoints keyA: %v", err)
	}
	if len(pointsA) != 1 {
		t.Errorf("keyA points = %d, want 1", len(pointsA))
	}
}
