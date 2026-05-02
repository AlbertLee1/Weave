package functions

import (
	"context"
	"fmt"

	"github.com/dop251/goja"
	"github.com/liyang/weave/pkg/functions/fncall"
)

// FunctionCaller dispatches an in-process function call by reference
// (RID, name, or name@version — resolution is the implementation's
// concern). The Goja shim invokes this when user JS calls
// weave.callFunction(ref, params).
//
// The interface is intentionally narrow so concrete wiring can live in
// cmd/server without pkg/functions taking on pkg/oms as a dependency.
// Implementations MUST honour the incoming ctx — in particular the call
// stack carried via fncall — and propagate it to any nested execution so
// depth + cycle invariants hold across call boundaries.
type FunctionCaller interface {
	CallFunction(ctx context.Context, ref string, params map[string]interface{}) (interface{}, error)
}

// SetFunctionCaller configures the host binding for weave.callFunction.
// When set, Execute will register a global `weave` object exposing
// callFunction(ref, params). When nil (default), the weave global stays
// absent — existing scripts that don't touch it remain unaffected.
func (r *Runtime) SetFunctionCaller(caller FunctionCaller) {
	r.functionCaller = caller
}

// registerWeaveShim registers the global `weave` object on the VM. Methods:
//
//   - weave.callFunction(ref, params) — recursively invoke another function
//     via the configured FunctionCaller (US-220). Only registered when a
//     FunctionCaller is attached to the runtime.
//   - weave.reportProgress(percent, message?) — surface a progress update
//     to the ProgressReporter carried on ctx (US-241). Always registered;
//     no-ops when no reporter is on ctx so scripts can use it unconditionally
//     across sync + async dispatch modes.
//
// Guardrails (US-220) for callFunction:
//   - Depth: rejects when the stack carried by ctx already holds fncall.MaxDepth
//     frames. The 9th nested call is where the limit kicks in.
//   - Cycle: rejects when ref already appears on the stack. Pair with
//     fncall.WithFrame(ctx, topLevelRef) before calling Execute so A→B→A
//     is flagged on the first re-entry.
func (r *Runtime) registerWeaveShim(vm *goja.Runtime, ctx context.Context) {
	weave := vm.NewObject()
	registerProgressShim(vm, weave, ctx)

	if r.functionCaller == nil {
		_ = vm.Set("weave", weave)
		return
	}

	weave.Set("callFunction", func(call goja.FunctionCall) goja.Value {
		refVal := call.Argument(0)
		if refVal == nil || goja.IsUndefined(refVal) || goja.IsNull(refVal) {
			panic(vm.NewGoError(fmt.Errorf("weave.callFunction: ref is required")))
		}
		ref := refVal.String()
		if ref == "" {
			panic(vm.NewGoError(fmt.Errorf("weave.callFunction: ref must be a non-empty string")))
		}

		var params map[string]interface{}
		if paramsArg := call.Argument(1); paramsArg != nil && !goja.IsUndefined(paramsArg) && !goja.IsNull(paramsArg) {
			switch exported := paramsArg.Export().(type) {
			case map[string]interface{}:
				params = exported
			case nil:
				// treated as omitted
			default:
				panic(vm.NewGoError(fmt.Errorf("weave.callFunction: params must be an object")))
			}
		}
		if params == nil {
			params = map[string]interface{}{}
		}

		stack := fncall.StackFromContext(ctx)
		if len(stack) >= fncall.MaxDepth {
			panic(vm.NewGoError(fmt.Errorf("FUNCTION_RECURSION_DEPTH_EXCEEDED: %w: depth=%d limit=%d ref=%s",
				fncall.ErrDepthExceeded, len(stack)+1, fncall.MaxDepth, ref)))
		}
		for _, entry := range stack {
			if entry == ref {
				panic(vm.NewGoError(fmt.Errorf("FUNCTION_CALL_CYCLE: %w: %s is already in stack %v",
					fncall.ErrCycleDetected, ref, stack)))
			}
		}

		childCtx := fncall.WithFrame(ctx, ref)

		result, err := r.functionCaller.CallFunction(childCtx, ref, params)
		if err != nil {
			panic(vm.NewGoError(err))
		}
		return vm.ToValue(result)
	})

	_ = vm.Set("weave", weave)
}
