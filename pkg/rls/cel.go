package rls

import (
	"context"
	"fmt"
	"sync"

	celpkg "github.com/liyang/weave/pkg/cel"
	"github.com/liyang/weave/pkg/auth"
)

// celValidate is the package-internal alias for pkg/cel.Validate. Kept as
// a named indirection so the handler tests can mock it cheaply if they
// want to and so the import surface from handlers.go stays single-line.
func celValidate(expression string) error {
	return celpkg.Validate(expression)
}

// celProgramCache holds compiled cel programs keyed by RowPolicy RID.
// Populated by Engine.Reload after every store refresh and read in the
// hot path by Engine.EvaluateRowCEL. Safe for concurrent reads while
// Reload swaps the map atomically under the engine's write lock.
type celProgramCache struct {
	mu       sync.RWMutex
	programs map[string]*celpkg.Program
}

func newCELProgramCache() *celProgramCache {
	return &celProgramCache{programs: make(map[string]*celpkg.Program)}
}

func (c *celProgramCache) get(rid string) *celpkg.Program {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.programs[rid]
}

func (c *celProgramCache) replace(next map[string]*celpkg.Program) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.programs = next
}

// userBindingFor flattens an auth.User into the CEL "user.*" binding map.
// Keys are stable: id, email, roles, groups, attributes (the attributes
// map is exposed as-is so CEL expressions can reach into arbitrary
// custom claims like user.attributes.dept or user.attributes.clearance).
// userGroups is passed separately because the engine resolves it lazily
// via GroupMembershipLookup and may have it ready by the time the CEL
// pass runs.
func userBindingFor(user *auth.User, userGroups []string) map[string]any {
	if user == nil {
		return map[string]any{}
	}
	roles := make([]string, len(user.Roles))
	copy(roles, user.Roles)
	groups := make([]string, len(userGroups))
	copy(groups, userGroups)
	attrs := user.Attributes
	if attrs == nil {
		attrs = map[string]any{}
	}
	// Promote first-level Attributes keys to top-level "user.*" so PRD
	// expressions like "user.dept" and "user.clearance" work without
	// needing "user.attributes.dept". The fully-qualified form remains
	// available via user.attributes.*.
	binding := map[string]any{
		"id":         user.ID,
		"email":      user.Email,
		"roles":      roles,
		"groups":     groups,
		"attributes": attrs,
	}
	for k, v := range attrs {
		if _, exists := binding[k]; exists {
			// Don't let attributes shadow the canonical id/email/roles
			// keys — that would let a malicious attribute payload
			// rewrite the policy's view of the caller's role set.
			continue
		}
		binding[k] = v
	}
	return binding
}

// EvaluateRowCEL is the post-load CEL gate. For each policy on otRID that
// (a) has a CEL expression and (b) applies to the caller, the policy's
// compiled program is evaluated against the (user, object) binding.
//
// Combination semantics mirror Engine.Compile's OR-shape for legacy
// Bleve-side policies: the gate passes if ANY applicable CEL policy
// returns true; it fails closed if every applicable policy returns
// false. When no CEL policies apply (or no CEL policies exist for this
// ObjectType), the gate is open and the row passes.
//
// Returns (true, nil) when the row is allowed. Errors propagate
// unwrapped so the caller can fail-closed at the outer layer — every
// non-nil err must be treated as "deny".
func (e *Engine) EvaluateRowCEL(ctx context.Context, user *auth.User, objectTypeRID string, properties map[string]any) (bool, error) {
	if e == nil {
		return true, nil
	}
	// Match Engine.Compile's contract: nil user => no decision (admins
	// have already been bypassed at the caller).
	if user == nil {
		return true, nil
	}
	if auth.HasPermission(user.Roles, auth.PermUserManage) {
		return true, nil
	}

	e.mu.RLock()
	policies := e.byObjectType[objectTypeRID]
	e.mu.RUnlock()
	if len(policies) == 0 {
		return true, nil
	}

	// Resolve group membership once per call. Lazy lookup matches Compile.
	var userGroups []string
	if e.groupLookup != nil {
		g, err := e.groupLookup.UserGroups(ctx, user.ID)
		if err != nil {
			return false, fmt.Errorf("rls: group lookup: %w", err)
		}
		userGroups = g
	}

	binding := celpkg.Binding{
		User:   userBindingFor(user, userGroups),
		Object: properties,
	}

	anyApplicable := false
	for _, p := range policies {
		if !p.HasCEL() {
			continue
		}
		if !p.AppliesTo.IsApplicable(user, userGroups) {
			continue
		}
		anyApplicable = true
		prg := e.celCache.get(p.RID)
		if prg == nil {
			// Compile-on-demand fallback: should not normally happen
			// because Reload pre-populates, but guarantees we don't
			// silently allow rows when a policy's program is missing.
			compiled, err := celpkg.Compile(p.CELExpression)
			if err != nil {
				return false, fmt.Errorf("rls: policy %s compile: %w", p.RID, err)
			}
			prg = compiled
		}
		ok, err := prg.Eval(binding)
		if err != nil {
			return false, fmt.Errorf("rls: policy %s eval: %w", p.RID, err)
		}
		if ok {
			return true, nil
		}
	}
	// No CEL policies declared for this OT, or none of the CEL policies
	// applied to this caller: the CEL gate is open.
	if !anyApplicable {
		return true, nil
	}
	// At least one CEL policy applied, and all of them rejected the row.
	return false, nil
}

// HasCELForObjectType reports whether any policy on objectTypeRID
// carries a CEL expression. OSS uses this to skip the per-row CEL pass
// when no policy needs it — avoids paying the Bleve→WireObject
// materialization cost on the unrelated read paths.
func (e *Engine) HasCELForObjectType(objectTypeRID string) bool {
	if e == nil {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, p := range e.byObjectType[objectTypeRID] {
		if p.HasCEL() {
			return true
		}
	}
	return false
}
