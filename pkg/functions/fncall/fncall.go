// Package fncall declares the context-propagated call-stack primitives and
// sentinel errors used by weave.callFunction (US-220). Lives in a leaf
// subpackage with ZERO imports from the rest of the tree so upstream layers
// (pkg/functions runtime, pkg/oms HTTP handlers, future test harnesses) can
// share the same depth/cycle invariants without risking an import cycle via
// pkg/oss → pkg/oms.
package fncall

import (
	"context"
	"errors"
)

// MaxDepth is the ceiling on nested weave.callFunction invocations. Exceeding
// this throws a JS error that callers can observe via try/catch — see
// ErrDepthExceeded for the Go-side sentinel.
const MaxDepth = 8

// ErrDepthExceeded is surfaced when a weave.callFunction invocation would
// grow the call stack past MaxDepth.
var ErrDepthExceeded = errors.New("function call depth exceeded")

// ErrCycleDetected is surfaced when a weave.callFunction invocation would
// re-enter a function already on the stack (A→B→A).
var ErrCycleDetected = errors.New("function call cycle detected")

type stackKey struct{}

// WithStack returns a child context carrying the given call stack. Each
// entry is a function reference (RID, name, or name@version) currently
// in-flight.
func WithStack(ctx context.Context, stack []string) context.Context {
	return context.WithValue(ctx, stackKey{}, stack)
}

// StackFromContext reads the call stack carried by ctx. Returns nil when
// ctx has no stack attached (top-level invocation with no seeded frame).
func StackFromContext(ctx context.Context) []string {
	v, _ := ctx.Value(stackKey{}).([]string)
	return v
}

// WithFrame returns a child context with ref appended to the call stack.
// The parent's slice is never mutated — each push allocates a fresh slice
// so unrelated branches of the context tree stay isolated.
func WithFrame(ctx context.Context, ref string) context.Context {
	stack := StackFromContext(ctx)
	next := make([]string, 0, len(stack)+1)
	next = append(next, stack...)
	next = append(next, ref)
	return WithStack(ctx, next)
}
