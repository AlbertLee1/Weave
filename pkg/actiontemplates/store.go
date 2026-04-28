package actiontemplates

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"
)

// ErrNotFound is returned when a Get / Update / Delete targets a
// template id that no longer exists OR is private and owned by
// another user. The handler maps this to 404 — the caller never
// learns whether the id existed under a different owner.
var ErrNotFound = errors.New("actiontemplates: not found")

// ErrNameConflict is returned when a Create / Update would result in
// two rows with the same (createdBy, actionType, name) tuple. Maps
// to 409 at the handler.
var ErrNameConflict = errors.New("actiontemplates: name conflict")

// Store is the narrow persistence surface. Kept off oms.Repository
// (Pattern #18) so adding action templates doesn't cascade into the
// codebase's many in-memory repo stubs.
type Store interface {
	Create(ctx context.Context, t *Template) error

	// Get returns the row when:
	//   - the id matches AND the caller owns it, OR
	//   - the id matches AND the row is shared.
	// Private rows owned by another user produce ErrNotFound.
	Get(ctx context.Context, id, callerID string) (*Template, error)

	// List returns rows visible to the caller for the given scope:
	//   - rows owned by the caller (regardless of shared), AND
	//   - rows owned by anyone where shared = TRUE.
	// Empty ontology / actionType returns every visible row owned by
	// or shared with the caller.
	List(ctx context.Context, callerID, ontology, actionType string) ([]*Template, error)

	// Update applies a partial update. Only the owner may update;
	// non-owners always see ErrNotFound (regardless of shared).
	Update(ctx context.Context, id, ownerID string, upd Update) error

	// Delete removes a template. Only the owner may delete.
	Delete(ctx context.Context, id, ownerID string) error
}

// MemoryStore is the in-memory Store impl used in tests and degraded
// (no-PG) deployments. Safe for concurrent use.
type MemoryStore struct {
	mu      sync.RWMutex
	rows    map[string]*Template // keyed by id
	nameIdx map[string]string    // keyed by createdBy + "\x00" + actionType + "\x00" + name → id
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		rows:    map[string]*Template{},
		nameIdx: map[string]string{},
	}
}

func nameKey(createdBy, actionType, name string) string {
	return createdBy + "\x00" + actionType + "\x00" + name
}

func cloneTemplate(t *Template) *Template {
	cp := *t
	if t.Parameters != nil {
		cp.Parameters = append(json.RawMessage(nil), t.Parameters...)
	}
	return &cp
}

// Create inserts t. Per-owner uniqueness is keyed by (createdBy,
// actionType, name). Stamps timestamps when zero. Returns
// ErrNameConflict on collision.
func (m *MemoryStore) Create(_ context.Context, t *Template) error {
	if t == nil {
		return errors.New("actiontemplates: nil template")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := nameKey(t.CreatedBy, t.ActionType, t.Name)
	if _, exists := m.nameIdx[key]; exists {
		return ErrNameConflict
	}
	if _, exists := m.rows[t.ID]; exists {
		return errors.New("actiontemplates: id already exists")
	}
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = now
	}
	if len(t.Parameters) == 0 {
		t.Parameters = json.RawMessage("{}")
	}
	m.rows[t.ID] = cloneTemplate(t)
	m.nameIdx[key] = t.ID
	return nil
}

// Get returns the row when callerID owns it OR the row is shared.
func (m *MemoryStore) Get(_ context.Context, id, callerID string) (*Template, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	row, ok := m.rows[id]
	if !ok {
		return nil, ErrNotFound
	}
	if row.CreatedBy != callerID && !row.Shared {
		return nil, ErrNotFound
	}
	return cloneTemplate(row), nil
}

// List returns rows visible to callerID for the (ontology,
// actionType) tuple. Empty ontology / actionType act as wildcards.
func (m *MemoryStore) List(_ context.Context, callerID, ontology, actionType string) ([]*Template, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Template, 0, len(m.rows))
	for _, row := range m.rows {
		if row.CreatedBy != callerID && !row.Shared {
			continue
		}
		if ontology != "" && row.Ontology != ontology {
			continue
		}
		if actionType != "" && row.ActionType != actionType {
			continue
		}
		out = append(out, cloneTemplate(row))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].CreatedBy < out[j].CreatedBy
	})
	return out, nil
}

// Update applies a partial update. Only the owner may update;
// non-owners always see ErrNotFound. Renaming respects per-owner
// uniqueness.
func (m *MemoryStore) Update(_ context.Context, id, ownerID string, upd Update) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[id]
	if !ok || row.CreatedBy != ownerID {
		return ErrNotFound
	}
	if upd.Name != nil && *upd.Name != row.Name {
		newKey := nameKey(ownerID, row.ActionType, *upd.Name)
		if other, exists := m.nameIdx[newKey]; exists && other != id {
			return ErrNameConflict
		}
		delete(m.nameIdx, nameKey(ownerID, row.ActionType, row.Name))
		row.Name = *upd.Name
		m.nameIdx[newKey] = id
	}
	if upd.Parameters != nil {
		params := *upd.Parameters
		if len(params) == 0 {
			params = json.RawMessage("{}")
		}
		row.Parameters = append(json.RawMessage(nil), params...)
	}
	if upd.Shared != nil {
		row.Shared = *upd.Shared
	}
	row.UpdatedAt = time.Now().UTC()
	return nil
}

// Delete removes a template. Only the owner may delete; non-owners
// see ErrNotFound.
func (m *MemoryStore) Delete(_ context.Context, id, ownerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[id]
	if !ok || row.CreatedBy != ownerID {
		return ErrNotFound
	}
	delete(m.rows, id)
	delete(m.nameIdx, nameKey(row.CreatedBy, row.ActionType, row.Name))
	return nil
}
