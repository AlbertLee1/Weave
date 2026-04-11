package timeseries

import (
	"context"
	"sort"
	"sync"
)

// MemoryStore is an in-process Store backed by a map[SeriesKey][]Point.
// It is the default single-machine backend; a PGStore (see pg_store.go)
// is available when a PostgreSQL pool is configured.
type MemoryStore struct {
	mu     sync.RWMutex
	series map[SeriesKey][]Point
}

// NewMemoryStore returns an empty in-memory Store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{series: map[SeriesKey][]Point{}}
}

// AppendPoint inserts p into the series at key, keeping the slice sorted
// by time ascending so First/Last stay O(1).
func (s *MemoryStore) AppendPoint(_ context.Context, key SeriesKey, p Point) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	points := s.series[key]
	points = append(points, p)
	sort.SliceStable(points, func(i, j int) bool {
		return points[i].Time.Before(points[j].Time)
	})
	s.series[key] = points
	return nil
}

// FirstPoint returns the earliest point, or ErrNoPoints.
func (s *MemoryStore) FirstPoint(_ context.Context, key SeriesKey) (*Point, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	points := s.series[key]
	if len(points) == 0 {
		return nil, ErrNoPoints
	}
	p := points[0]
	return &p, nil
}

// LastPoint returns the latest point, or ErrNoPoints.
func (s *MemoryStore) LastPoint(_ context.Context, key SeriesKey) (*Point, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	points := s.series[key]
	if len(points) == 0 {
		return nil, ErrNoPoints
	}
	p := points[len(points)-1]
	return &p, nil
}

// StreamPoints returns a copy of all points for the series, ordered by
// time ascending.
func (s *MemoryStore) StreamPoints(_ context.Context, key SeriesKey) ([]Point, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	points := s.series[key]
	out := make([]Point, len(points))
	copy(out, points)
	return out, nil
}
