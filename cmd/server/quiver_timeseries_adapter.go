package main

import (
	"context"

	"github.com/liyang/weave/pkg/quiver"
	"github.com/liyang/weave/pkg/timeseries"
)

// quiverTimeSeriesAdapter bridges the pkg/timeseries.Store interface to
// the narrow quiver.TimeSeriesReader capability the /dashboards/{rid}/data
// handler needs. Lives in cmd/server so pkg/quiver stays unaware of
// pkg/timeseries — same "narrow capability in pkg, adapter in cmd/server"
// pattern as the funnel resolvers (edit_only_resolver.go,
// link_propagation_resolver.go, etc).
type quiverTimeSeriesAdapter struct {
	store timeseries.Store
}

func newQuiverTimeSeriesAdapter(store timeseries.Store) *quiverTimeSeriesAdapter {
	return &quiverTimeSeriesAdapter{store: store}
}

// StreamPoints translates the quiver-side TimeSeriesKey to the
// timeseries-side SeriesKey and re-shapes the result list. The two
// Point structs are byte-compatible (`time` + `value`), so the loop is
// a 1:1 field rename rather than a JSON round-trip.
func (a *quiverTimeSeriesAdapter) StreamPoints(ctx context.Context, key quiver.TimeSeriesKey) ([]quiver.TimeSeriesPoint, error) {
	pts, err := a.store.StreamPoints(ctx, timeseries.SeriesKey{
		Ontology:   key.Ontology,
		ObjectType: key.ObjectType,
		PrimaryKey: key.PrimaryKey,
		Property:   key.Property,
	})
	if err != nil {
		return nil, err
	}
	out := make([]quiver.TimeSeriesPoint, len(pts))
	for i, p := range pts {
		out[i] = quiver.TimeSeriesPoint{Time: p.Time, Value: p.Value}
	}
	return out, nil
}
