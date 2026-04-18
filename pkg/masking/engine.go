package masking

import (
	"context"
	"fmt"
	"sync"

	"github.com/liyang/weave/pkg/auth"
)

// Engine indexes ColumnMask rows by ObjectType RID and compiles the set of
// applicable mask transforms for a given caller at read time. Policies are
// kept in memory for the lifetime of the process — call Reload after any
// write to refresh the cache. Safe for concurrent use.
type Engine struct {
	store       Store
	groupLookup GroupMembershipLookup

	mu           sync.RWMutex
	byObjectType map[string][]*ColumnMask
}

// New returns an Engine with an empty cache. Call Reload before relying on
// Compile. A nil groupLookup disables Group-scoped AppliesTo matching; the
// engine behaves as if the caller is in no groups.
func New(store Store, gl GroupMembershipLookup) *Engine {
	return &Engine{
		store:        store,
		groupLookup:  gl,
		byObjectType: make(map[string][]*ColumnMask),
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
		return fmt.Errorf("masking.Reload: %w", err)
	}
	index := make(map[string][]*ColumnMask, len(rows))
	for _, m := range rows {
		index[m.ObjectTypeRID] = append(index[m.ObjectTypeRID], m)
	}
	e.mu.Lock()
	e.byObjectType = index
	e.mu.Unlock()
	return nil
}

// SetMasks replaces the cached masks for a single ObjectType RID. Used by
// tests and the admin handler's fast-path refresh. Passing an empty slice
// drops the entry.
func (e *Engine) SetMasks(objectTypeRID string, masks []*ColumnMask) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(masks) == 0 {
		delete(e.byObjectType, objectTypeRID)
		return
	}
	copied := make([]*ColumnMask, len(masks))
	for i, m := range masks {
		cp := *m
		copied[i] = &cp
	}
	e.byObjectType[objectTypeRID] = copied
}

// Compile resolves the mask transforms that SHOULD be applied to the caller.
// A mask's AppliesTo lists the identities ALLOWED to see clear data; callers
// in the allow list are skipped (no transform). Callers NOT in the allow
// list receive the transform.
//
// Semantics:
//   - nil user                     → nil (no masks; callers should have
//     rejected anonymous reads via middleware before reaching this)
//   - admin (PermUserManage)       → nil (bypass; admins always see clear
//     data, matches security.Engine + rls.Engine conventions)
//   - no masks on ObjectType       → nil (no masking)
//   - masks exist, all allow the caller  → empty map (treated by callers as
//     "no transforms"; empty vs nil is only important for tests)
//   - masks exist, some apply      → map[propertyApiName]MaskRule with one
//     entry per property
//
// Per-property, the LAST mask in iteration order wins when multiple masks
// target the same property (store returns a stable list, so this is
// deterministic per reload). In practice admins should author one mask per
// property; conflicts are a configuration error and are not actively
// rejected — the store layer does not enforce uniqueness.
func (e *Engine) Compile(ctx context.Context, user *auth.User, objectTypeRID string) (map[string]MaskRule, error) {
	if e == nil || user == nil {
		return nil, nil
	}
	if auth.HasPermission(user.Roles, auth.PermUserManage) {
		return nil, nil
	}

	e.mu.RLock()
	masks := e.byObjectType[objectTypeRID]
	e.mu.RUnlock()
	if len(masks) == 0 {
		return nil, nil
	}

	var userGroups []string
	if e.groupLookup != nil {
		g, err := e.groupLookup.UserGroups(ctx, user.ID)
		if err != nil {
			return nil, fmt.Errorf("masking: group lookup: %w", err)
		}
		userGroups = g
	}

	out := make(map[string]MaskRule)
	for _, m := range masks {
		// AppliesTo carries the ALLOWED identities. Non-empty allow list +
		// caller matches ⇒ skip (clear view). Non-empty allow list + caller
		// does not match ⇒ apply. Empty allow list ⇒ always apply (no one
		// allowed except admin, which already short-circuited above).
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
	return len(e.byObjectType[objectTypeRID])
}

func hasAllowList(a AppliesTo) bool {
	return len(a.Roles) > 0 || len(a.Groups) > 0 || len(a.Users) > 0
}
