package rls

import (
	"context"
	"fmt"
	"sync"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/oss/where"
)

// Engine indexes RowPolicy rows by ObjectType RID and compiles the union of
// applicable predicates into a Bleve query at read time. Policies are kept
// in memory for the lifetime of the process — call Reload after any write
// to refresh the cache. Safe for concurrent use.
type Engine struct {
	store       Store
	groupLookup GroupMembershipLookup

	mu           sync.RWMutex
	byObjectType map[string][]*RowPolicy
}

// New returns an Engine with an empty cache. Callers MUST invoke Reload
// before relying on Compile, otherwise every lookup falls through to
// "no policies exist". Passing a nil groupLookup disables group-scoped
// matching — policies that only list Groups silently never apply.
func New(store Store, gl GroupMembershipLookup) *Engine {
	return &Engine{
		store:        store,
		groupLookup:  gl,
		byObjectType: make(map[string][]*RowPolicy),
	}
}

// Reload pulls the current set of policies from the store and replaces the
// in-memory index. Callers should invoke this after every admin write.
// A store failure aborts the reload — the previous cache survives so a
// transient DB hiccup does not wipe enforcement.
func (e *Engine) Reload(ctx context.Context) error {
	if e == nil || e.store == nil {
		return nil
	}
	rows, err := e.store.List(ctx)
	if err != nil {
		return fmt.Errorf("rls.Reload: %w", err)
	}
	index := make(map[string][]*RowPolicy, len(rows))
	for _, p := range rows {
		index[p.ObjectTypeRID] = append(index[p.ObjectTypeRID], p)
	}
	e.mu.Lock()
	e.byObjectType = index
	e.mu.Unlock()
	return nil
}

// SetPolicies replaces the cached policies for a single ObjectType RID.
// Used by tests and by the admin handler's fast-path refresh. Passing an
// empty slice drops the entry.
func (e *Engine) SetPolicies(objectTypeRID string, ps []*RowPolicy) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(ps) == 0 {
		delete(e.byObjectType, objectTypeRID)
		return
	}
	copied := make([]*RowPolicy, len(ps))
	for i, p := range ps {
		cp := *p
		copied[i] = &cp
	}
	e.byObjectType[objectTypeRID] = copied
}

// Compile builds the Bleve query representing the union of every RowPolicy
// whose AppliesTo matches user. Semantics:
//
//   - nil user                       → nil (no filter, but callers should
//     have already rejected anonymous reads via middleware)
//   - admin (PermUserManage role)    → nil (bypass; matches security.Engine)
//   - no policies on ObjectType      → nil (full visibility)
//   - policies exist, none applicable→ nil (permissive: admins can layer
//     filters without locking out unrelated users)
//   - one applicable policy          → that policy's compiled predicate
//   - N applicable policies          → DisjunctionQuery (OR-combined)
//
// A nil return means "no additional filter"; callers keep their base query.
func (e *Engine) Compile(ctx context.Context, user *auth.User, objectTypeRID string) (query.Query, error) {
	if e == nil || user == nil {
		return nil, nil
	}
	if auth.HasPermission(user.Roles, auth.PermUserManage) {
		return nil, nil
	}

	e.mu.RLock()
	policies := e.byObjectType[objectTypeRID]
	e.mu.RUnlock()
	if len(policies) == 0 {
		return nil, nil
	}

	var userGroups []string
	if e.groupLookup != nil {
		g, err := e.groupLookup.UserGroups(ctx, user.ID)
		if err != nil {
			return nil, fmt.Errorf("rls: group lookup: %w", err)
		}
		userGroups = g
	}

	var clauses []query.Query
	for _, p := range policies {
		if !p.AppliesTo.IsApplicable(user, userGroups) {
			continue
		}
		var clause where.WhereClause
		if err := unmarshalPredicate(p.Predicate, &clause); err != nil {
			return nil, fmt.Errorf("rls: policy %s: %w", p.RID, err)
		}
		q, err := where.ConvertToBleveQuery(&clause)
		if err != nil {
			return nil, fmt.Errorf("rls: policy %s: %w", p.RID, err)
		}
		clauses = append(clauses, q)
	}

	switch len(clauses) {
	case 0:
		return nil, nil
	case 1:
		return clauses[0], nil
	default:
		dq := bleve.NewDisjunctionQuery(clauses...)
		dq.SetMin(1)
		return dq, nil
	}
}

// Size returns the number of policies cached for an ObjectType RID. Mainly
// useful for tests and health endpoints.
func (e *Engine) Size(objectTypeRID string) int {
	if e == nil {
		return 0
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.byObjectType[objectTypeRID])
}
