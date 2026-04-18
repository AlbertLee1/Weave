package oms

import "context"

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
