package apps

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"
)

// ErrNotFound is returned when a Get / Update / Delete targets an App
// that no longer exists or that the caller is not allowed to access.
// Maps to 404 AppNotFound at the handler.
var ErrNotFound = errors.New("apps: not found")

// ErrNameConflict is returned when a Create / Update would result in
// two rows with the same (owner_id, name) tuple. Maps to 409
// AppNameConflict at the handler.
var ErrNameConflict = errors.New("apps: name conflict")

// Store is the narrow persistence surface. Kept off oms.Repository so
// adding apps doesn't cascade into the codebase's many in-memory repo
// stubs (same dep-direction trick as dashboards.Store).
//
// Every Update bumps the App's Version and inserts a snapshot into the
// version history; the most-recent snapshot's LayoutJSON / Name match
// the live row's columns. ListVersions returns history newest-first so
// the SPA's version-rollback UI can render without an extra sort pass.
//
// All mutating methods are owner-scoped — non-owners receive
// ErrNotFound regardless of whether the row exists. createdBy is the
// authenticated user who initiated the call (used as the version row's
// audit attribution).
type Store interface {
	Create(ctx context.Context, app *App, createdBy string) error
	Get(ctx context.Context, rid, ownerID string) (*App, error)
	List(ctx context.Context, ownerID string) ([]*App, error)
	Update(ctx context.Context, rid, ownerID string, upd Update, createdBy string) error
	Delete(ctx context.Context, rid, ownerID string) error
	ListVersions(ctx context.Context, rid, ownerID string) ([]*AppVersion, error)
	GetVersion(ctx context.Context, rid string, version int, ownerID string) (*AppVersion, error)
}

// MemoryStore is the in-memory Store impl used in tests and degraded
// (no-PG) deployments. Safe for concurrent use.
type MemoryStore struct {
	mu       sync.RWMutex
	rows     map[string]*App                // keyed by RID
	versions map[string]map[int]*AppVersion // RID → version → snapshot
	nameIdx  map[string]string              // ownerID + "\x00" + name → RID
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		rows:     map[string]*App{},
		versions: map[string]map[int]*AppVersion{},
		nameIdx:  map[string]string{},
	}
}

func nameKey(ownerID, name string) string { return ownerID + "\x00" + name }

func cloneApp(a *App) *App {
	cp := *a
	if a.LayoutJSON != nil {
		cp.LayoutJSON = append(json.RawMessage(nil), a.LayoutJSON...)
	}
	return &cp
}

func cloneVersion(v *AppVersion) *AppVersion {
	cp := *v
	if v.LayoutJSON != nil {
		cp.LayoutJSON = append(json.RawMessage(nil), v.LayoutJSON...)
	}
	return &cp
}

