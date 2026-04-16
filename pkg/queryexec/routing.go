package queryexec

import (
	"context"
	"strings"

	"github.com/liyang/weave/pkg/functions"
	"github.com/liyang/weave/pkg/oms"
)

// RoutingQueryExecutor routes query execution based on the FunctionRID prefix:
//   - http:// or https:// -> HTTPQueryExecutor
//   - anything else (ri.* RIDs) -> GojaQueryExecutor
type RoutingQueryExecutor struct {
	goja *GojaQueryExecutor
	http *HTTPQueryExecutor
}

// NewRoutingQueryExecutor creates a RoutingQueryExecutor with both Goja and
// HTTP sub-executors.
func NewRoutingQueryExecutor(rt *functions.Runtime, lookup FunctionLookup) *RoutingQueryExecutor {
	return &RoutingQueryExecutor{
		goja: NewGojaQueryExecutor(rt, lookup),
		http: NewHTTPQueryExecutor(),
	}
}

// Execute routes to the appropriate sub-executor based on the FunctionRID prefix.
func (e *RoutingQueryExecutor) Execute(ctx context.Context, qt *oms.QueryType, params map[string]interface{}) (interface{}, error) {
	rid := qt.FunctionRID
	if strings.HasPrefix(rid, "http://") || strings.HasPrefix(rid, "https://") {
		return e.http.Execute(ctx, qt, params)
	}
	return e.goja.Execute(ctx, qt, params)
}
