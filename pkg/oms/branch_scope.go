package oms

import (
	"context"
	"net/http"
)

// BranchHeader is the HTTP request header that pins a read to a
// non-main branch. PRD-V2 Gap-T4: callers can override the default
// "main" branch via either `?branch=<name>` query parameter (the
// historical signal, supported since US-381 / US-384) OR the
// `X-Weave-Branch: <name>` header introduced in round 39. The query
// param wins when both are present so explicit URL pinning beats
// the implicit per-client default — this matches Foundry's "request
// param is authoritative" rule.
const BranchHeader = "X-Weave-Branch"

// ResolveBranchFromRequest returns the effective branch name for r.
// Precedence:
//   1. ?branch=<name> query parameter (back-compat — every existing
//      caller in pkg/oms, pkg/oss, pkg/actions reads this first).
//   2. X-Weave-Branch HTTP header (round 39 / Gap-T4 addition —
//      lets a client pin to a non-main branch without rewriting
//      every URL).
//   3. DefaultBranch ("main") when neither is set.
//
// Returns the raw, untrimmed value so callers can decide whether
// to reject leading/trailing whitespace (pkg/oss/objectset's
// resolveBranch already does that for the loadObjects endpoint).
func ResolveBranchFromRequest(r *http.Request) string {
	if r == nil {
		return DefaultBranch
	}
	if q := r.URL.Query().Get("branch"); q != "" {
		return q
	}
	if h := r.Header.Get(BranchHeader); h != "" {
		return h
	}
	return DefaultBranch
}

// DefaultBranch is the canonical "main" branch identifier shared by every
// row written without an explicit branch. New schema columns introduced by
// US-384 (action_types.branch_id, functions.branch_id) default to this
// value at the SQL layer so legacy callers continue to land on the trunk.
const DefaultBranch = "main"

type branchScopeKey struct{}

// WithBranchScope stamps the requested branch name onto ctx so downstream
// repository read paths (US-384 OnBranch helpers) and the action /
// function dispatchers can route lookups to a branch-specific row when
// available, falling back to the main row otherwise. Empty input or the
// DefaultBranch sentinel is a no-op so legacy main-only paths stay free
// of context churn.
func WithBranchScope(ctx context.Context, branch string) context.Context {
	if branch == "" || branch == DefaultBranch {
		return ctx
	}
	return context.WithValue(ctx, branchScopeKey{}, branch)
}

// BranchScopeFromContext returns the branch name stamped via
// WithBranchScope. Returns DefaultBranch ("main") when no scope is set so
// the caller can treat the value as authoritative without a nil/empty
// guard.
func BranchScopeFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(branchScopeKey{}).(string); ok && v != "" {
		return v
	}
	return DefaultBranch
}

// NormalizeBranchID returns the supplied branch identifier with empty
// input substituted to DefaultBranch ("main"). Used at the storage
// boundary so a row never lands with an empty string in branch_id even
// if the caller forgot to populate the field.
func NormalizeBranchID(s string) string {
	if s == "" {
		return DefaultBranch
	}
	return s
}
