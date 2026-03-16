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
}

type storeEntry struct {
	def       *Definition
	createdAt time.Time
}

// NewStore creates a new ObjectSet store with the given TTL.
func NewStore(ttl time.Duration) *Store {
	return &Store{
		entries: make(map[string]*storeEntry),
		ttl:     ttl,
	}
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
