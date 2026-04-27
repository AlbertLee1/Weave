package featureflags

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// ErrFlagNotFound is returned by Store.GetFlag / UpdateFlag / DeleteFlag
// when the named flag does not exist.
var ErrFlagNotFound = errors.New("featureflags: flag not found")

// ErrFlagAlreadyExists is returned by Store.CreateFlag when a flag
// with the same name is already persisted.
var ErrFlagAlreadyExists = errors.New("featureflags: flag already exists")

// Store is the narrow persistence surface feature-flag admin CRUD
// depends on. Kept off oms.Repository intentionally — extending that
// would cascade into ~15 mock stubs across the repo for a leaf feature.
type Store interface {
	CreateFlag(ctx context.Context, flag *Flag) error
	GetFlag(ctx context.Context, name string) (*Flag, error)
	ListFlags(ctx context.Context) ([]*Flag, error)
	UpdateFlag(ctx context.Context, name string, upd FlagUpdate) error
	DeleteFlag(ctx context.Context, name string) error
}

// MemoryStore is the in-memory implementation of Store. Used in tests
// and in degraded-mode deployments without PostgreSQL.
type MemoryStore struct {
	mu    sync.RWMutex
	flags map[string]*Flag
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{flags: map[string]*Flag{}}
}

// CreateFlag inserts flag. Stamps CreatedAt / UpdatedAt when unset.
// Returns ErrFlagAlreadyExists if a flag with the same name is already
// present.
func (s *MemoryStore) CreateFlag(_ context.Context, flag *Flag) error {
	if flag == nil {
		return errors.New("featureflags: flag is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.flags[flag.Name]; ok {
		return ErrFlagAlreadyExists
	}
	now := time.Now().UTC()
	if flag.CreatedAt.IsZero() {
		flag.CreatedAt = now
	}
	if flag.UpdatedAt.IsZero() {
		flag.UpdatedAt = now
	}
	cp := *flag
	cp.Realms = append([]string(nil), flag.Realms...)
	cp.Users = append([]string(nil), flag.Users...)
	s.flags[flag.Name] = &cp
	return nil
}

// GetFlag returns the flag or ErrFlagNotFound.
func (s *MemoryStore) GetFlag(_ context.Context, name string) (*Flag, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, ok := s.flags[name]
	if !ok {
		return nil, ErrFlagNotFound
	}
	cp := *f
	cp.Realms = append([]string(nil), f.Realms...)
	cp.Users = append([]string(nil), f.Users...)
	return &cp, nil
}

// ListFlags returns every flag sorted by name ascending.
func (s *MemoryStore) ListFlags(_ context.Context) ([]*Flag, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Flag, 0, len(s.flags))
	for _, f := range s.flags {
		cp := *f
		cp.Realms = append([]string(nil), f.Realms...)
		cp.Users = append([]string(nil), f.Users...)
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// UpdateFlag applies a partial update. ErrFlagNotFound when missing.
func (s *MemoryStore) UpdateFlag(_ context.Context, name string, upd FlagUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.flags[name]
	if !ok {
		return ErrFlagNotFound
	}
	if upd.Description != nil {
		f.Description = *upd.Description
	}
	if upd.Enabled != nil {
		f.Enabled = *upd.Enabled
	}
	if upd.Realms != nil {
		f.Realms = append([]string(nil), (*upd.Realms)...)
	}
	if upd.Users != nil {
		f.Users = append([]string(nil), (*upd.Users)...)
	}
	f.UpdatedAt = time.Now().UTC()
	return nil
}

// DeleteFlag removes the named flag. ErrFlagNotFound when missing.
func (s *MemoryStore) DeleteFlag(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.flags[name]; !ok {
		return ErrFlagNotFound
	}
	delete(s.flags, name)
	return nil
}
