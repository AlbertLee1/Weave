package rls

import (
	"context"
	"fmt"
	"sync"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"

	"github.com/liyang/weave/pkg/auth"
	celpkg "github.com/liyang/weave/pkg/cel"
	"github.com/liyang/weave/pkg/oss/where"
)

// Engine indexes RowPolicy rows by ObjectType RID and compiles the union of
// applicable predicates into a Bleve query at read time. Policies are kept
// in memory for the lifetime of the process — call Reload after any write
// to refresh the cache. Safe for concurrent use.
//
// US-487 extension: when a RowPolicy carries a CEL expression, its
// compiled cel.Program is cached separately in celCache and consulted by
// EvaluateRowCEL during the OSS post-filter pass. Bleve-side WhereClause
// predicates and CEL expressions are independent enforcement lanes; see
// EvaluateRowCEL for the gate combination rules.
type Engine struct {
	store       Store
	groupLookup GroupMembershipLookup

	mu           sync.RWMutex
	byObjectType map[string][]*RowPolicy

	celCache *celProgramCache
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
		celCache:     newCELProgramCache(),
	}
}

// Reload pulls the current set of policies from the store and replaces the
// in-memory index. Callers should invoke this after every admin write.
// A store failure aborts the reload — the previous cache survives so a
// transient DB hiccup does not wipe enforcement.
//
// US-487: alongside the legacy index this rebuilds the CEL program
// cache by compiling each policy's CELExpression. A compile failure for
// ONE policy does NOT abort the reload — the offending policy is
// recorded in the index but missing from celCache, so EvaluateRowCEL
// will fail-closed on it via the on-demand fallback. This keeps a
// single broken CEL from disabling enforcement for unrelated policies.
func (e *Engine) Reload(ctx context.Context) error {
	if e == nil || e.store == nil {
		return nil
	}
	rows, err := e.store.List(ctx)
	if err != nil {
		return fmt.Errorf("rls.Reload: %w", err)
	}
	index := make(map[string][]*RowPolicy, len(rows))
	programs := make(map[string]*celpkg.Program)
	for _, p := range rows {
		index[p.ObjectTypeRID] = append(index[p.ObjectTypeRID], p)
		if p.HasCEL() {
			prg, cerr := celpkg.Compile(p.CELExpression)
			if cerr != nil {
				// Quarantine the broken policy: skip caching its
				// program so EvaluateRowCEL surfaces a runtime error
				// rather than silently passing rows.
				continue
			}
			programs[p.RID] = prg
		}
	}
	e.mu.Lock()
	e.byObjectType = index
	e.mu.Unlock()
	e.celCache.replace(programs)
	return nil
}

// SetPolicies replaces the cached policies for a single ObjectType RID.
// Used by tests and by the admin handler's fast-path refresh. Passing an
// empty slice drops the entry. US-487 CEL programs are recompiled for
// the newly-attached policies so EvaluateRowCEL stays consistent with
// the cache — broken CEL is quarantined the same way Reload handles it.
func (e *Engine) SetPolicies(objectTypeRID string, ps []*RowPolicy) {
	if e == nil {
		return
	}
	e.mu.Lock()
	if len(ps) == 0 {
		delete(e.byObjectType, objectTypeRID)
		e.mu.Unlock()
		e.rebuildCELCacheLocked()
		return
	}
	copied := make([]*RowPolicy, len(ps))
	for i, p := range ps {
		cp := *p
		copied[i] = &cp
	}
	e.byObjectType[objectTypeRID] = copied
	e.mu.Unlock()
	e.rebuildCELCacheLocked()
}

// rebuildCELCacheLocked rebuilds the CEL program cache from the current
// byObjectType index. Pulled out so SetPolicies and any future targeted
// refresh path share a single rebuild routine. Acquires its own RLock
// internally — callers must NOT hold the write lock.
func (e *Engine) rebuildCELCacheLocked() {
	e.mu.RLock()
	programs := make(map[string]*celpkg.Program)
	for _, ps := range e.byObjectType {
		for _, p := range ps {
			if !p.HasCEL() {
				continue
			}
			prg, err := celpkg.Compile(p.CELExpression)
			if err != nil {
				continue
			}
			programs[p.RID] = prg
		}
	}
	e.mu.RUnlock()
	e.celCache.replace(programs)
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
		// US-487: skip CEL-only policies — they are evaluated in the
		// post-filter lane by EvaluateRowCEL, not compiled into Bleve.
		if len(p.Predicate) == 0 || string(p.Predicate) == "null" {
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
