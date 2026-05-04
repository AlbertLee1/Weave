package notifications

import (
	"context"
	"sort"
	"sync"
	"time"
)

// MemoryPreferenceStore is the in-process PreferenceStore used by unit
// tests and degraded-mode boots without PG. The PG-backed implementation
// lives in cmd/server (pg_preference_store.go) so pkg/notifications
// stays free of pgxpool imports.
type MemoryPreferenceStore struct {
	mu    sync.RWMutex
	rows  map[string]map[string]Preference
	clock func() time.Time
}

// NewMemoryPreferenceStore returns an empty in-memory store.
func NewMemoryPreferenceStore() *MemoryPreferenceStore {
	return &MemoryPreferenceStore{
		rows:  make(map[string]map[string]Preference),
		clock: time.Now,
	}
}

// ListByUser returns every preference row for the given user, sorted
// by channel for stable output. An unknown user yields an empty slice.
func (s *MemoryPreferenceStore) ListByUser(_ context.Context, userID string) ([]Preference, error) {
	if s == nil || userID == "" {
		return nil, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	byChan, ok := s.rows[userID]
	if !ok {
		return nil, nil
	}
	out := make([]Preference, 0, len(byChan))
	for _, p := range byChan {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Channel < out[j].Channel })
	return out, nil
}

// Upsert inserts or updates the row for (UserID, Channel). The store
// stamps CreatedAt on first write and refreshes UpdatedAt on every
// write — same pattern as the PG-backed store so callers can rely on
// the timestamps regardless of backend.
func (s *MemoryPreferenceStore) Upsert(_ context.Context, p *Preference) error {
	if s == nil || p == nil || p.UserID == "" || p.Channel == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rows == nil {
		s.rows = make(map[string]map[string]Preference)
	}
	byChan, ok := s.rows[p.UserID]
	if !ok {
		byChan = make(map[string]Preference)
		s.rows[p.UserID] = byChan
	}
	now := s.clock()
	stored := *p
	if existing, ok := byChan[p.Channel]; ok {
		stored.CreatedAt = existing.CreatedAt
	} else {
		stored.CreatedAt = now
	}
	stored.UpdatedAt = now
	byChan[p.Channel] = stored
	return nil
}

// Delete removes the row. Idempotent — deleting a missing row is a
// success.
func (s *MemoryPreferenceStore) Delete(_ context.Context, userID, channel string) error {
	if s == nil || userID == "" || channel == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if byChan, ok := s.rows[userID]; ok {
		delete(byChan, channel)
		if len(byChan) == 0 {
			delete(s.rows, userID)
		}
	}
	return nil
}

var _ PreferenceStore = (*MemoryPreferenceStore)(nil)
