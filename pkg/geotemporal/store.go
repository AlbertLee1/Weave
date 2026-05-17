// Package geotemporal implements the storage layer backing Foundry OSv2
// GeotemporalSeriesProperty endpoints (latestValue / streamHistoricValues).
//
// A Store persists ordered (time, position) values keyed by a SeriesKey
// (ontology + objectType + primaryKey + property). Position is stored
// opaquely as an interface{} so the backend is shape-agnostic; on the wire
// Foundry serialises positions as GeoJSON Point objects.
//
// Only an in-memory implementation ships today; the PostGIS / JSONB backend
// is intentionally deferred per the Phase 4 scope note in the PRD (open
// question #3).
package geotemporal

import (
	"context"
	"errors"
	"time"
)

// Value is a single (time, position) reading on a geotemporal series.
//
// Time is serialised as RFC3339 (ISO 8601). Position is whatever the caller
// stored — Foundry emits GeoJSON Point on the wire, but the Go type is
// interface{} to keep the store shape-agnostic.
type Value struct {
	Time     time.Time   `json:"time"`
	Position interface{} `json:"position"`
}

// SeriesKey uniquely identifies one geotemporal series. It mirrors the path
// parameters on the Foundry GeotemporalSeriesProperty endpoints.
type SeriesKey struct {
	Ontology   string
	ObjectType string
	PrimaryKey string
	Property   string
}

// Store persists and retrieves geotemporal series values. Implementations
// MUST be safe for concurrent use.
type Store interface {
	// LatestValue returns the most recent value for the series, or
	// ErrNoValues when the series is empty / unknown.
	LatestValue(ctx context.Context, key SeriesKey) (*Value, error)
	// StreamHistoricValues returns all values for the series, ordered by
	// time ascending. An empty/unknown series returns an empty slice and no
	// error — callers distinguish "no data" via the slice length.
	StreamHistoricValues(ctx context.Context, key SeriesKey) ([]Value, error)
	// AppendValue appends a single value to the series. Values may arrive
	// out of order; implementations MUST keep storage sorted so LatestValue
	// stays correct.
	AppendValue(ctx context.Context, key SeriesKey, v Value) error
}

// ErrNoValues is returned by LatestValue when the addressed series has no
// data. Handlers translate this into a 404.
var ErrNoValues = errors.New("geotemporal: no values")

// BBox is an axis-aligned geographic bounding box. The longitude span runs
// from MinLng (west) to MaxLng (east) and the latitude span from MinLat
// (south) to MaxLat (north). Edges are inclusive — a point sitting exactly
// on a boundary is treated as inside the box.
//
// BBox is in WGS-84 / EPSG:4326 degrees (the same coordinate system used
// in the GeoJSON Point payloads stored in the series). The struct does NOT
// model antimeridian wraparound; callers that need it must split into two
// boxes themselves.
type BBox struct {
	MinLng float64
	MinLat float64
	MaxLng float64
	MaxLat float64
}

// Contains reports whether a (lng, lat) point lies inside the bbox,
// inclusive of every edge.
func (b BBox) Contains(lng, lat float64) bool {
	return lng >= b.MinLng && lng <= b.MaxLng && lat >= b.MinLat && lat <= b.MaxLat
}

// TimeRange is a closed time interval [Start, End]. Either bound may be the
// zero time, which makes that half-line unbounded: a zero Start means
// "everything up to End", a zero End means "everything from Start onward",
// and both zero means "no time filter at all".
type TimeRange struct {
	Start time.Time
	End   time.Time
}

// Contains reports whether ts falls inside the range, applying the
// "zero == unbounded" rule on each side.
func (r TimeRange) Contains(ts time.Time) bool {
	if !r.Start.IsZero() && ts.Before(r.Start) {
		return false
	}
	if !r.End.IsZero() && ts.After(r.End) {
		return false
	}
	return true
}

// SpatialTemporalQuerier is an optional Store capability that returns the
// subset of a series filtered by a bbox AND a time range simultaneously.
// Implementations MUST return values sorted by Time ascending and MUST
// return an empty (non-nil) slice — not ErrNoValues — when nothing
// matches, mirroring StreamHistoricValues' "empty == empty slice" rule.
//
// Position values that are not GeoJSON Point payloads (i.e. the shape
// {"type": "Point", "coordinates": [lng, lat]}) are silently skipped:
// the spatial filter has no meaning for them and surfacing an error would
// poison otherwise-valid results.
type SpatialTemporalQuerier interface {
	QueryBBoxRange(ctx context.Context, key SeriesKey, bbox BBox, tr TimeRange) ([]Value, error)
}
