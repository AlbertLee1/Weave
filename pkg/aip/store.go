package aip

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// ErrThreadNotFound is returned by Store methods when the requested
// thread does not exist.
var ErrThreadNotFound = errors.New("aip: thread not found")

// ErrThreadAlreadyExists is returned by Store.CreateThread when the id
// is already taken.
var ErrThreadAlreadyExists = errors.New("aip: thread already exists")

// Store is the narrow persistence surface the AIP handlers depend on.
// Kept off oms.Repository intentionally so adding AIP doesn't cascade
// into the ~15 in-memory repo stubs scattered across the codebase
// (see featureflags.Store / tenants.Store for prior art).
type Store interface {
	CreateThread(ctx context.Context, t *Thread) error
	GetThread(ctx context.Context, id string) (*Thread, error)
	ListThreads(ctx context.Context, createdBy string) ([]*Thread, error)
	UpdateThread(ctx context.Context, id string, upd ThreadUpdate) error
	DeleteThread(ctx context.Context, id string) error

	AppendMessage(ctx context.Context, m *Message) error
	ListMessages(ctx context.Context, threadID string) ([]*Message, error)
}

// MemoryStore is the in-memory Store impl used in tests and degraded
// (no PG) deployments. Safe for concurrent use.
type MemoryStore struct {
	mu       sync.RWMutex
	threads  map[string]*Thread
	messages map[string][]*Message
	nextID   int64
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		threads:  map[string]*Thread{},
		messages: map[string][]*Message{},
	}
}

// CreateThread inserts t. Stamps CreatedAt / UpdatedAt when zero.
func (s *MemoryStore) CreateThread(_ context.Context, t *Thread) error {
	if t == nil {
		return errors.New("aip: thread is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.threads[t.ID]; ok {
		return ErrThreadAlreadyExists
	}
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = now
	}
	cp := *t
	s.threads[t.ID] = &cp
	return nil
}

// GetThread returns the named thread or ErrThreadNotFound.
func (s *MemoryStore) GetThread(_ context.Context, id string) (*Thread, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.threads[id]
	if !ok {
		return nil, ErrThreadNotFound
	}
	cp := *t
	return &cp, nil
}

// ListThreads returns all threads owned by createdBy. When createdBy is
// "" every thread is returned (admin / dev callers). Output is sorted
// newest-first by CreatedAt.
func (s *MemoryStore) ListThreads(_ context.Context, createdBy string) ([]*Thread, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Thread, 0, len(s.threads))
	for _, t := range s.threads {
		if createdBy != "" && t.CreatedBy != createdBy {
			continue
		}
		cp := *t
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// UpdateThread applies a partial update; ErrThreadNotFound when missing.
func (s *MemoryStore) UpdateThread(_ context.Context, id string, upd ThreadUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.threads[id]
	if !ok {
		return ErrThreadNotFound
	}
	if upd.Title != nil {
		t.Title = *upd.Title
	}
	if upd.Model != nil {
		t.Model = *upd.Model
	}
	if upd.SystemPrompt != nil {
		t.SystemPrompt = *upd.SystemPrompt
	}
	t.UpdatedAt = time.Now().UTC()
	return nil
}

// DeleteThread removes the thread and its messages. ErrThreadNotFound
// when missing.
func (s *MemoryStore) DeleteThread(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.threads[id]; !ok {
		return ErrThreadNotFound
	}
	delete(s.threads, id)
	delete(s.messages, id)
	return nil
}

// AppendMessage inserts m at the tail of its thread. Allocates a
// monotonic ID and stamps CreatedAt / TokenCount when unset. Returns
// ErrThreadNotFound when the thread does not exist.
func (s *MemoryStore) AppendMessage(_ context.Context, m *Message) error {
	if m == nil {
		return errors.New("aip: message is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.threads[m.ThreadID]
	if !ok {
		return ErrThreadNotFound
	}
	s.nextID++
	m.ID = s.nextID
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	cp := *m
	s.messages[m.ThreadID] = append(s.messages[m.ThreadID], &cp)
	t.UpdatedAt = m.CreatedAt
	return nil
}

// ListMessages returns every message in threadID ordered by ID asc.
// Empty thread returns an empty (non-nil) slice. Returns
// ErrThreadNotFound when the thread does not exist.
func (s *MemoryStore) ListMessages(_ context.Context, threadID string) ([]*Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.threads[threadID]; !ok {
		return nil, ErrThreadNotFound
	}
	src := s.messages[threadID]
	out := make([]*Message, 0, len(src))
	for _, m := range src {
		cp := *m
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
