// Package formula evaluates a Goja JavaScript expression against a loaded
// object's property map and returns the resulting value.
//
// US-200 Derived Properties framework:
//   - 100ms hard timeout (VM interrupt from a goroutine watching ctx)
//   - No host I/O: require/fetch/fs/net/process/setTimeout etc. are stripped
//   - The object's properties are exposed read-only via a DynamicObject on
//     `this` and the `$` global, so `this.firstName` or `$.firstName` both
//     work. Writes throw a TypeError.
//
// A formula source can be either a bare expression (e.g.
// `this.firstName + " " + this.lastName`) or a statement-bodied script that
// calls `return`. The evaluator wraps each call in a fresh IIFE so authors
// never have to declare `function main` themselves.
package formula

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dop251/goja"
)

// DefaultTimeout is the hard execution budget for a single formula call.
// Formulas are evaluated per-object on hot query paths, so the cap is
// intentionally tight — anything slower belongs in a Function or a
// background aggregation (see US-202 Computed Properties).
const DefaultTimeout = 100 * time.Millisecond

// dangerousGlobals is the deny-list of host APIs that must not be reachable
// from a formula. Matches the pkg/functions runtime set plus anything that
// could enable side effects from an expression.
var dangerousGlobals = []string{
	"require", "fetch", "fs", "net", "process",
	"child_process", "os",
	"setTimeout", "setInterval", "setImmediate",
	"clearTimeout", "clearInterval", "clearImmediate",
	"XMLHttpRequest", "WebSocket",
}

// ErrTimeout is returned when the VM is interrupted because the formula
// exceeded its execution budget.
var ErrTimeout = errors.New("formula: execution timeout")

// ErrCompile wraps JS syntax / compilation errors.
var ErrCompile = errors.New("formula: compile error")

// ErrRuntime wraps runtime exceptions thrown while evaluating a formula.
var ErrRuntime = errors.New("formula: runtime error")

// Evaluator compiles and evaluates a single formula source. It is safe to
// reuse across goroutines because each Evaluate call spins up a fresh VM;
// the struct itself is immutable after construction.
type Evaluator struct {
	source  string
	program *goja.Program
	timeout time.Duration
}

// New compiles the given JS source with the default 100ms timeout.
func New(source string) (*Evaluator, error) {
	return NewWithTimeout(source, DefaultTimeout)
}

// NewWithTimeout is New with a custom per-call timeout. Used by tests to
// exercise the interrupt path without waiting 100ms.
func NewWithTimeout(source string, timeout time.Duration) (*Evaluator, error) {
	trimmed := strings.TrimSpace(source)
	if trimmed == "" {
		return nil, fmt.Errorf("%w: source is empty", ErrCompile)
	}
	body := wrapSource(trimmed)
	prog, err := goja.Compile("<formula>", body, true)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCompile, err)
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Evaluator{source: source, program: prog, timeout: timeout}, nil
}

// wrapSource turns the user's source into an IIFE named `__formula`. If the
// source already contains `return`, we treat it as a statement body;
// otherwise it's a bare expression we return directly.
func wrapSource(src string) string {
	if strings.Contains(src, "return") {
		return "function __formula(self){\n" + src + "\n}"
	}
	return "function __formula(self){ return (" + src + "); }"
}

// Evaluate runs the compiled formula with `obj` bound to `this` and the
// first argument `self`. The returned value is a plain Go value (string,
// int64/float64, bool, []interface{}, map[string]interface{}, or nil),
// exported from the VM.
//
// The caller's ctx is honored: if it fires before the timeout the VM is
// interrupted with ErrTimeout. Cancellation from ctx after Evaluate
// returns is a no-op.
func (e *Evaluator) Evaluate(ctx context.Context, obj map[string]interface{}) (interface{}, error) {
	execCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	vm := goja.New()
	vm.SetMaxCallStackSize(256)

	// Strip host APIs. Set to undefined first, then Delete — mirrors the
	// two-step approach used by pkg/functions so later Object.keys() walks
	// of the global object don't expose the names.
	for _, name := range dangerousGlobals {
		_ = vm.Set(name, goja.Undefined())
	}
	for _, name := range dangerousGlobals {
		vm.GlobalObject().Delete(name)
	}

	view := vm.NewDynamicObject(&readOnlyView{vm: vm, data: obj})
	if err := vm.Set("$", view); err != nil {
		return nil, fmt.Errorf("%w: bind $: %v", ErrRuntime, err)
	}

	var interrupted atomic.Bool
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-execCtx.Done():
			if interrupted.CompareAndSwap(false, true) {
				vm.Interrupt(ErrTimeout)
			}
		case <-done:
		}
	}()

	if _, err := vm.RunProgram(e.program); err != nil {
		return nil, classifyRuntimeError(err, &interrupted)
	}

	fn, ok := goja.AssertFunction(vm.Get("__formula"))
	if !ok {
		return nil, fmt.Errorf("%w: compiled formula missing entry point", ErrRuntime)
	}

	// Bind `this` and the first arg to the same read-only view so authors
	// can write either `this.firstName` or `self.firstName`.
	result, err := fn(view, view)
	if err != nil {
		return nil, classifyRuntimeError(err, &interrupted)
	}
	return exportResult(result), nil
}

// classifyRuntimeError maps a goja error to ErrTimeout when it was caused
// by an interrupt from our watchdog, otherwise wraps it as ErrRuntime.
func classifyRuntimeError(err error, interrupted *atomic.Bool) error {
	if interrupted != nil && interrupted.Load() {
		return ErrTimeout
	}
	var iv *goja.InterruptedError
	if errors.As(err, &iv) {
		return ErrTimeout
	}
	return fmt.Errorf("%w: %v", ErrRuntime, err)
}

// exportResult normalizes a goja.Value to a plain Go value.
func exportResult(v goja.Value) interface{} {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return nil
	}
	return v.Export()
}

// readOnlyView exposes `data` through goja.DynamicObject so every field
// read is satisfied by the underlying map and every write throws. It must
// be bound to the VM that created it so the returned goja.Values belong
// to the correct runtime.
type readOnlyView struct {
	vm   *goja.Runtime
	data map[string]interface{}
}

func (r *readOnlyView) Get(key string) goja.Value {
	if r.data == nil {
		return goja.Undefined()
	}
	v, ok := r.data[key]
	if !ok {
		return goja.Undefined()
	}
	return r.vm.ToValue(v)
}

func (r *readOnlyView) Set(key string, _ goja.Value) bool {
	panic(r.vm.NewTypeError("formula: object properties are read-only (attempted to set %q)", key))
}

func (r *readOnlyView) Has(key string) bool {
	if r.data == nil {
		return false
	}
	_, ok := r.data[key]
	return ok
}

func (r *readOnlyView) Delete(key string) bool {
	panic(r.vm.NewTypeError("formula: object properties are read-only (attempted to delete %q)", key))
}

func (r *readOnlyView) Keys() []string {
	if len(r.data) == 0 {
		return nil
	}
	out := make([]string, 0, len(r.data))
	for k := range r.data {
		out = append(out, k)
	}
	return out
}
