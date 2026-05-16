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

// QueryBBoxRange returns the subset of the series whose timestamp lies in
// tr AND whose position lies in bbox. Results are sorted by Time ascending.
// Positions that are not GeoJSON Point shaped are skipped (see the
// SpatialTemporalQuerier doc).
func (s *MemoryStore) QueryBBoxRange(_ context.Context, key SeriesKey, bbox BBox, tr TimeRange) ([]Value, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := s.series[key]
	out := make([]Value, 0, len(values))
	for _, v := range values {
		if !tr.Contains(v.Time) {
			continue
		}
		lng, lat, ok := pointCoords(v.Position)
		if !ok {
			continue
		}
		if !bbox.Contains(lng, lat) {
			continue
		}
		out = append(out, v)
	}
	return out, nil
}

// pointCoords extracts (lng, lat) from a GeoJSON Point. It accepts both the
// canonical map[string]interface{} shape that round-trips through JSON and
// the typed []float64 slice that callers may use in-process. Anything else
// returns ok=false so the caller can skip the row.
func pointCoords(pos interface{}) (lng, lat float64, ok bool) {
	m, ok := pos.(map[string]interface{})
	if !ok {
		return 0, 0, false
	}
	coords, ok := m["coordinates"]
	if !ok {
		return 0, 0, false
	}
	switch c := coords.(type) {
	case []interface{}:
		if len(c) < 2 {
			return 0, 0, false
		}
		x, okx := toFloat(c[0])
		y, oky := toFloat(c[1])
		if !okx || !oky {
			return 0, 0, false
		}
		return x, y, true
	case []float64:
		if len(c) < 2 {
			return 0, 0, false
		}
		return c[0], c[1], true
	}
	return 0, 0, false
}

func toFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}
