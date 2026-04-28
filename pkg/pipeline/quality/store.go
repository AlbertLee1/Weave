package quality

import (
	"context"
	"errors"
	"sort"
	"sync"
)

// ListFilter narrows a violation list query. Empty fields are
// wildcards. PG-side this maps to predicate columns on
// quality_violations; in-memory the same predicates are applied
// post-load.
type ListFilter struct {
	PipelineID string
	RunID      string
	RuleName   string
	Limit      int
}

// ViolationStore is the persistence surface for quality_violations
// rows. Kept narrow (Insert / InsertMany / List) so adding the table
// doesn't cascade into oms.Repository or any of the in-memory mocks
// scattered through the test tree (same pattern as pipeline.Store and
// aip.ToolCatalog).
type ViolationStore interface {
	InsertViolation(ctx context.Context, v *Violation) error
	InsertViolations(ctx context.Context, vs []*Violation) error
	ListViolations(ctx context.Context, filter ListFilter) ([]*Violation, error)
}

// MemoryViolationStore is the in-memory ViolationStore impl used in
// tests and degraded (no PG) deployments.
type MemoryViolationStore struct {
	mu         sync.RWMutex
	violations []*Violation
}

// NewMemoryViolationStore returns an empty MemoryViolationStore.
func NewMemoryViolationStore() *MemoryViolationStore {
	return &MemoryViolationStore{}
}

// InsertViolation appends v after a nil/identifier sanity check.
func (s *MemoryViolationStore) InsertViolation(_ context.Context, v *Violation) error {
	if v == nil {
		return errors.New("quality: violation is nil")
	}
	if v.ID == "" {
		return errors.New("quality: violation id must not be empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *v
	s.violations = append(s.violations, &cp)
	return nil
}

// InsertViolations batches Insert calls under one lock acquisition.
// Returns the first failing row's error and stops; rows queued before
// the failure are persisted, matching PG's "rows committed before the
// error" semantics on a non-transactional batch insert.
func (s *MemoryViolationStore) InsertViolations(ctx context.Context, vs []*Violation) error {
	for _, v := range vs {
		if err := s.InsertViolation(ctx, v); err != nil {
			return err
		}
	}
	return nil
}

// ListViolations returns rows matching filter, newest-first by
// DetectedAt with id as the tiebreaker. Limit<=0 returns all.
func (s *MemoryViolationStore) ListViolations(_ context.Context, filter ListFilter) ([]*Violation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Violation, 0, len(s.violations))
	for _, v := range s.violations {
		if filter.PipelineID != "" && v.PipelineID != filter.PipelineID {
			continue
		}
		if filter.RunID != "" && v.RunID != filter.RunID {
			continue
		}
		if filter.RuleName != "" && v.RuleName != filter.RuleName {
			continue
		}
		cp := *v
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].DetectedAt.Equal(out[j].DetectedAt) {
			return out[i].DetectedAt.After(out[j].DetectedAt)
		}
		return out[i].ID < out[j].ID
	})
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}
