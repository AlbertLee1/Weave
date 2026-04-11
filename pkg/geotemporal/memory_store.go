package geotemporal

import (
	"context"
	"sort"
	"sync"
)

// MemoryStore is an in-process Store backed by a map[SeriesKey][]Value.
// It is the default single-machine backend; persistent backends (PostGIS,
// JSONB) are deferred per the Phase 4 open question in the PRD.
type MemoryStore struct {
	mu     sync.RWMutex
	series map[SeriesKey][]Value
}

// NewMemoryStore returns an empty in-memory Store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{series: map[SeriesKey][]Value{}}
}

// AppendValue inserts v into the series at key, keeping the slice sorted by
// time ascending so LatestValue stays O(1).
func (s *MemoryStore) AppendValue(_ context.Context, key SeriesKey, v Value) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	values := s.series[key]
	values = append(values, v)
	sort.SliceStable(values, func(i, j int) bool {
		return values[i].Time.Before(values[j].Time)
	})
	s.series[key] = values
	return nil
}

// LatestValue returns the most recent value, or ErrNoValues.
func (s *MemoryStore) LatestValue(_ context.Context, key SeriesKey) (*Value, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := s.series[key]
	if len(values) == 0 {
		return nil, ErrNoValues
	}
	v := values[len(values)-1]
	return &v, nil
}

// StreamHistoricValues returns a copy of all values for the series, ordered
// by time ascending.
func (s *MemoryStore) StreamHistoricValues(_ context.Context, key SeriesKey) ([]Value, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := s.series[key]
	out := make([]Value, len(values))
	copy(out, values)
	return out, nil
}
