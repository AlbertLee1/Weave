// VTX-123 — Scenario authorization & diff masking.
//
// Scenarios are private to their creator. Reads and writes are gated by
// AuthorizeRead / AuthorizeWrite: only the user named in scenario.CreatedBy
// (or an admin holding auth.PermUserManage) may access the row. Foreign
// callers receive ErrForbidden — even when they hold other roles like
// editor or ontology-owner, because a scenario is owner-scoped state, not
// ontology state.
//
// Diff masking layers on top: when a scenario edit references an object the
// caller is not cleared to see (object carries markings the caller does not
// hold), MaskEditsForUser replaces the edit's NewValue with the redaction
// sentinel. The op + identifiers are preserved so the diff UI can still
// surface that *something* changed without leaking the value itself —
// mirroring Foundry-style mandatory access control over scenario state.

package scenarios

import (
	"encoding/json"
	"errors"

	"github.com/liyang/weave/pkg/auth"
)

// ErrUnauthenticated indicates the request reached the authz layer with no
// resolved user. Callers should translate this to HTTP 401.
var ErrUnauthenticated = errors.New("unauthenticated")

// ErrForbidden indicates the resolved user is authenticated but is not
// authorized for the requested operation. Callers should translate this
// to HTTP 403.
var ErrForbidden = errors.New("forbidden")

// RedactedValueLiteral is the JSON literal substituted in for NewValue when
// the caller is not cleared to see an edit's underlying object. It is a
// valid JSON string so downstream consumers that json.Unmarshal the diff
// keep working; UIs can detect the literal and render a "[redacted]" badge.
const RedactedValueLiteral = `"[redacted]"`

// AuthorizeRead returns nil if `user` is allowed to read `scen`, otherwise
// a sentinel error. The contract:
//
//   - nil user            → ErrUnauthenticated (caller forgot middleware)
//   - nil scenario        → ErrScenarioNotFound (helps handlers compose the
//     repo lookup → authz chain without an extra nil-check)
//   - user.ID == CreatedBy → nil (owner can always read)
//   - admin (PermUserManage) → nil (matches masking / rls bypass convention)
//   - otherwise           → ErrForbidden
//
// Empty CreatedBy is treated as "no owner" and falls through to the role
// check, so seed scenarios written by an older migration without a creator
// are still gated by admin only.
func AuthorizeRead(user *auth.User, scen *Scenario) error {
	if user == nil {
		return ErrUnauthenticated
	}
	if scen == nil {
		return ErrScenarioNotFound
	}
	if user.ID != "" && user.ID == scen.CreatedBy {
		return nil
	}
	if auth.HasPermission(user.Roles, auth.PermUserManage) {
		return nil
	}
	return ErrForbidden
}

// AuthorizeWrite is the write-side counterpart. The matrix is currently the
// same as AuthorizeRead (only owner + admin), but it lives as a separate
// function so future stories can tighten or loosen one side without
// touching the other.
func AuthorizeWrite(user *auth.User, scen *Scenario) error {
	if user == nil {
		return ErrUnauthenticated
	}
	if scen == nil {
		return ErrScenarioNotFound
	}
	if user.ID != "" && user.ID == scen.CreatedBy {
		return nil
	}
	if auth.HasPermission(user.Roles, auth.PermUserManage) {
		return nil
	}
	return ErrForbidden
}

// MaskEditsForUser returns a copy of `edits` with NewValue redacted for any
// edit whose object carries markings the caller does not hold. Returned
// edits are independent of the input — callers may mutate the result
// without affecting the original slice.
//
// Rules:
//   - nil edits          → empty slice (callers can len() the result safely)
//   - admin user         → no redaction (consistent with masking.Compile)
//   - nil markings index → no redaction (objects assumed unmarked)
//   - object unmarked    → edit unchanged
//   - caller holds every required marking → edit unchanged (AND semantics)
//   - caller missing any → NewValue replaced by RedactedValueLiteral; the
//     op, ObjectType, ObjectID, Property and link fields stay intact so
//     diff UIs can still show "JFK.capacity changed (value hidden)"
//
// Link edits (addLink / deleteLink) carry no NewValue and so pass through
// unchanged regardless of markings; link-level access is the caller's
// concern.
func MaskEditsForUser(user *auth.User, edits []ScenarioEdit, markings map[ObjectKey][]string) []ScenarioEdit {
	if len(edits) == 0 {
		return []ScenarioEdit{}
	}
	out := make([]ScenarioEdit, len(edits))
	copy(out, edits)

	if user != nil && auth.HasPermission(user.Roles, auth.PermUserManage) {
		return out
	}
	if len(markings) == 0 {
		return out
	}
	held := heldMarkings(user)
	for i := range out {
		if len(out[i].NewValue) == 0 {
			continue
		}
		req := markings[ObjectKey{ObjectType: out[i].ObjectType, ObjectID: out[i].ObjectID}]
		if len(req) == 0 {
			continue
		}
		if auth.EvaluateMarkings(held, req) {
			continue
		}
		out[i].NewValue = json.RawMessage(RedactedValueLiteral)
	}
	return out
}

// heldMarkings is a thin wrapper around auth.Markings that tolerates the
// nil-user case so callers can fan out without an extra guard. It also
// short-circuits when Attributes is nil — auth.Markings already does this
// but the inlined check skips the map lookup.
func heldMarkings(user *auth.User) []string {
	if user == nil || user.Attributes == nil {
		return nil
	}
	raw, ok := user.Attributes[auth.MarkingsAttributeKey]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
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
