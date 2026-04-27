package tenants

import (
	"context"
	"sort"
	"sync"
	"time"
)

// Store is the persistence interface for tenant quota rows. The
// production impl lives in cmd/server/tenant_quotas_store.go (PG-backed)
// — keeping pkg/tenants free of pgx imports follows the same dep-
// direction trick pkg/featureflags / pkg/gdpr already use.
type Store interface {
	CreateQuota(ctx context.Context, q *Quota) error
	GetQuota(ctx context.Context, tenant string) (*Quota, error)
	ListQuotas(ctx context.Context) ([]*Quota, error)
	UpdateQuota(ctx context.Context, tenant string, upd QuotaUpdate) error
	DeleteQuota(ctx context.Context, tenant string) error
}

// MemoryStore is the test-friendly in-memory Store. Safe for concurrent
// use; ListQuotas returns rows sorted by tenant for deterministic
// fixtures.
type MemoryStore struct {
	mu    sync.RWMutex
	rows  map[string]*Quota
	clock func() time.Time
}

// NewMemoryStore returns an empty in-memory Store wired to time.Now.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{rows: make(map[string]*Quota), clock: time.Now}
}

func (s *MemoryStore) CreateQuota(_ context.Context, q *Quota) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.rows[q.Tenant]; exists {
		return ErrQuotaAlreadyExists
	}
	now := s.clock().UTC()
	cp := *q
	cp.CreatedAt = now
	cp.UpdatedAt = now
	s.rows[cp.Tenant] = &cp
	*q = cp
	return nil
}

func (s *MemoryStore) GetQuota(_ context.Context, tenant string) (*Quota, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	q, ok := s.rows[tenant]
	if !ok {
		return nil, ErrQuotaNotFound
	}
	cp := *q
	return &cp, nil
}

func (s *MemoryStore) ListQuotas(_ context.Context) ([]*Quota, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Quota, 0, len(s.rows))
	for _, q := range s.rows {
		cp := *q
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tenant < out[j].Tenant })
	return out, nil
}

func (s *MemoryStore) UpdateQuota(_ context.Context, tenant string, upd QuotaUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.rows[tenant]
	if !ok {
		return ErrQuotaNotFound
	}
	if upd.MaxObjects != nil {
		q.MaxObjects = *upd.MaxObjects
	}
	if upd.MaxStorage != nil {
		q.MaxStorage = *upd.MaxStorage
	}
	if upd.MaxQPS != nil {
		q.MaxQPS = *upd.MaxQPS
	}
	if upd.Burst != nil {
		q.Burst = *upd.Burst
	}
	if upd.Description != nil {
		q.Description = *upd.Description
	}
	q.UpdatedAt = s.clock().UTC()
	return nil
}

func (s *MemoryStore) DeleteQuota(_ context.Context, tenant string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rows[tenant]; !ok {
		return ErrQuotaNotFound
	}
	delete(s.rows, tenant)
	return nil
}
