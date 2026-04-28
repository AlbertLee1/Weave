package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"
)

// ErrNotFound is returned when a Get / Update / Delete targets a
// dashboard id that no longer exists or that the caller is not allowed
// to access. The handler maps this to 404 DashboardNotFound — callers
// never learn whether the id existed under another owner.
var ErrNotFound = errors.New("dashboards: not found")

// ErrNameConflict is returned when a Create / Update would result in
// two rows with the same (createdBy, name) tuple. Maps to 409
// DashboardNameConflict at the handler.
var ErrNameConflict = errors.New("dashboards: name conflict")

// Store is the narrow persistence surface. Kept off oms.Repository so
// adding dashboards doesn't cascade into the codebase's many in-memory
// repo stubs (same dep-direction trick as savedsearches.Store).
//
// Get is the only method that can succeed cross-user — when IsPublic is
// true, any authenticated caller may read the row; mutating methods
// (Update / Delete) and List remain owner-scoped.
type Store interface {
	Create(ctx context.Context, d *Dashboard) error
	Get(ctx context.Context, id, createdBy string) (*Dashboard, error)
	List(ctx context.Context, createdBy string) ([]*Dashboard, error)
	Update(ctx context.Context, id, createdBy string, upd Update) error
	Delete(ctx context.Context, id, createdBy string) error
}

// MemoryStore is the in-memory Store impl used in tests and degraded
// (no-PG) deployments. Safe for concurrent use.
type MemoryStore struct {
	mu      sync.RWMutex
	rows    map[string]*Dashboard // keyed by id
	nameIdx map[string]string     // keyed by createdBy + "\x00" + name → id
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		rows:    map[string]*Dashboard{},
		nameIdx: map[string]string{},
	}
}

func nameKey(createdBy, name string) string { return createdBy + "\x00" + name }

func clone(d *Dashboard) *Dashboard {
	cp := *d
	if d.Definition != nil {
		cp.Definition = append(json.RawMessage(nil), d.Definition...)
	}
	return &cp
}

// Create inserts d. Name uniqueness is per-owner. Stamps timestamps
// when zero. Returns ErrNameConflict on (createdBy, name) collision.
func (m *MemoryStore) Create(_ context.Context, d *Dashboard) error {
	if d == nil {
		return errors.New("dashboards: nil dashboard")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := nameKey(d.CreatedBy, d.Name)
	if _, exists := m.nameIdx[key]; exists {
		return ErrNameConflict
	}
	if _, exists := m.rows[d.ID]; exists {
		return errors.New("dashboards: id already exists")
	}
	now := time.Now().UTC()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	if d.UpdatedAt.IsZero() {
		d.UpdatedAt = now
	}
	if len(d.Definition) == 0 {
		d.Definition = json.RawMessage("{}")
	}
	m.rows[d.ID] = clone(d)
	m.nameIdx[key] = d.ID
	return nil
}

// Get returns the row when (a) the caller owns it OR (b) the row is
// public. Returns ErrNotFound for any other lookup.
func (m *MemoryStore) Get(_ context.Context, id, createdBy string) (*Dashboard, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	row, ok := m.rows[id]
	if !ok {
		return nil, ErrNotFound
	}
	if row.CreatedBy != createdBy && !row.IsPublic {
		return nil, ErrNotFound
	}
	return clone(row), nil
}

// List returns every dashboard owned by createdBy, sorted by Name
// ascending. Empty slice (not nil) when the caller owns nothing.
func (m *MemoryStore) List(_ context.Context, createdBy string) ([]*Dashboard, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Dashboard, 0, len(m.rows))
	for _, row := range m.rows {
		if row.CreatedBy != createdBy {
			continue
		}
		out = append(out, clone(row))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Update applies a partial update. Only the owner may mutate. Renaming
// respects per-owner uniqueness. ErrNotFound when missing or not owned;
// ErrNameConflict when the new name collides.
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
	if upd.IsPublic != nil {
		row.IsPublic = *upd.IsPublic
	}
	row.UpdatedAt = time.Now().UTC()
	return nil
}

// Delete removes a dashboard. ErrNotFound when missing / not owned.
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
