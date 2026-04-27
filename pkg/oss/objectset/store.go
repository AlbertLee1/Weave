package objectset

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Store provides temporary storage for ObjectSet definitions with TTL.
type Store struct {
	mu      sync.RWMutex
	entries map[string]*storeEntry
	ttl     time.Duration
	stopCh  chan struct{}
	stopped sync.Once
}

type storeEntry struct {
	def       *Definition
	createdAt time.Time
}

// NewStore creates a new ObjectSet store with the given TTL.
// No background cleanup is started; call Cleanup() manually or use NewStoreWithCleanup.
func NewStore(ttl time.Duration) *Store {
	return &Store{
		entries: make(map[string]*storeEntry),
		ttl:     ttl,
	}
}

// NewStoreWithCleanup creates a new ObjectSet store that runs a background goroutine
// to clean up expired entries at the given interval. Call Stop() to shut down the goroutine.
func NewStoreWithCleanup(ttl, cleanupInterval time.Duration) *Store {
	s := &Store{
		entries: make(map[string]*storeEntry),
		ttl:     ttl,
		stopCh:  make(chan struct{}),
	}
	go s.cleanupLoop(cleanupInterval)
	return s
}

// cleanupLoop periodically calls Cleanup until the store is stopped.
func (s *Store) cleanupLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.Cleanup()
		case <-s.stopCh:
			return
		}
	}
}

// Stop shuts down the background cleanup goroutine. Safe to call multiple times.
func (s *Store) Stop() {
	s.stopped.Do(func() {
		if s.stopCh != nil {
			close(s.stopCh)
		}
	})
}

// Put stores an ObjectSet definition and returns its reference ID.
func (s *Store) Put(def *Definition) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := uuid.New().String()
	s.entries[id] = &storeEntry{
		def:       def,
		createdAt: time.Now(),
	}
	return id
}

// Get retrieves a stored ObjectSet definition by its reference ID.
func (s *Store) Get(id string) (*Definition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.entries[id]
	if !ok {
		return nil, fmt.Errorf("objectSet %q not found", id)
	}

	if time.Since(entry.createdAt) > s.ttl {
		return nil, fmt.Errorf("objectSet %q has expired", id)
	}

	return entry.def, nil
}

// Cleanup removes expired entries.
func (s *Store) Cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for id, entry := range s.entries {
		if now.Sub(entry.createdAt) > s.ttl {
			delete(s.entries, id)
		}
	}
}

// Count returns the number of stored entries.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

// EntrySnapshot is a stable read-only view of a stored ObjectSet, suitable
// for callers that want to enumerate the live store without coupling to its
// internal layout. Definition is the same pointer the original Put received.
type EntrySnapshot struct {
	ID         string
	Definition *Definition
	CreatedAt  time.Time
}

// ListEntries returns a snapshot of every non-expired entry. The returned
// slice is freshly allocated and safe to mutate; the *Definition pointers
// reference the same Definitions held in the store, so callers that mutate
// them will affect future Get results — treat the values as read-only.
func (s *Store) ListEntries() []EntrySnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	out := make([]EntrySnapshot, 0, len(s.entries))
	for id, entry := range s.entries {
		if now.Sub(entry.createdAt) > s.ttl {
			continue
		}
		out = append(out, EntrySnapshot{ID: id, Definition: entry.def, CreatedAt: entry.createdAt})
	}
	return out
}

// GetEntry is like Get but also returns the entry's creation timestamp. It
// returns the same expiry / not-found errors Get does.
func (s *Store) GetEntry(id string) (*EntrySnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.entries[id]
	if !ok {
		return nil, fmt.Errorf("objectSet %q not found", id)
	}
	if time.Since(entry.createdAt) > s.ttl {
		return nil, fmt.Errorf("objectSet %q has expired", id)
	}
	return &EntrySnapshot{ID: id, Definition: entry.def, CreatedAt: entry.createdAt}, nil
}
