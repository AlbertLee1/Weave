package functions

import (
	"context"

	"github.com/dop251/goja"
)

// ProgressReporter is the narrow callback the `weave.reportProgress(percent,
// msg)` JS shim forwards user-land progress updates to. US-241.
//
// Kept as a minimal (ctx, percent, message) signature so concrete wiring —
// action_jobs table row updates, NATS pub/sub events, structured log lines —
// can live in the caller's package without pkg/functions taking a dependency
// on pkg/actions / pkg/funnel / pkg/oms. Mirrors the FunctionCaller shape.
type ProgressReporter interface {
	Report(ctx context.Context, percent int, message string)
}

// progressReporterCtxKey is an unexported context key; external callers go
// through WithProgressReporter / ProgressReporterFromContext.
type progressReporterCtxKey struct{}

// WithProgressReporter returns a context that carries reporter for later
// retrieval by the Goja runtime shim. A nil reporter is treated as "clear" —
// useful for child goroutines that should not inherit progress side effects.
func WithProgressReporter(ctx context.Context, reporter ProgressReporter) context.Context {
	return context.WithValue(ctx, progressReporterCtxKey{}, reporter)
}

// ProgressReporterFromContext extracts the reporter carried on ctx. Returns
// nil when absent so callers can test-and-skip without a dedicated sentinel.
func ProgressReporterFromContext(ctx context.Context) ProgressReporter {
	if ctx == nil {
		return nil
	}
	v := ctx.Value(progressReporterCtxKey{})
	if v == nil {
		return nil
	}
	r, _ := v.(ProgressReporter)
	return r
}

// registerProgressShim adds `reportProgress(percent, message?)` to the given
// weave object. Always registered — when no reporter is on ctx the call is a
// no-op so scripts can use it unconditionally without branching on deployment
// mode (sync vs async).
//
// Percent is clamped to [0,100] before the reporter sees it. JS numeric args
// are truncated (not rounded) so 75.7 → 75 — matches Go's int conversion.
// Message is optional and defaults to "" when omitted or undefined.
func registerProgressShim(vm *goja.Runtime, weave *goja.Object, ctx context.Context) {
	weave.Set("reportProgress", func(call goja.FunctionCall) goja.Value {
		reporter := ProgressReporterFromContext(ctx)
		if reporter == nil {
			return goja.Undefined()
		}
		percent := 0
		if arg := call.Argument(0); arg != nil && !goja.IsUndefined(arg) && !goja.IsNull(arg) {
			percent = int(arg.ToInteger())
		}
		if percent < 0 {
			percent = 0
		}
		if percent > 100 {
			percent = 100
		}
		message := ""
		if arg := call.Argument(1); arg != nil && !goja.IsUndefined(arg) && !goja.IsNull(arg) {
			message = arg.String()
		}
		reporter.Report(ctx, percent, message)
		return goja.Undefined()
	})
}
