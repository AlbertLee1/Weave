package oms

import (
	"context"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// MemoryColumnLineageStore is an in-memory implementation of
// ColumnLineageStore for unit tests + degraded-mode bootstraps. Safe for
// concurrent use.
type MemoryColumnLineageStore struct {
	mu     sync.RWMutex
	nextID int64
	edges  map[int64]ColumnLineageEdge // id → edge
	clock  func() time.Time
}

// NewMemoryColumnLineageStore returns a freshly-zeroed in-memory store.
// The store stamps timestamps via time.Now by default; tests that need
// deterministic ordering can swap the clock via WithClock.
func NewMemoryColumnLineageStore() *MemoryColumnLineageStore {
	return &MemoryColumnLineageStore{
		edges: make(map[int64]ColumnLineageEdge),
	}
}

// WithClock swaps the timestamp source. Returns the receiver for
// chaining inside test setup blocks.
func (s *MemoryColumnLineageStore) WithClock(clock func() time.Time) *MemoryColumnLineageStore {
	s.clock = clock
	return s
}

func (s *MemoryColumnLineageStore) now() time.Time {
	if s.clock != nil {
		return s.clock()
	}
	return time.Now().UTC()
}

// ReplaceColumnLineageForBinding clears every edge owned by bindingRID
// and re-inserts the supplied set. Each inserted edge's ID + Timestamp
// are back-filled.
func (s *MemoryColumnLineageStore) ReplaceColumnLineageForBinding(_ context.Context, bindingRID string, edges []ColumnLineageEdge) error {
	if bindingRID == "" {
		return errors.New("oms: bindingRID is required for ReplaceColumnLineageForBinding")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, e := range s.edges {
		if e.BindingRID == bindingRID {
			delete(s.edges, id)
		}
	}
	for i := range edges {
		e := &edges[i]
		e.BindingRID = bindingRID
		e.ID = atomic.AddInt64(&s.nextID, 1)
		if e.Timestamp.IsZero() {
			e.Timestamp = s.now()
		}
		s.edges[e.ID] = *e
	}
	return nil
}

// DeleteColumnLineageForBinding removes every edge owned by bindingRID.
func (s *MemoryColumnLineageStore) DeleteColumnLineageForBinding(_ context.Context, bindingRID string) (int64, error) {
	if bindingRID == "" {
		return 0, errors.New("oms: bindingRID is required for DeleteColumnLineageForBinding")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for id, e := range s.edges {
		if e.BindingRID == bindingRID {
			delete(s.edges, id)
			n++
		}
	}
	return n, nil
}

// ListUpstreamColumnLineageForProperty returns every edge whose
// dst_property_rid matches, newest-first.
func (s *MemoryColumnLineageStore) ListUpstreamColumnLineageForProperty(_ context.Context, propertyRID string, limit int) ([]ColumnLineageEdge, error) {
	if limit <= 0 {
		limit = defaultColumnLineageListLimit
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []ColumnLineageEdge
	for _, e := range s.edges {
		if e.DstPropertyRID == propertyRID {
			out = append(out, e)
		}
	}
	sortColumnLineageDesc(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ListDownstreamColumnLineageForDatasetColumn returns every edge whose
// upstream (dataset, column) matches.
func (s *MemoryColumnLineageStore) ListDownstreamColumnLineageForDatasetColumn(_ context.Context, datasetRID, column string, limit int) ([]ColumnLineageEdge, error) {
	if limit <= 0 {
		limit = defaultColumnLineageListLimit
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []ColumnLineageEdge
	for _, e := range s.edges {
		if e.SrcDatasetRID == datasetRID && e.SrcColumn == column {
			out = append(out, e)
		}
	}
	sortColumnLineageDesc(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func sortColumnLineageDesc(edges []ColumnLineageEdge) {
	sort.Slice(edges, func(i, j int) bool {
		if !edges[i].Timestamp.Equal(edges[j].Timestamp) {
			return edges[i].Timestamp.After(edges[j].Timestamp)
		}
		return edges[i].ID > edges[j].ID
	})
}