// Create inserts a new App and stamps Version=1, recording an initial
// snapshot in the version history. Validates name + layout up front;
// either failure aborts the write before any state change.
func (m *MemoryStore) Create(_ context.Context, app *App, createdBy string) error {
	if app == nil {
		return errors.New("apps: nil app")
	}
	if err := ValidateName(app.Name); err != nil {
		return err
	}
	if err := ValidateLayout(app.LayoutJSON); err != nil {
		return errors.Join(ErrInvalidLayout, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := nameKey(app.OwnerID, app.Name)
	if _, exists := m.nameIdx[key]; exists {
		return ErrNameConflict
	}
	if _, exists := m.rows[app.RID]; exists {
		return errors.New("apps: rid already exists")
	}
	now := time.Now().UTC()
	if app.CreatedAt.IsZero() {
		app.CreatedAt = now
	}
	app.UpdatedAt = now
	app.Version = 1
	if len(app.LayoutJSON) == 0 {
		app.LayoutJSON = json.RawMessage("{}")
	}
	m.rows[app.RID] = cloneApp(app)
	m.nameIdx[key] = app.RID
	m.versions[app.RID] = map[int]*AppVersion{
		1: {
			AppRID:     app.RID,
			Version:    1,
			Name:       app.Name,
			LayoutJSON: append(json.RawMessage(nil), app.LayoutJSON...),
			CreatedAt:  now,
			CreatedBy:  createdBy,
		},
	}
	return nil
}

// Get returns the App when (and only when) ownerID owns it. Apps are
// owner-private until US-396 introduces publish/share semantics.
func (m *MemoryStore) Get(_ context.Context, rid, ownerID string) (*App, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	row, ok := m.rows[rid]
	if !ok || row.OwnerID != ownerID {
		return nil, ErrNotFound
	}
	return cloneApp(row), nil
}

// List returns every App owned by ownerID, sorted by Name ascending.
// Empty slice (not nil) when the caller owns nothing.
func (m *MemoryStore) List(_ context.Context, ownerID string) ([]*App, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*App, 0, len(m.rows))
	for _, row := range m.rows {
		if row.OwnerID != ownerID {
			continue
		}
		out = append(out, cloneApp(row))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Update applies a partial update, bumps Version, and records a fresh
// snapshot in the version history. Even a no-op update (both DTO
// fields nil) bumps the version — the contract is "every Update call
// is auditable history". Validation runs BEFORE any state change so a
// bad layout cannot leave the live row half-mutated.
func (m *MemoryStore) Update(_ context.Context, rid, ownerID string, upd Update, createdBy string) error {
	if upd.Name != nil {
		if err := ValidateName(*upd.Name); err != nil {
			return err
		}
	}
	if upd.LayoutJSON != nil {
		if err := ValidateLayout(*upd.LayoutJSON); err != nil {
			return errors.Join(ErrInvalidLayout, err)
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[rid]
	if !ok || row.OwnerID != ownerID {
		return ErrNotFound
	}
	if upd.Name != nil && *upd.Name != row.Name {
		newKey := nameKey(ownerID, *upd.Name)
		if other, exists := m.nameIdx[newKey]; exists && other != rid {
			return ErrNameConflict
		}
		delete(m.nameIdx, nameKey(ownerID, row.Name))
		row.Name = *upd.Name
		m.nameIdx[newKey] = rid
	}
	if upd.LayoutJSON != nil {
		row.LayoutJSON = append(json.RawMessage(nil), (*upd.LayoutJSON)...)
	}
	row.Version++
	row.UpdatedAt = time.Now().UTC()
	hist := m.versions[rid]
	if hist == nil {
		hist = map[int]*AppVersion{}
		m.versions[rid] = hist
	}
	hist[row.Version] = &AppVersion{
		AppRID:     rid,
		Version:    row.Version,
		Name:       row.Name,
		LayoutJSON: append(json.RawMessage(nil), row.LayoutJSON...),
		CreatedAt:  row.UpdatedAt,
		CreatedBy:  createdBy,
	}
	return nil
}

// Delete removes an App and cascades its version history. Owner-only.
func (m *MemoryStore) Delete(_ context.Context, rid, ownerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[rid]
	if !ok || row.OwnerID != ownerID {
		return ErrNotFound
	}
	delete(m.rows, rid)
	delete(m.versions, rid)
	delete(m.nameIdx, nameKey(row.OwnerID, row.Name))
	return nil
}

// ListVersions returns every history snapshot for the App, newest
// first. Owner-only.
func (m *MemoryStore) ListVersions(_ context.Context, rid, ownerID string) ([]*AppVersion, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	row, ok := m.rows[rid]
	if !ok || row.OwnerID != ownerID {
		return nil, ErrNotFound
	}
	hist, ok := m.versions[rid]
	if !ok {
		return []*AppVersion{}, nil
	}
	out := make([]*AppVersion, 0, len(hist))
	for _, v := range hist {
		out = append(out, cloneVersion(v))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version > out[j].Version })
	return out, nil
}

// GetVersion returns one specific historical snapshot. Owner-only.
func (m *MemoryStore) GetVersion(_ context.Context, rid string, version int, ownerID string) (*AppVersion, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	row, ok := m.rows[rid]
	if !ok || row.OwnerID != ownerID {
		return nil, ErrNotFound
	}
	hist, ok := m.versions[rid]
	if !ok {
		return nil, ErrNotFound
	}
	v, ok := hist[version]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneVersion(v), nil
}
