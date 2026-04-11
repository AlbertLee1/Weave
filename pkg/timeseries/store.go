// Package timeseries implements the storage layer backing Foundry OSv2
// TimeSeriesProperty endpoints (firstPoint / lastPoint / streamPoints).
//
// A Store persists ordered (time, value) points keyed by a SeriesKey
// (ontology + objectType + primaryKey + property). It exposes two
// implementations:
//
//   - MemoryStore: in-process, default single-machine backend.
//   - PGStore: PostgreSQL-backed implementation mirroring the Foundry
//     schema (see migrations/000016_timeseries.up.sql).
package timeseries

import (
	"context"
	"errors"
	"time"
)

// Point is a single (time, value) reading on a time series.
//
// Time is serialized as RFC3339 (ISO 8601) on the wire; Value is whatever
// the caller stored — Foundry supports numeric, string, and struct values
// so the Go type is interface{}.
type Point struct {
	Time  time.Time   `json:"time"`
	Value interface{} `json:"value"`
}

// SeriesKey uniquely identifies one series in the store. It mirrors the
// path parameters on the Foundry TimeSeriesProperty endpoints.
type SeriesKey struct {
	Ontology   string
	ObjectType string
	PrimaryKey string
	Property   string
}

// Store persists and retrieves time series points. Implementations MUST be
// safe for concurrent use.
type Store interface {
	// FirstPoint returns the earliest point for the series, or ErrNoPoints
	// when the series is empty / unknown.
	FirstPoint(ctx context.Context, key SeriesKey) (*Point, error)
	// LastPoint returns the latest point for the series, or ErrNoPoints.
	LastPoint(ctx context.Context, key SeriesKey) (*Point, error)
	// StreamPoints returns all points for the series, ordered by time
	// ascending. An empty/unknown series returns an empty slice and no
	// error — callers distinguish "no data" via the slice length.
	StreamPoints(ctx context.Context, key SeriesKey) ([]Point, error)
	// AppendPoint appends a single point to the series. Points may arrive
	// out of order; implementations MUST keep storage sorted so First/Last
	// stay correct.
	AppendPoint(ctx context.Context, key SeriesKey, p Point) error
}

// ErrNoPoints is returned by FirstPoint / LastPoint when the addressed
// series has no data. Handlers translate this into a 404.
var ErrNoPoints = errors.New("timeseries: no points")
