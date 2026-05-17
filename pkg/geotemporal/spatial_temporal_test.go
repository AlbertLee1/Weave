package geotemporal_test

import (
	"context"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/geotemporal"
)

// US-466: MemoryStore must satisfy SpatialTemporalQuerier so the in-process
// backend has parity with PgStore.QueryBBoxRange. These tests exercise the
// same predicate semantics the integration tests pin down on the PG side:
// the (bbox, time-range) double filter is inclusive on every edge, results
// are ordered by ts ASC, and the zero-time on either TimeRange bound means
// "unbounded on this side".

func us466Key() geotemporal.SeriesKey {
	return geotemporal.SeriesKey{
		Ontology:   "ri.ontology.main.ontology.demo",
		ObjectType: "vehicle",
		PrimaryKey: "v1",
		Property:   "track",
	}
}

func us466Point(lng, lat float64) map[string]interface{} {
	return map[string]interface{}{
		"type":        "Point",
		"coordinates": []interface{}{lng, lat},
	}
}

func TestMemoryStore_Given_MixedPoints_When_QueryBBoxRange_Then_OnlyBBoxAndTimeMatchesReturned(t *testing.T) {
	store := geotemporal.NewMemoryStore()
	ctx := context.Background()
	key := us466Key()

	base, _ := time.Parse(time.RFC3339, "2026-04-01T00:00:00Z")
	seeds := []geotemporal.Value{
		// inside bbox + inside time window
		{Time: base.Add(1 * time.Hour), Position: us466Point(-122.40, 37.78)},
		{Time: base.Add(2 * time.Hour), Position: us466Point(-122.39, 37.77)},
		// inside bbox, outside time window
		{Time: base.Add(10 * 24 * time.Hour), Position: us466Point(-122.41, 37.79)},
		// outside bbox, inside time window
		{Time: base.Add(3 * time.Hour), Position: us466Point(-100.00, 40.00)},
		// outside both
		{Time: base.Add(20 * 24 * time.Hour), Position: us466Point(0.0, 0.0)},
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

func TestMemoryStore_Given_PointOnBBoxEdge_When_QueryBBoxRange_Then_IncludedInclusively(t *testing.T) {
	store := geotemporal.NewMemoryStore()
	ctx := context.Background()
	key := us466Key()

	t1, _ := time.Parse(time.RFC3339, "2026-04-01T00:00:00Z")
	t2 := t1.Add(time.Hour)
	for _, v := range []geotemporal.Value{
		{Time: t1, Position: us466Point(-122.5, 37.5)}, // SW corner
		{Time: t2, Position: us466Point(-122.0, 38.0)}, // NE corner
	} {
		if err := store.AppendValue(ctx, key, v); err != nil {
			t.Fatalf("AppendValue: %v", err)
		}
	}

	bbox := geotemporal.BBox{MinLng: -122.5, MinLat: 37.5, MaxLng: -122.0, MaxLat: 38.0}
	tr := geotemporal.TimeRange{Start: t1, End: t2}
	got, err := store.QueryBBoxRange(ctx, key, bbox, tr)
	if err != nil {
		t.Fatalf("QueryBBoxRange: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("edge points dropped: got %d values, want 2", len(got))
	}
}

func TestMemoryStore_Given_ZeroTimeBounds_When_QueryBBoxRange_Then_UnboundedOnThatSide(t *testing.T) {
	store := geotemporal.NewMemoryStore()
	ctx := context.Background()
	key := us466Key()

	t1, _ := time.Parse(time.RFC3339, "2020-01-01T00:00:00Z")
	t2, _ := time.Parse(time.RFC3339, "2030-01-01T00:00:00Z")
	for _, v := range []geotemporal.Value{
		{Time: t1, Position: us466Point(-122.4, 37.7)},
		{Time: t2, Position: us466Point(-122.4, 37.7)},
	} {
		if err := store.AppendValue(ctx, key, v); err != nil {
			t.Fatalf("AppendValue: %v", err)
		}
	}

	bbox := geotemporal.BBox{MinLng: -180, MinLat: -90, MaxLng: 180, MaxLat: 90}

	// Both bounds zero -> entire series.
	got, err := store.QueryBBoxRange(ctx, key, bbox, geotemporal.TimeRange{})
	if err != nil {
		t.Fatalf("QueryBBoxRange unbounded: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("unbounded TimeRange: got %d, want 2", len(got))
	}

	// Only Start set -> [Start, +inf).
	got, err = store.QueryBBoxRange(ctx, key, bbox, geotemporal.TimeRange{Start: t1.Add(time.Hour)})
	if err != nil {
		t.Fatalf("QueryBBoxRange Start-only: %v", err)
	}
	if len(got) != 1 || !got[0].Time.Equal(t2) {
		t.Errorf("Start-only TimeRange: got %+v, want [%v]", got, t2)
	}

	// Only End set -> (-inf, End].
	got, err = store.QueryBBoxRange(ctx, key, bbox, geotemporal.TimeRange{End: t1.Add(time.Hour)})
	if err != nil {
		t.Fatalf("QueryBBoxRange End-only: %v", err)
	}
	if len(got) != 1 || !got[0].Time.Equal(t1) {
		t.Errorf("End-only TimeRange: got %+v, want [%v]", got, t1)
	}
}

func TestMemoryStore_Given_UnknownSeries_When_QueryBBoxRange_Then_EmptyNoError(t *testing.T) {
	store := geotemporal.NewMemoryStore()
	ctx := context.Background()
	got, err := store.QueryBBoxRange(ctx, us466Key(),
		geotemporal.BBox{MinLng: -180, MinLat: -90, MaxLng: 180, MaxLat: 90},
		geotemporal.TimeRange{})
	if err != nil {
		t.Fatalf("QueryBBoxRange unknown series: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("unknown series returned %d points, want 0", len(got))
	}
}

func TestMemoryStore_Satisfies_SpatialTemporalQuerier(t *testing.T) {
	var _ geotemporal.SpatialTemporalQuerier = geotemporal.NewMemoryStore()
}
