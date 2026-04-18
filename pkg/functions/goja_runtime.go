package functions

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/dop251/goja"
	"github.com/liyang/weave/pkg/functions/fnerrors"
)

// Config holds configuration for the Goja runtime sandbox.
type Config struct {
	MaxExecutionTime time.Duration
	MaxMemoryBytes   int64
}

// DefaultConfig returns the production defaults required by US-218: a 5s
// CPU execution budget and a 128MB heap ceiling. Both limits are enforced
// inside Execute — the context deadline drives goja's Interrupt watchdog,
// and a companion goroutine polls runtime.MemStats.HeapAlloc against a
// baseline snapshot captured before execution.
func DefaultConfig() Config {
	return Config{
		MaxExecutionTime: 5 * time.Second,
		MaxMemoryBytes:   128 * 1024 * 1024,
	}
}

// interruptReason is embedded in vm.Interrupt so Execute can distinguish a
// timeout trigger (→ fnerrors.ErrTimeout / HTTP 408) from a memory-limit
// trigger (→ fnerrors.ErrMemoryLimit / HTTP 429). The value is surfaced
// via goja's *InterruptedError.Value() on the returned error.
const (
	interruptReasonTimeout = "function.timeout"
	interruptReasonMemory  = "function.memory"
)

// maxCallStackSize caps recursion depth to prevent stack overflow.
const maxCallStackSize = 1024

// memCheckInterval is how often the memory watchdog polls heap usage.
const memCheckInterval = 50 * time.Millisecond

// Runtime is a sandboxed JavaScript execution environment backed by Goja.
type Runtime struct {
	config         Config
	ontologyClient OntologyClient
}

// NewRuntime creates a new sandboxed Goja runtime with the given config.
func NewRuntime(cfg Config) *Runtime {
	return &Runtime{config: cfg}
}

// Execute runs the given JavaScript source in a sandboxed Goja VM.
// The source must define a `function main(input)` that returns a value.
// The input parameter is passed as the first argument to main.
func (r *Runtime) Execute(ctx context.Context, source string, input interface{}) (interface{}, error) {
	// Apply execution timeout via context
	execCtx, cancel := context.WithTimeout(ctx, r.config.MaxExecutionTime)
	defer cancel()

	vm := goja.New()

	// Sandbox: limit call stack depth to prevent runaway recursion.
	vm.SetMaxCallStackSize(maxCallStackSize)

	// Sandbox: explicitly remove dangerous globals.
	dangerousGlobals := []string{
		"require", "fetch", "fs", "net", "process",
		"child_process", "os", "setTimeout", "setInterval",
		"setImmediate", "clearTimeout", "clearInterval", "clearImmediate",
	}
	for _, name := range dangerousGlobals {
		_ = vm.Set(name, goja.Undefined())
	}
	for _, name := range dangerousGlobals {
		vm.GlobalObject().Delete(name)
	}

	// Set up context-based timeout and memory watchdog.
	var interrupted atomic.Bool
	done := make(chan struct{})
	defer close(done)

	// Capture baseline heap allocation before execution.
	var baseline runtime.MemStats
	runtime.ReadMemStats(&baseline)

	go func() {
		ticker := time.NewTicker(memCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-execCtx.Done():
				if interrupted.CompareAndSwap(false, true) {
					vm.Interrupt(interruptReasonTimeout)
				}
				return
			case <-done:
				return
			case <-ticker.C:
				// Periodic GC + memory check
				runtime.GC()
				var current runtime.MemStats
				runtime.ReadMemStats(&current)
				if current.HeapAlloc > baseline.HeapAlloc+uint64(r.config.MaxMemoryBytes) {
					if interrupted.CompareAndSwap(false, true) {
						vm.Interrupt(interruptReasonMemory)
					}
					return
				}
			}
		}
	}()

	// Register ontology shim if client is configured.
	r.registerOntologyShim(vm, execCtx)

	// Compile and run the source
	_, err := vm.RunString(source)
	if err != nil {
		return nil, wrapGojaError(err)
	}

	// Get the main function
	mainFn, ok := goja.AssertFunction(vm.Get("main"))
	if !ok {
		return nil, fmt.Errorf("source must define a 'main' function")
	}

	// Call main(input)
	var arg goja.Value
	if input != nil {
		arg = vm.ToValue(input)
	} else {
		arg = goja.Null()
	}

	result, err := mainFn(goja.Undefined(), arg)
	if err != nil {
		return nil, wrapGojaError(err)
	}

	return result.Export(), nil
}

// wrapGojaError translates a goja-originated error into the fnerrors
// sentinel when the runtime was interrupted by the quota watchdog.
// Callers use errors.Is(err, fnerrors.ErrTimeout) / ErrMemoryLimit to map
// the condition to the appropriate HTTP status (408 / 429). Non-interrupt
// errors (syntax, thrown exceptions, ...) pass through unchanged.
func wrapGojaError(err error) error {
	if err == nil {
		return nil
	}
	var ie *goja.InterruptedError
	if errors.As(err, &ie) {
		switch ie.Value() {
		case interruptReasonTimeout:
			return fmt.Errorf("%w: %s", fnerrors.ErrTimeout, err.Error())
		case interruptReasonMemory:
			return fmt.Errorf("%w: %s", fnerrors.ErrMemoryLimit, err.Error())
		}
	}
	return fmt.Errorf("execution error: %w", err)
}
