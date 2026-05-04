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
// template id that no longer exists OR is not visible to the caller
// under the current Scope rules. The handler maps this to 404 — the
// caller never learns whether the id existed under a different owner.
var ErrNotFound = errors.New("actiontemplates: not found")

// ErrNameConflict is returned when a Create / Update would result in
// two rows with the same (createdBy, actionType, name) tuple. Maps
// to 409 at the handler.
var ErrNameConflict = errors.New("actiontemplates: name conflict")

// Visibility carries the per-request inputs the store needs to
// enforce Scope rules without reaching into pkg/auth itself.
//
//   - CallerID:  the authenticated user id; rows whose CreatedBy
//                matches are always visible.
//   - Teammates: ids of users who share at least one group with
//                CallerID. Rows scoped TEAM are visible when their
//                CreatedBy is in this list. Empty / nil means "no
//                team mates" (TEAM rows from other owners stay
//                hidden).
//
// Empty CallerID is rejected at the handler layer; the store assumes
// it is always populated.
type Visibility struct {
	CallerID  string
	Teammates []string
}

// Store is the narrow persistence surface. Kept off oms.Repository
// (Pattern #18) so adding action templates doesn't cascade into the
// codebase's many in-memory repo stubs.
type Store interface {
	Create(ctx context.Context, t *Template) error

	// Get returns the row when it is visible to vis under Scope rules.
	// Cross-owner private rows produce ErrNotFound (no id leak).
	Get(ctx context.Context, id string, vis Visibility) (*Template, error)

	// List returns rows visible to vis for the given scope:
	//   - rows owned by vis.CallerID (regardless of Scope), AND
	//   - rows with Scope=PUBLIC, AND
	//   - rows with Scope=TEAM whose CreatedBy ∈ vis.Teammates.
	// Empty ontology / actionType returns every visible row.
	List(ctx context.Context, vis Visibility, ontology, actionType string) ([]*Template, error)

	// Update applies a partial update. Only the owner may update;
	// non-owners always see ErrNotFound (regardless of Scope).
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

// canSee returns true when row is visible under Scope rules to vis.
func canSee(row *Template, vis Visibility) bool {
	if row.CreatedBy == vis.CallerID {
		return true
	}
	switch row.Scope {
	case ScopePublic:
		return true
	case ScopeTeam:
		for _, mate := range vis.Teammates {
			if mate == row.CreatedBy {
				return true
			}
		}
		return false
	default:
		return false
	}
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
	if t.Scope == "" {
		t.Scope = ScopeFromShared(t.Shared)
	}
	t.Shared = SharedFromScope(t.Scope)
	m.rows[t.ID] = cloneTemplate(t)
	m.nameIdx[key] = t.ID
	return nil
}

// Get returns the row when it is visible under Scope rules.
func (m *MemoryStore) Get(_ context.Context, id string, vis Visibility) (*Template, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	row, ok := m.rows[id]
	if !ok {
		return nil, ErrNotFound
	}
	if !canSee(row, vis) {
		return nil, ErrNotFound
	}
	return cloneTemplate(row), nil
}

// List returns rows visible to vis for the (ontology, actionType)
// tuple. Empty ontology / actionType act as wildcards.
func (m *MemoryStore) List(_ context.Context, vis Visibility, ontology, actionType string) ([]*Template, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Template, 0, len(m.rows))
	for _, row := range m.rows {
		if !canSee(row, vis) {
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
	if upd.Scope != nil {
		row.Scope = *upd.Scope
		row.Shared = SharedFromScope(row.Scope)
	} else if upd.Shared != nil {
		row.Scope = ScopeFromShared(*upd.Shared)
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
