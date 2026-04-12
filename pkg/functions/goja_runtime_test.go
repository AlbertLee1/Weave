package functions

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestGojaRuntime_HelloWorld(t *testing.T) {
	rt := NewRuntime(DefaultConfig())
	result, err := rt.Execute(context.Background(), `
		function main(input) {
			return "Hello, World!";
		}
	`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s, ok := result.(string)
	if !ok {
		t.Fatalf("expected string, got %T", result)
	}
	if s != "Hello, World!" {
		t.Fatalf("expected 'Hello, World!', got %q", s)
	}
}

func TestGojaRuntime_WithInput(t *testing.T) {
	rt := NewRuntime(DefaultConfig())
	result, err := rt.Execute(context.Background(), `
		function main(input) {
			return input.a + input.b;
		}
	`, map[string]interface{}{"a": 3.0, "b": 4.0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Goja may return int64 or float64 depending on the arithmetic result
	switch v := result.(type) {
	case int64:
		if v != 7 {
			t.Fatalf("expected 7, got %v", v)
		}
	case float64:
		if v != 7.0 {
			t.Fatalf("expected 7, got %v", v)
		}
	default:
		t.Fatalf("expected numeric, got %T (%v)", result, result)
	}
}

func TestGojaSandboxDisabled(t *testing.T) {
	rt := NewRuntime(DefaultConfig())

	tests := []struct {
		name   string
		source string
	}{
		{
			name: "fetch is undefined",
			source: `function main(input) {
				if (typeof fetch !== "undefined") {
					throw new Error("fetch should be undefined");
				}
				return "ok";
			}`,
		},
		{
			name: "require is undefined",
			source: `function main(input) {
				if (typeof require !== "undefined") {
					throw new Error("require should be undefined");
				}
				return "ok";
			}`,
		},
		{
			name: "process is undefined",
			source: `function main(input) {
				if (typeof process !== "undefined") {
					throw new Error("process should be undefined");
				}
				return "ok";
			}`,
		},
		{
			name: "fs module not available",
			source: `function main(input) {
				try {
					var fs = require("fs");
					throw new Error("fs should not be available");
				} catch(e) {
					if (e.message === "fs should not be available") throw e;
					return "ok";
				}
			}`,
		},
		{
			name: "child_process not available",
			source: `function main(input) {
				if (typeof child_process !== "undefined") {
					throw new Error("child_process should be undefined");
				}
				return "ok";
			}`,
		},
		{
			name: "os not available",
			source: `function main(input) {
				if (typeof os !== "undefined") {
					throw new Error("os should be undefined");
				}
				return "ok";
			}`,
		},
		{
			name: "net not available",
			source: `function main(input) {
				if (typeof net !== "undefined") {
					throw new Error("net should be undefined");
				}
				return "ok";
			}`,
		},
		{
			name: "setTimeout not available",
			source: `function main(input) {
				if (typeof setTimeout !== "undefined") {
					throw new Error("setTimeout should be undefined");
				}
				return "ok";
			}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := rt.Execute(context.Background(), tc.source, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != "ok" {
				t.Fatalf("expected 'ok', got %v", result)
			}
		})
	}
}

func TestGojaSandbox_MathAndJSON(t *testing.T) {
	rt := NewRuntime(DefaultConfig())

	t.Run("Math is available", func(t *testing.T) {
		result, err := rt.Execute(context.Background(), `
			function main(input) {
				return Math.max(1, 2, 3);
			}
		`, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != int64(3) {
			t.Fatalf("expected 3, got %v (%T)", result, result)
		}
	})

	t.Run("JSON is available", func(t *testing.T) {
		result, err := rt.Execute(context.Background(), `
			function main(input) {
				var obj = {a: 1};
				return JSON.stringify(obj);
			}
		`, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != `{"a":1}` {
			t.Fatalf("expected '{\"a\":1}', got %v", result)
		}
	})
}

func TestGojaRuntime_NoMainFunction(t *testing.T) {
	rt := NewRuntime(DefaultConfig())
	_, err := rt.Execute(context.Background(), `
		var x = 42;
	`, nil)
	if err == nil {
		t.Fatal("expected error when main function is missing")
	}
}

func TestGojaRuntime_SyntaxError(t *testing.T) {
	rt := NewRuntime(DefaultConfig())
	_, err := rt.Execute(context.Background(), `
		function main(input) {{{
	`, nil)
	if err == nil {
		t.Fatal("expected error on syntax error")
	}
}

func TestGojaRuntime_MainThrows(t *testing.T) {
	rt := NewRuntime(DefaultConfig())
	_, err := rt.Execute(context.Background(), `
		function main(input) {
			throw new Error("intentional error");
		}
	`, nil)
	if err == nil {
		t.Fatal("expected error when main throws")
	}
}

func TestGojaTimeoutOOM(t *testing.T) {
	t.Run("timeout_infinite_loop", func(t *testing.T) {
		cfg := Config{
			MaxExecutionTime: 500 * time.Millisecond,
			MaxMemoryBytes:   32 * 1024 * 1024,
		}
		rt := NewRuntime(cfg)

		start := time.Now()
		_, err := rt.Execute(context.Background(), `
			function main(input) {
				while(true) {}
			}
		`, nil)
		elapsed := time.Since(start)

		if err == nil {
			t.Fatal("expected timeout error for infinite loop")
		}
		if !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "cancel") && !strings.Contains(err.Error(), "interrupt") {
			t.Fatalf("expected timeout-related error, got: %v", err)
		}
		// Should complete within a reasonable margin of the configured timeout
		if elapsed > 3*time.Second {
			t.Fatalf("timeout took too long: %v (expected ~500ms)", elapsed)
		}
	})

	t.Run("timeout_context_cancellation", func(t *testing.T) {
		cfg := Config{
			MaxExecutionTime: 10 * time.Second,
			MaxMemoryBytes:   32 * 1024 * 1024,
		}
		rt := NewRuntime(cfg)

		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		start := time.Now()
		_, err := rt.Execute(ctx, `
			function main(input) {
				while(true) {}
			}
		`, nil)
		elapsed := time.Since(start)

		if err == nil {
			t.Fatal("expected error when context is cancelled")
		}
		if elapsed > 3*time.Second {
			t.Fatalf("context cancellation took too long: %v (expected ~500ms)", elapsed)
		}
	})

	t.Run("oom_deep_recursion", func(t *testing.T) {
		cfg := Config{
			MaxExecutionTime: 10 * time.Second, // Long timeout so stack limit triggers first
			MaxMemoryBytes:   32 * 1024 * 1024,
		}
		rt := NewRuntime(cfg)

		start := time.Now()
		_, err := rt.Execute(context.Background(), `
			function main(input) {
				function recurse(n) { return recurse(n + 1); }
				return recurse(0);
			}
		`, nil)
		elapsed := time.Since(start)

		if err == nil {
			t.Fatal("expected stack overflow error for deep recursion")
		}
		// Must be caught by stack limit, not timeout — should resolve well under 2s
		if elapsed > 2*time.Second {
			t.Fatalf("deep recursion took %v — should be caught by stack limit, not timeout", elapsed)
		}
		// Goja's stack overflow error includes the call site (e.g. "at recurse (<eval>:...)")
		// rather than the words "stack overflow" — accept either form.
		errStr := err.Error()
		isStackError := strings.Contains(errStr, "stack") ||
			strings.Contains(errStr, "overflow") ||
			strings.Contains(errStr, "at recurse")
		if !isStackError {
			t.Fatalf("expected stack overflow error, got: %v", err)
		}
	})

	t.Run("oom_large_allocation", func(t *testing.T) {
		cfg := Config{
			MaxExecutionTime: 10 * time.Second, // Long timeout so memory limit triggers first
			MaxMemoryBytes:   1 * 1024 * 1024,  // 1MB limit
		}
		rt := NewRuntime(cfg)

		start := time.Now()
		_, err := rt.Execute(context.Background(), `
			function main(input) {
				var arr = [];
				for (var i = 0; i < 10000000; i++) {
					arr.push("x".repeat(1000));
				}
				return arr.length;
			}
		`, nil)
		elapsed := time.Since(start)

		if err == nil {
			t.Fatal("expected memory limit error for large allocation")
		}
		// Must be caught by memory watchdog, not timeout
		if elapsed > 5*time.Second {
			t.Fatalf("large allocation took %v — should be caught by memory limit, not timeout", elapsed)
		}
	})
}
