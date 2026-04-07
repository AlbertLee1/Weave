package oss

import (
	"context"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/oms"
)

// PolicyFilter is the OSS-side ABAC enforcement layer. It loads
// SecurityPolicies for an ObjectType from the OMS repository, evaluates them
// against each candidate WireObject, drops the rows the user can't see, and
// redacts property values listed in PROPERTY-scope masks.
//
// Lifecycle: one PolicyFilter instance is wired into ServiceImpl during
// server boot. It is goroutine-safe; concurrent reads share the same
// underlying repository handle.
//
// Performance: this MVP looks policies up on every call. A small in-process
// cache (TTL ~30s, keyed by objectTypeRID) can be added later when policy
// volume justifies it; the public surface does not need to change.
type PolicyFilter struct {
	repo oms.Repository
}

// NewPolicyFilter constructs a PolicyFilter backed by the given OMS
// repository. The repository is used only for ListSecurityPolicies and
// GetObjectTypeByAPIName lookups.
func NewPolicyFilter(repo oms.Repository) *PolicyFilter {
	return &PolicyFilter{repo: repo}
}

// FilterObjects applies ABAC policies to the given slice of WireObjects and
// returns a new slice containing only the visible rows with masked property
// values stripped from each row's Properties map.
//
// Fast paths (in order):
//
//  1. Empty input -> return as-is.
//  2. Nil filter (defensive) -> return as-is.
//  3. Admin role -> bypass policy evaluation entirely.
//  4. ObjectType has no attached policies -> return as-is (back-compat).
//
// On any other path the evaluator runs once per object. The returned slice
// is a fresh allocation; callers do NOT need to copy it before mutation.
func (f *PolicyFilter) FilterObjects(
	ctx context.Context,
	user *auth.User,
	ontologyRID, objectTypeAPIName string,
	objects []*WireObject,
) ([]*WireObject, error) {
	// 1. Empty input.
	if len(objects) == 0 {
		return objects, nil
	}
	// 2. Defensive nil receiver.
	if f == nil || f.repo == nil {
		return objects, nil
	}
	// 3. Admin bypass. Admins always see everything; this also matches the
	//    dev-mode default user that has Roles=["admin"].
	if user != nil && hasAdminRole(user.Roles) {
		return objects, nil
	}

	// Look up the ObjectType to translate the apiName into an RID, which is
	// the key on security_policies. If the type vanished between the index
	// query and now, fail closed (return nothing) rather than panic.
	ot, err := f.repo.GetObjectTypeByAPIName(ctx, ontologyRID, objectTypeAPIName)
	if err != nil {
		return nil, err
	}

	policies, err := f.repo.ListSecurityPolicies(ctx, ot.RID)
	if err != nil {
		return nil, err
	}

	// 4. No policies attached -> back-compat pass-through.
	if len(policies) == 0 {
		return objects, nil
	}

	evaluator := auth.NewPolicyEvaluator(policies)
	out := make([]*WireObject, 0, len(objects))
	for _, obj := range objects {
		if obj == nil {
			continue
		}
		allow, masks, err := evaluator.Evaluate(user, obj.Properties)
		if err != nil {
			return nil, err
		}
		if !allow {
			continue
		}
		out = append(out, redactObject(obj, masks))
	}
	return out, nil
}

// hasAdminRole reports whether any of the given role names is the admin role.
// Pulled out for readability and so the bypass can be tweaked in one place.
func hasAdminRole(roles []string) bool {
	for _, r := range roles {
		if r == auth.RoleAdmin {
			return true
		}
	}
	return false
}

// redactObject returns a shallow-copied WireObject with the masked fields
// removed from the Properties map. The original object is left untouched so
// the caller can re-use it if needed (and so concurrent evaluators don't
// race on the same map).
func redactObject(obj *WireObject, masks []string) *WireObject {
	if len(masks) == 0 {
		return obj
	}
	maskSet := make(map[string]bool, len(masks))
	for _, m := range masks {
		maskSet[m] = true
	}
	props := make(map[string]interface{}, len(obj.Properties))
	for k, v := range obj.Properties {
		if maskSet[k] {
			continue
		}
		props[k] = v
	}
	return &WireObject{
		RID:        obj.RID,
		PrimaryKey: obj.PrimaryKey,
		APIName:    obj.APIName,
		Properties: props,
	}
}
