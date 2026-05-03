package oms

import "context"

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
