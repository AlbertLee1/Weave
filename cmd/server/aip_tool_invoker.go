package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/liyang/weave/pkg/aip"
	"github.com/liyang/weave/pkg/oms"
)

// aipFunctionInvoker bridges aip.FunctionInvoker (the narrow callback the
// SendMessage tool loop uses) onto the existing oms.Repository +
// oms.FunctionExecutor pair. Lives in cmd/server/ so pkg/aip stays free
// of any oms / functions dep — same dep-direction trick as
// pgEdgePropertiesResolver / interface_method_dispatcher.
//
// Either field may be nil: a nil repo or executor produces a clean
// ErrToolHandlerNotConfigured error that surfaces as
// AIPToolHandlerNotConfigured at the SendMessage handler boundary so
// operators notice the missing wiring instead of silently skipping the
// tool call.
type aipFunctionInvoker struct {
	repo     oms.Repository
	executor oms.FunctionExecutor
}

func newAIPFunctionInvoker(repo oms.Repository, executor oms.FunctionExecutor) *aipFunctionInvoker {
	return &aipFunctionInvoker{repo: repo, executor: executor}
}

// Invoke resolves rid through the repository and dispatches to the
// FunctionExecutor. The catalog row already vouches for the FunctionRID
// pointing at a real Function entry, but we re-validate here so a stale
// catalogue row (Function deleted after catalog write) surfaces a clean
// error instead of an opaque executor crash.
func (a *aipFunctionInvoker) Invoke(ctx context.Context, rid string, params map[string]interface{}) (interface{}, error) {
	if a == nil || a.repo == nil || a.executor == nil {
		return nil, aip.ErrToolHandlerNotConfigured
	}
	fn, err := a.repo.GetFunction(ctx, rid)
	if err != nil {
		// Distinguish "function deleted" from a transient PG error so
		// the SendMessage handler can map the former to a tool error.
		if errors.Is(err, oms.ErrNotFound) {
			return nil, fmt.Errorf("aip: tool handler function %s not found: %w", rid, err)
		}
		return nil, fmt.Errorf("aip: tool handler lookup failed: %w", err)
	}
	if fn == nil {
		return nil, fmt.Errorf("aip: tool handler function %s not found", rid)
	}
	return a.executor.Execute(ctx, fn, params)
}
