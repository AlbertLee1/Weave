package quiver

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"
)

// ErrNotFound is returned when a Get / Update / Delete targets a RID
// that no longer exists or that the caller is not allowed to mutate.
// Maps to 404 QuiverDashboardNotFound at the handler.
var ErrNotFound = errors.New("quiver: not found")

// ErrNameConflict is returned when a Create / Update would result in
// two rows with the same (owner, name) tuple. Maps to 409
// QuiverDashboardNameConflict at the handler.
var ErrNameConflict = errors.New("quiver: name conflict")

// Store is the narrow persistence surface. Kept off oms.Repository so
// adding quiver dashboards doesn't cascade into the codebase's many
// in-memory repo stubs (same dep-direction trick as dashboards.Store).
//
// GetByRID is the only method that succeeds cross-user — it backs the
// read-only `/quiver/{rid}/view` share route. List + mutating methods
// stay owner-scoped.
type Store interface {
	Save(ctx context.Context, d *Dashboard) error
	Get(ctx context.Context, rid, owner string) (*Dashboard, error)
	GetByRID(ctx context.Context, rid string) (*Dashboard, error)
	List(ctx context.Context, owner string) ([]*Dashboard, error)
	Update(ctx context.Context, rid, owner string, upd Update) error
	Delete(ctx context.Context, rid, owner string) error
}

// MemoryStore is the in-memory Store impl used in tests and degraded
// (no-PG) deployments. Safe for concurrent use.
type MemoryStore struct {
	mu      sync.RWMutex
	rows    map[string]*Dashboard // keyed by rid
	nameIdx map[string]string     // keyed by owner + "\x00" + name → rid
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		rows:    map[string]*Dashboard{},
		nameIdx: map[string]string{},
	}
}

func nameKey(owner, name string) string { return owner + "\x00" + name }

func clone(d *Dashboard) *Dashboard {
	cp := *d
	if d.Config != nil {
		cp.Config = append(json.RawMessage(nil), d.Config...)
	}
	return &cp
}

// Save inserts d. Name uniqueness is per-owner. Stamps timestamps when
// zero. Returns ErrNameConflict on (owner, name) collision.
func (m *MemoryStore) Save(_ context.Context, d *Dashboard) error {
	if d == nil {
		return errors.New("quiver: nil dashboard")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := nameKey(d.Owner, d.Name)
	if existingRID, exists := m.nameIdx[key]; exists && existingRID != d.RID {
		return ErrNameConflict
	}
	if _, exists := m.rows[d.RID]; exists {
		return errors.New("quiver: rid already exists")
	}
	now := time.Now().UTC()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	if d.UpdatedAt.IsZero() {
		d.UpdatedAt = now
	}
	if len(d.Config) == 0 {
		d.Config = json.RawMessage("{}")
	}
	m.rows[d.RID] = clone(d)
	m.nameIdx[key] = d.RID
	return nil
}

// Get returns the row when the caller owns it. Returns ErrNotFound for
// any other lookup. Use GetByRID for the share-link surface.
func (m *MemoryStore) Get(_ context.Context, rid, owner string) (*Dashboard, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	row, ok := m.rows[rid]
	if !ok || row.Owner != owner {
		return nil, ErrNotFound
	}
	return clone(row), nil
}

// GetByRID returns the row for any authenticated caller. Backs the
// read-only `/quiver/{rid}/view` share route — the RID is the share
// secret.
func (m *MemoryStore) GetByRID(_ context.Context, rid string) (*Dashboard, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	row, ok := m.rows[rid]
	if !ok {
		return nil, ErrNotFound
	}
	return clone(row), nil
}

// List returns every dashboard owned by owner, sorted by Name
// ascending. Empty slice (not nil) when the caller owns nothing.
func (m *MemoryStore) List(_ context.Context, owner string) ([]*Dashboard, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Dashboard, 0, len(m.rows))
	for _, row := range m.rows {
		if row.Owner != owner {
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
func (m *MemoryStore) Update(_ context.Context, rid, owner string, upd Update) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[rid]
	if !ok || row.Owner != owner {
		return ErrNotFound
	}
	if upd.Name != nil && *upd.Name != row.Name {
		newKey := nameKey(owner, *upd.Name)
		if other, exists := m.nameIdx[newKey]; exists && other != rid {
			return ErrNameConflict
		}
		delete(m.nameIdx, nameKey(owner, row.Name))
		row.Name = *upd.Name
		m.nameIdx[newKey] = rid
	}
	if upd.Config != nil {
		def := *upd.Config
		if len(def) == 0 {
			def = json.RawMessage("{}")
		}
		row.Config = append(json.RawMessage(nil), def...)
	}
	row.UpdatedAt = time.Now().UTC()
	return nil
}

// Delete removes a dashboard. ErrNotFound when missing / not owned.
func (m *MemoryStore) Delete(_ context.Context, rid, owner string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[rid]
	if !ok || row.Owner != owner {
		return ErrNotFound
	}
	delete(m.rows, rid)
	delete(m.nameIdx, nameKey(row.Owner, row.Name))
	return nil
}
