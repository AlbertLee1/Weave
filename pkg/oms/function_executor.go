package oms

import (
	"context"
	"time"
)

// FunctionExecutor dispatches a Function call (Goja or HTTP) and returns the
// raw result. The handler at POST /functions/{rid}/execute validates and
// coerces the caller's parameters BEFORE invoking Execute, so implementations
// can trust that `params` already satisfies the Function's declared signature.
//
// Adapters live in pkg/queryexec / cmd/server (the same place QueryExecutor
// is wired) so pkg/oms stays free of the goja runtime + http client deps.
type FunctionExecutor interface {
	Execute(ctx context.Context, fn *Function, params map[string]interface{}) (interface{}, error)
}

// FunctionQuotaLimiter gates Function execution against a per-realm
// per-minute quota (US-218). Allow returns false when the caller's
// realm has exhausted its budget within the rolling window — the handler
// then surfaces HTTP 429.
//
// The interface is intentionally narrow (one method) so the concrete
// implementation lives in pkg/functions/quota without pkg/oms taking on
// the full quota package as a dependency.
type FunctionQuotaLimiter interface {
	Allow(key string) bool
}

// DefaultFunctionExecutionTimeout is the per-call CPU budget the
// ExecuteFunction handler enforces via context.WithTimeout. The underlying
// Goja runtime also honours this limit (see pkg/functions.DefaultConfig),
// but the handler-side deadline guarantees the 5s ceiling even when an
// alternative FunctionExecutor is wired that does not consult its own
// runtime config.
const DefaultFunctionExecutionTimeout = 5 * time.Second
