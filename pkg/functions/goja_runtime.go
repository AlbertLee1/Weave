package functions

import (
	"context"
	"fmt"
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
	vm := goja.New()

	// Sandbox: explicitly remove dangerous globals.
	// Goja doesn't provide these by default, but we delete them defensively
	// in case any future goja version or user code defines them.
	dangerousGlobals := []string{
		"require", "fetch", "fs", "net", "process",
		"child_process", "os", "setTimeout", "setInterval",
		"setImmediate", "clearTimeout", "clearInterval", "clearImmediate",
	}
	for _, name := range dangerousGlobals {
		_ = vm.Set(name, goja.Undefined())
	}
	// Now delete them so typeof returns "undefined"
	for _, name := range dangerousGlobals {
		vm.GlobalObject().Delete(name)
	}

	// Set up context-based timeout
	done := make(chan struct{})
	defer close(done)

	go func() {
		select {
		case <-ctx.Done():
			vm.Interrupt("execution cancelled: " + ctx.Err().Error())
		case <-time.After(r.config.MaxExecutionTime):
			vm.Interrupt("execution timeout exceeded")
		case <-done:
			// execution completed normally
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
