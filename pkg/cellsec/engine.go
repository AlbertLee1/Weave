package cellsec

import (
	"context"
	"fmt"
	"sync"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/masking"
)

// Engine indexes CellMask rows by (ObjectType RID, primary key) so a query-
// time lookup for a specific row yields its applicable cell-level transforms
// in O(1). Policies are kept in memory for the lifetime of the process —
// call Reload after any write to refresh the cache. Safe for concurrent use.
type Engine struct {
	store       Store
	groupLookup GroupMembershipLookup

	mu      sync.RWMutex
	byOTKey map[string]map[string][]*CellMask // otRID → primaryKey → masks
}

// New returns an Engine with an empty cache. Call Reload before relying on
// Compile. A nil groupLookup disables Group-scoped AppliesTo matching; the
// engine behaves as if the caller is in no groups.
func New(store Store, gl GroupMembershipLookup) *Engine {
	return &Engine{
		store:       store,
		groupLookup: gl,
		byOTKey:     make(map[string]map[string][]*CellMask),
	}
}

// Reload pulls the current set of masks from the store and replaces the
// in-memory index. A store failure aborts the reload so a transient DB
// hiccup does not wipe enforcement.
func (e *Engine) Reload(ctx context.Context) error {
	if e == nil || e.store == nil {
		return nil
	}
	rows, err := e.store.List(ctx)
	if err != nil {
		return fmt.Errorf("cellsec.Reload: %w", err)
	}
	index := make(map[string]map[string][]*CellMask, len(rows))
	for _, m := range rows {
		pkIndex, ok := index[m.ObjectTypeRID]
		if !ok {
			pkIndex = make(map[string][]*CellMask)
			index[m.ObjectTypeRID] = pkIndex
		}
		pkIndex[m.PrimaryKey] = append(pkIndex[m.PrimaryKey], m)
	}
	e.mu.Lock()
	e.byOTKey = index
	e.mu.Unlock()
	return nil
}

// Compile resolves the cell-mask transforms that SHOULD be applied to the
// caller for the cell located at (objectTypeRID, primaryKey).
//
// Semantics match pkg/masking.Engine:
//   - nil user                     → nil (no masks)
//   - admin (PermUserManage)       → nil (bypass)
//   - no masks on (OT, PK)         → nil
//   - masks exist, all allow caller → empty map
//   - masks exist, some apply      → map[propertyApiName]MaskRule
//
// When multiple masks target the same property on the same cell, the LAST
// mask in iteration order wins; admins should author one mask per (cell,
// property) tuple.
func (e *Engine) Compile(ctx context.Context, user *auth.User, objectTypeRID, primaryKey string) (map[string]masking.MaskRule, error) {
	if e == nil || user == nil {
		return nil, nil
	}
	if auth.HasPermission(user.Roles, auth.PermUserManage) {
		return nil, nil
	}

	e.mu.RLock()
	pkIndex, ok := e.byOTKey[objectTypeRID]
	var masks []*CellMask
	if ok {
		masks = pkIndex[primaryKey]
	}
	e.mu.RUnlock()
	if len(masks) == 0 {
		return nil, nil
	}

	var userGroups []string
	if e.groupLookup != nil {
		g, err := e.groupLookup.UserGroups(ctx, user.ID)
		if err != nil {
			return nil, fmt.Errorf("cellsec: group lookup: %w", err)
		}
		userGroups = g
	}

	out := make(map[string]masking.MaskRule)
	for _, m := range masks {
		if hasAllowList(m.AppliesTo) && m.AppliesTo.IsApplicable(user, userGroups) {
			continue
		}
		out[m.PropertyAPIName] = m.MaskRule
	}
	return out, nil
}

// Size returns the number of masks cached for an ObjectType RID. Useful for
// handler tests (verifying post-write Reload) and health probes.
func (e *Engine) Size(objectTypeRID string) int {
	if e == nil {
		return 0
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	total := 0
	for _, masks := range e.byOTKey[objectTypeRID] {
		total += len(masks)
	}
	return total
}

// SetMasks replaces the cached masks for a single (ObjectType RID, primaryKey)
// pair. Used by tests and the admin handler's fast-path refresh. Passing an
// empty slice drops the entry.
func (e *Engine) SetMasks(objectTypeRID, primaryKey string, masks []*CellMask) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	pkIndex, ok := e.byOTKey[objectTypeRID]
	if len(masks) == 0 {
		if ok {
			delete(pkIndex, primaryKey)
			if len(pkIndex) == 0 {
				delete(e.byOTKey, objectTypeRID)
			}
		}
		return
	}
	if !ok {
		pkIndex = make(map[string][]*CellMask)
		e.byOTKey[objectTypeRID] = pkIndex
	}
	copied := make([]*CellMask, len(masks))
	for i, m := range masks {
		cp := *m
		copied[i] = &cp
	}
	pkIndex[primaryKey] = copied
}

func hasAllowList(a masking.AppliesTo) bool {
	return len(a.Roles) > 0 || len(a.Groups) > 0 || len(a.Users) > 0
}
