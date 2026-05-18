package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/liyang/weave/pkg/functions"
	"github.com/liyang/weave/pkg/oms"
)

type gojaFunctionExecutor struct {
	config functions.Config
}

func newGojaFunctionExecutor() oms.FunctionExecutor {
	return &gojaFunctionExecutor{config: functions.DefaultConfig()}
}

func (g *gojaFunctionExecutor) Execute(ctx context.Context, fn *oms.Function, params map[string]interface{}) (interface{}, error) {
	if fn == nil {
		return nil, errors.New("function executor: nil function")
	}
	if params == nil {
		params = map[string]interface{}{}
	}

	switch fn.NormalisedRuntime() {
	case oms.FunctionRuntimeGoja:
		rt := functions.NewRuntime(g.config)
		return rt.Execute(ctx, fn.SourceCode, map[string]interface{}{
			"parameters": params,
		})
	case oms.FunctionRuntimeHTTP:
		return nil, fmt.Errorf("function executor: runtime %q is not wired", fn.NormalisedRuntime())
	default:
		return nil, fmt.Errorf("function executor: unsupported runtime %q", fn.NormalisedRuntime())
	}
}
