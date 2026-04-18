package cellsec

import (
	"context"
	"sync"
)

// Store abstracts persistence for CellMask rows. Update / Delete on missing
// RIDs MUST return ErrNotFound so admin handlers surface 404 instead of a
// silent no-op. Kept deliberately narrow so degraded-mode test harnesses
// can satisfy it with an in-memory fake.
type Store interface {
	Create(ctx context.Context, m *CellMask) error
	Get(ctx context.Context, rid string) (*CellMask, error)
	List(ctx context.Context) ([]*CellMask, error)
	ListByObjectType(ctx context.Context, objectTypeRID string) ([]*CellMask, error)
	Update(ctx context.Context, rid string, upd CellMaskUpdate) (*CellMask, error)
	Delete(ctx context.Context, rid string) error
}

// GroupMembershipLookup resolves the groups a user belongs to. A nil lookup
// is treated as "no group resolution" — AppliesTo.Groups silently matches
// nothing. Signature matches masking.GroupMembershipLookup so one
// adapter in cmd/server satisfies both via structural typing.
type GroupMembershipLookup interface {
	UserGroups(ctx context.Context, userID string) ([]string, error)
}

// MemoryStore is the in-memory Store implementation used by tests and
// degraded-mode bootstraps where no PG is available.
type MemoryStore struct {
	mu    sync.RWMutex
	masks map[string]*CellMask
}

// NewMemoryStore returns a fresh empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{masks: make(map[string]*CellMask)}
}

func (s *MemoryStore) Create(_ context.Context, m *CellMask) error {
	if err := m.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *m
	s.masks[m.RID] = &cp
	return nil
}

func (s *MemoryStore) Get(_ context.Context, rid string) (*CellMask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.masks[rid]
	if !ok {
		return nil, ErrNotFound
	}
	out := *m
	return &out, nil
}

func (s *MemoryStore) List(_ context.Context) ([]*CellMask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*CellMask, 0, len(s.masks))
	for _, m := range s.masks {
		cp := *m
		out = append(out, &cp)
	}
	return out, nil
}

func (s *MemoryStore) ListByObjectType(_ context.Context, objectTypeRID string) ([]*CellMask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*CellMask, 0)
	for _, m := range s.masks {
		if m.ObjectTypeRID == objectTypeRID {
			cp := *m
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (s *MemoryStore) Update(_ context.Context, rid string, upd CellMaskUpdate) (*CellMask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.masks[rid]
	if !ok {
		return nil, ErrNotFound
	}
	if upd.MaskRule != nil {
		m.MaskRule = *upd.MaskRule
	}
	if upd.AppliesTo != nil {
		m.AppliesTo = *upd.AppliesTo
	}
	if upd.Description != nil {
		m.Description = *upd.Description
	}
	out := *m
	return &out, nil
}

func (s *MemoryStore) Delete(_ context.Context, rid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.masks[rid]; !ok {
		return ErrNotFound
	}
	delete(s.masks, rid)
	return nil
}
