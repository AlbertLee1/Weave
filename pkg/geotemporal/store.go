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
