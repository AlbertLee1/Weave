package where

import "errors"

// ErrInvalidWhereClause is the sentinel that wraps every user-side
// where-clause parsing or validation failure (PRD-V2 wire-shape
// alignment, round 36). Handlers use errors.Is to distinguish "the
// caller sent us a bad where clause" (HTTP 400 INVALID_ARGUMENT)
// from a downstream Bleve / Postgres failure (HTTP 500 INTERNAL).
//
// Before this sentinel existed, both error classes funneled through
// `apierror.NewInvalidParameter("SearchObjectsFailed", …)` (HTTP
// 400) — round 26's wire-shape audit deferred the fix because the
// converter intermixed user-input and server-side errors at a
// single return site. Rounds 30-35 worked on Gap-A4; round 36
// returns to the wire-shape series with this sentinel.
//
// Callers MUST wrap with `%w` so the underlying message survives
// the chain — handlers surface the inner message via the API error
// `parameters.reason` field so SDK clients can render the exact
// reason (e.g. "regex pattern invalid: ...").
var ErrInvalidWhereClause = errors.New("invalid where clause")
