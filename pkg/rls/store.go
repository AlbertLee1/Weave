package rls

import (
	"context"
	"sync"
)

// Store abstracts persistence for RowPolicy rows. Implementations MUST return
// ErrNotFound for lookups that do not match a row; update / delete on
// missing RIDs should return ErrNotFound rather than nil so handlers surface
// 404 instead of silent no-ops. Kept deliberately narrow so degraded-mode
// test routers can satisfy it with an in-memory fake without stubbing the
// full oms.Repository surface.
type Store interface {
	Create(ctx context.Context, p *RowPolicy) error
	Get(ctx context.Context, rid string) (*RowPolicy, error)
	List(ctx context.Context) ([]*RowPolicy, error)
	ListByObjectType(ctx context.Context, objectTypeRID string) ([]*RowPolicy, error)
	Update(ctx context.Context, rid string, upd RowPolicyUpdate) (*RowPolicy, error)
	Delete(ctx context.Context, rid string) error
}

// GroupMembershipLookup resolves the groups a user belongs to. Implementations
// should return an empty slice (not nil) when the user is in no groups. A nil
// GroupMembershipLookup is treated as "no group lookup configured" — Compile
// then skips group-scoped policies silently.
type GroupMembershipLookup interface {
	UserGroups(ctx context.Context, userID string) ([]string, error)
}

// MemoryStore is an in-memory Store implementation used by tests and
// degraded-mode bootstraps where no PG is available. Safe for concurrent use.
type MemoryStore struct {
	mu       sync.RWMutex
	policies map[string]*RowPolicy
}

// NewMemoryStore returns a fresh empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{policies: make(map[string]*RowPolicy)}
}

func (m *MemoryStore) Create(_ context.Context, p *RowPolicy) error {
	if err := p.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	copy := *p
	m.policies[p.RID] = &copy
	return nil
}

func (m *MemoryStore) Get(_ context.Context, rid string) (*RowPolicy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.policies[rid]
	if !ok {
		return nil, ErrNotFound
	}
	out := *p
	return &out, nil
}

func (m *MemoryStore) List(_ context.Context) ([]*RowPolicy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*RowPolicy, 0, len(m.policies))
	for _, p := range m.policies {
		cp := *p
		out = append(out, &cp)
	}
	return out, nil
}

func (m *MemoryStore) ListByObjectType(_ context.Context, objectTypeRID string) ([]*RowPolicy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*RowPolicy, 0)
	for _, p := range m.policies {
		if p.ObjectTypeRID == objectTypeRID {
			cp := *p
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *MemoryStore) Update(_ context.Context, rid string, upd RowPolicyUpdate) (*RowPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.policies[rid]
	if !ok {
		return nil, ErrNotFound
	}
	if upd.Predicate != nil {
		p.Predicate = *upd.Predicate
	}
	if upd.AppliesTo != nil {
		p.AppliesTo = *upd.AppliesTo
	}
	if upd.Description != nil {
		p.Description = *upd.Description
	}
	out := *p
	return &out, nil
}

func (m *MemoryStore) Delete(_ context.Context, rid string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.policies[rid]; !ok {
		return ErrNotFound
	}
	delete(m.policies, rid)
	return nil
}
