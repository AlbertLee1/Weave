package functions

import (
	"context"
	"testing"
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
