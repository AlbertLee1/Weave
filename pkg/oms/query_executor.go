package oms

import "context"

// QueryExecutor dispatches query execution to a backing function (Goja or HTTP)
// and returns the raw result. Implementations live in pkg/queryexec to avoid
// import cycles (pkg/functions transitively imports pkg/oms).
type QueryExecutor interface {
	Execute(ctx context.Context, qt *QueryType, params map[string]interface{}) (interface{}, error)
}
