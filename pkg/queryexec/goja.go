package queryexec

import (
	"context"
	"fmt"

	"github.com/liyang/weave/pkg/functions"
	"github.com/liyang/weave/pkg/oms"
)

// FunctionLookup is a narrow interface for fetching function source code by RID.
type FunctionLookup interface {
	GetFunction(ctx context.Context, rid string) (*oms.Function, error)
}

// GojaQueryExecutor executes query functions via the embedded Goja JavaScript
// runtime. It looks up the function source by the QueryType's FunctionRID and
// passes the query parameters as the JS main(input) argument.
type GojaQueryExecutor struct {
	runtime *functions.Runtime
	lookup  FunctionLookup
}

// NewGojaQueryExecutor creates a GojaQueryExecutor wired to the given runtime
// and function lookup.
func NewGojaQueryExecutor(rt *functions.Runtime, lookup FunctionLookup) *GojaQueryExecutor {
	return &GojaQueryExecutor{runtime: rt, lookup: lookup}
}

// Execute looks up the function source, executes it via Goja, and returns the
// raw result. The JS function receives {queryTypeRid, queryTypeApiName,
// functionRid, parameters} as input.
func (e *GojaQueryExecutor) Execute(ctx context.Context, qt *oms.QueryType, params map[string]interface{}) (interface{}, error) {
	fn, err := e.lookup.GetFunction(ctx, qt.FunctionRID)
	if err != nil {
		return nil, fmt.Errorf("query executor: lookup function %q: %w", qt.FunctionRID, err)
	}

	input := map[string]interface{}{
		"queryTypeRid":     qt.RID,
		"queryTypeApiName": qt.APIName,
		"functionRid":      qt.FunctionRID,
		"parameters":       params,
	}

	result, err := e.runtime.Execute(ctx, fn.SourceCode, input)
	if err != nil {
		return nil, fmt.Errorf("query executor: execute function %q: %w", qt.FunctionRID, err)
	}

	return result, nil
}
