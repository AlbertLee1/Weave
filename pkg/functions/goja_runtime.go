package functions

import (
	"context"
	"fmt"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/dop251/goja"
)

// Config holds configuration for the Goja runtime sandbox.
type Config struct {
	MaxExecutionTime time.Duration
	MaxMemoryBytes   int64
}

// DefaultConfig returns sensible defaults: 5s timeout, 32MB memory.
func DefaultConfig() Config {
	return Config{
		MaxExecutionTime: 5 * time.Second,
		MaxMemoryBytes:   32 * 1024 * 1024,
	}
}

// maxCallStackSize caps recursion depth to prevent stack overflow.
const maxCallStackSize = 1024

// memCheckInterval is how often the memory watchdog polls heap usage.
const memCheckInterval = 50 * time.Millisecond

// Runtime is a sandboxed JavaScript execution environment backed by Goja.
type Runtime struct {
	config Config
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
					vm.Interrupt("execution timeout exceeded")
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
						vm.Interrupt("memory limit exceeded")
					}
					return
				}
			}
		}
	}()

	// Compile and run the source
	_, err := vm.RunString(source)
	if err != nil {
		return nil, fmt.Errorf("compile error: %w", err)
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
		return nil, fmt.Errorf("execution error: %w", err)
	}

	return result.Export(), nil
}
