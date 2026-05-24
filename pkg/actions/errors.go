package actions

import "errors"

// ErrActionTypeNotFound is the round-38 wire-shape sentinel for the
// case where the caller's URL/body references an action type the
// ontology doesn't define. The handler routes errors.Is matches to
// HTTP 404 ActionTypeNotFound — Foundry's wire contract is that
// resources that don't exist surface as 404 NOT_FOUND, not 400
// INVALID_ARGUMENT (which was the historical lump).
//
// Before this sentinel existed, both "unknown action type" AND
// genuine downstream failures funneled through `NewInvalidParameter(
// "ActionFailed", …)` returning HTTP 400. SDK clients had no way to
// distinguish "you asked for an action that doesn't exist" from
// "the server failed mid-action-execution".
var ErrActionTypeNotFound = errors.New("action type not found")

// ErrInvalidActionParameters is the round-38 sentinel for user-side
// parameter parse + validation errors at the start of executor.Apply
// — before any rule runs, before any DB write, before any NATS
// publish. Wraps `parse params` JSON unmarshal failures and
// `ValidateParameters` shape/value failures. The handler routes
// errors.Is matches to HTTP 400 InvalidActionParameters so SDK
// clients see "fix your parameters" instead of "retry / oncall".
//
// Errors AFTER parameter validation (rule execution, function
// dispatch, NATS publish, etc.) fall through to the default 500
// ActionFailed envelope — those are server-side or schema-author
// issues operators need to investigate, not caller bugs.
//
// The existing `typedAPIError` extraction in handlers.go still wins
// over this sentinel because ParameterSchemaError, ValueType
// constraint enforcement (WEAVE_VALIDATION_ENUM), and friends carry
// richer field-level detail; this sentinel covers the plain
// fmt.Errorf path from ValidateParameters.
var ErrInvalidActionParameters = errors.New("invalid action parameters")
