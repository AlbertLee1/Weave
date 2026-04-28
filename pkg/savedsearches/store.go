package savedsearches

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"
)

// ErrNotFound is returned when a Get / Update / Delete targets a
// saved-search id that no longer exists or doesn't belong to the
// caller. The handler maps this to 404 SavedSearchNotFound — the
// caller never learns whether the id existed under another user.
var ErrNotFound = errors.New("savedsearches: not found")

// ErrNameConflict is returned when a Create / Update would result in
// two rows with the same (createdBy, name) tuple. Maps to 409
// SavedSearchNameConflict at the handler.
var ErrNameConflict = errors.New("savedsearches: name conflict")

// Store is the narrow persistence surface. Kept off oms.Repository so
// adding saved searches doesn't cascade into the codebase's many
// in-memory repo stubs (same dep-direction trick as featureflags.Store
// and aip.Store).
type Store interface {
	Create(ctx context.Context, s *SavedSearch) error
	Get(ctx context.Context, id, createdBy string) (*SavedSearch, error)
	List(ctx context.Context, createdBy, ontology, objectType string) ([]*SavedSearch, error)
	Update(ctx context.Context, id, createdBy string, upd Update) error
	Delete(ctx context.Context, id, createdBy string) error
}

// MemoryStore is the in-memory Store impl used in tests and degraded
// (no-PG) deployments. Safe for concurrent use.
type MemoryStore struct {
	mu      sync.RWMutex
	rows    map[string]*SavedSearch // keyed by id
	nameIdx map[string]string       // keyed by createdBy + "\x00" + name → id
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		rows:    map[string]*SavedSearch{},
		nameIdx: map[string]string{},
	}
}

func nameKey(createdBy, name string) string { return createdBy + "\x00" + name }

func cloneSaved(s *SavedSearch) *SavedSearch {
	cp := *s
	if s.Definition != nil {
		cp.Definition = append(json.RawMessage(nil), s.Definition...)
	}
	return &cp
}

// Create inserts s. Name uniqueness is per-owner. Stamps timestamps
// when zero. Returns ErrNameConflict on (createdBy, name) collision.
func (m *MemoryStore) Create(_ context.Context, s *SavedSearch) error {
	if s == nil {
		return errors.New("savedsearches: nil saved search")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := nameKey(s.CreatedBy, s.Name)
	if _, exists := m.nameIdx[key]; exists {
		return ErrNameConflict
	}
	if _, exists := m.rows[s.ID]; exists {
		return errors.New("savedsearches: id already exists")
	}
	now := time.Now().UTC()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	if s.UpdatedAt.IsZero() {
		s.UpdatedAt = now
	}
	if len(s.Definition) == 0 {
		s.Definition = json.RawMessage("{}")
	}
	m.rows[s.ID] = cloneSaved(s)
	m.nameIdx[key] = s.ID
	return nil
}

// Get returns the row when both id matches AND the caller owns it.
// Cross-user lookups produce ErrNotFound (no information leak).
func (m *MemoryStore) Get(_ context.Context, id, createdBy string) (*SavedSearch, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	row, ok := m.rows[id]
	if !ok || row.CreatedBy != createdBy {
		return nil, ErrNotFound
	}
	return cloneSaved(row), nil
}

// List returns the caller's rows for the given ontology + objectType,
// sorted by Name ascending. Empty ontology/objectType returns every
// saved search owned by the caller (admin-style usage).
func (m *MemoryStore) List(_ context.Context, createdBy, ontology, objectType string) ([]*SavedSearch, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*SavedSearch, 0, len(m.rows))
	for _, row := range m.rows {
		if row.CreatedBy != createdBy {
			continue
		}
		if ontology != "" && row.Ontology != ontology {
			continue
		}
		if objectType != "" && row.ObjectType != objectType {
			continue
		}
		out = append(out, cloneSaved(row))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Update applies a partial update. Renaming respects per-owner
// uniqueness. ErrNotFound when the row is missing or owned by another
// user; ErrNameConflict when the new name collides.
func (m *MemoryStore) Update(_ context.Context, id, createdBy string, upd Update) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[id]
	if !ok || row.CreatedBy != createdBy {
		return ErrNotFound
	}
	if upd.Name != nil && *upd.Name != row.Name {
		newKey := nameKey(createdBy, *upd.Name)
		if other, exists := m.nameIdx[newKey]; exists && other != id {
			return ErrNameConflict
		}
		delete(m.nameIdx, nameKey(createdBy, row.Name))
		row.Name = *upd.Name
		m.nameIdx[newKey] = id
	}
	if upd.Definition != nil {
		def := *upd.Definition
		if len(def) == 0 {
			def = json.RawMessage("{}")
		}
		row.Definition = append(json.RawMessage(nil), def...)
	}
	row.UpdatedAt = time.Now().UTC()
	return nil
}

// Delete removes a saved search. ErrNotFound when missing / not owned.
func (m *MemoryStore) Delete(_ context.Context, id, createdBy string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[id]
	if !ok || row.CreatedBy != createdBy {
		return ErrNotFound
	}
	delete(m.rows, id)
	delete(m.nameIdx, nameKey(row.CreatedBy, row.Name))
	return nil
}
