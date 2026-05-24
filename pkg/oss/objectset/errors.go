package objectset

import "errors"

// ErrInvalidObjectSetDefinition is the round-37 wire-shape sentinel
// that wraps user-side ObjectSet definition errors — definition shape
// problems caught by Definition.Validate(), unsupported `methodInput`
// type, and the catch-all "unknown objectSet type" fallback. Handlers
// use errors.Is to distinguish "the caller sent a malformed
// definition" (HTTP 400 INVALID_ARGUMENT) from a downstream Bleve /
// PG failure inside one of the executeBase / executeFilter /
// executeSearchAround / ... branches (HTTP 500 INTERNAL).
//
// Before this sentinel existed, the handler funneled BOTH error
// classes through `NewInvalidParameter("ObjectSetFailed", …)`
// returning HTTP 400. Round 26's audit deferred the fix because
// the executor mixed user-input and server-side errors at every
// return site.
//
// Companion to where.ErrInvalidWhereClause from round 36 — the
// executor's where conversion already carries that sentinel through
// `%w` chains, so the handler check sees both as user-side.
var ErrInvalidObjectSetDefinition = errors.New("invalid objectSet definition")
