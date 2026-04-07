package oss

import (
	"context"

	"github.com/liyang/weave/pkg/auth"
)

// MarkingFilter is the OSS-side mandatory access control enforcement
// layer. It loads the requesting user's marking grants from a
// MarkingRepository, then drops every WireObject whose __markings field
// includes a marking the user does not hold.
//
// Unlike PolicyFilter (ABAC, with allow/deny rules and property masks),
// MarkingFilter has no notion of conditions or effects: a missing grant
// is always a hard deny. The only escape hatches are an explicit grant
// via MarkingRepository.GrantMarking, or admin role short-circuit
// handled at a higher level.
//
// Lifecycle: one MarkingFilter instance is wired into ServiceImpl during
// server boot via SetMarkingFilter. It is goroutine-safe; the underlying
// repository handle is shared.
//
// Performance: this MVP looks user grants up on every call. A small
// in-process cache (TTL ~30s, keyed by userID) can be added later when
// volume justifies it; the public surface does not need to change.
type MarkingFilter struct {
	repo auth.MarkingRepository
}

// NewMarkingFilter constructs a MarkingFilter backed by the given
// MarkingRepository. The repository is used only for GetUserMarkings
// lookups on the read path.
func NewMarkingFilter(repo auth.MarkingRepository) *MarkingFilter {
	return &MarkingFilter{repo: repo}
}

// FilterObjects applies marking-based MAC to the given slice of
// WireObjects and returns a fresh slice containing only the rows the
// user is cleared to see.
//
// Fast paths (in order):
//
//  1. Empty input -> return as-is.
//  2. Nil filter (defensive) -> return as-is.
//
// On any other path the filter loads the user's grants once and walks
// the slice. The returned slice is a fresh allocation; callers do NOT
// need to copy it before mutation.
func (f *MarkingFilter) FilterObjects(
	ctx context.Context,
	userID string,
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

	// Load grants once. An empty userID is allowed (e.g. dev mode with no
	// authenticated user) — that user simply gets the empty grant set,
	// which means only un-marked objects are visible.
	grantList, err := f.repo.GetUserMarkings(ctx, userID)
	if err != nil {
		return nil, err
	}
	grants := make(map[string]bool, len(grantList))
	for _, g := range grantList {
		grants[g] = true
	}

	out := make([]*WireObject, 0, len(objects))
	for _, obj := range objects {
		if obj == nil {
			continue
		}
		marks := extractMarkings(obj)
		// Un-marked rows are PUBLIC by definition (back-compat).
		if len(marks) == 0 {
			out = append(out, obj)
			continue
		}
		// Hard subset check: every marking on the object must be granted
		// to the user.
		ok := true
		for _, m := range marks {
			if !grants[m] {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, obj)
		}
	}
	return out, nil
}

// extractMarkings reads the reserved __markings field off a WireObject
// and returns its values as a string slice. Bleve and the JSON wire
// format both flatten multi-value fields in slightly different ways
// depending on cardinality, so this helper accepts:
//
//   - []string                — produced by Go writer paths
//   - []interface{}           — produced by JSON-decoded Bleve hits
//   - string                  — produced by Bleve when a single-value
//     keyword field happens to come back as a bare string
//
// Anything else (including nil) is treated as zero markings, which
// means the row is PUBLIC.
func extractMarkings(obj *WireObject) []string {
	if obj == nil || obj.Properties == nil {
		return nil
	}
	raw, ok := obj.Properties[auth.MarkingsField]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	default:
		return nil
	}
}
