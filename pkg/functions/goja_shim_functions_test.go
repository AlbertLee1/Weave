package functions

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/liyang/weave/pkg/functions/fncall"
)

// scriptedFunctionCaller is a test double. Each entry maps a ref → JS
// source. CallFunction reinvokes the runtime with the child context so the
// call stack flows through exactly as it would via a real executor wiring.
type scriptedFunctionCaller struct {
	runtime *Runtime
	scripts map[string]string
	calls   atomic.Int64
}

func (c *scriptedFunctionCaller) CallFunction(ctx context.Context, ref string, params map[string]interface{}) (interface{}, error) {
	c.calls.Add(1)
	src, ok := c.scripts[ref]
	if !ok {
		return nil, fmt.Errorf("function %q not found", ref)
	}
	return c.runtime.Execute(ctx, src, params)
}

func TestWeaveCallFunction_HappyPath(t *testing.T) {
	rt := NewRuntime(DefaultConfig())
	caller := &scriptedFunctionCaller{
		runtime: rt,
		scripts: map[string]string{
			"ri.main.main.function.double": `
				function main(input) {
					return input.value * 2;
				}
			`,
		},
	}
	rt.SetFunctionCaller(caller)

	result, err := rt.Execute(context.Background(), `
		function main(input) {
			var r = weave.callFunction("ri.main.main.function.double", {value: 21});
			return r + 1;
		}
	`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Goja exports whole-number arithmetic as int64.
	got := toFloat64(t, result)
	if got != 43 {
		t.Fatalf("expected 43, got %v (%T)", result, result)
	}
	if caller.calls.Load() != 1 {
		t.Fatalf("expected 1 call, got %d", caller.calls.Load())
	}
}

func TestWeaveCallFunction_ParamsFlowThrough(t *testing.T) {
	rt := NewRuntime(DefaultConfig())
	var captured map[string]interface{}
	caller := &scriptedFunctionCaller{
		runtime: rt,
		scripts: map[string]string{
			"helper": `
				function main(input) {
					return {echo: input};
				}
			`,
		},
	}
	// Override to capture the raw params map.
	originalScripts := caller.scripts
	caller = &scriptedFunctionCaller{runtime: rt, scripts: originalScripts}
	interceptor := &capturingCaller{inner: caller, captured: &captured}
	rt.SetFunctionCaller(interceptor)

	_, err := rt.Execute(context.Background(), `
		function main(input) {
			return weave.callFunction("helper", {a: "hi", n: 7, flag: true});
		}
	`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured["a"] != "hi" {
		t.Fatalf("expected params.a=hi, got %v", captured["a"])
	}
	if toFloat64(t, captured["n"]) != 7 {
		t.Fatalf("expected params.n=7, got %v", captured["n"])
	}
	if captured["flag"] != true {
		t.Fatalf("expected params.flag=true, got %v", captured["flag"])
	}
}

func TestWeaveCallFunction_NoCallerShimAbsent(t *testing.T) {
	rt := NewRuntime(DefaultConfig())
	// No SetFunctionCaller call — weave.callFunction must be absent even
	// though the weave global itself is always registered (US-241 added
	// reportProgress which has no per-runtime dependency).

	result, err := rt.Execute(context.Background(), `
		function main(input) {
			return typeof weave.callFunction;
		}
	`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "undefined" {
		t.Fatalf("expected typeof weave.callFunction == undefined, got %v", result)
	}
}

func TestWeaveCallFunction_CycleDetected_AToBToA(t *testing.T) {
	rt := NewRuntime(DefaultConfig())
	caller := &scriptedFunctionCaller{
		runtime: rt,
		scripts: map[string]string{
			"A": `
				function main(input) {
					return weave.callFunction("B", input);
				}
			`,
			"B": `
				function main(input) {
					return weave.callFunction("A", input);
				}
			`,
		},
	}
	rt.SetFunctionCaller(caller)

	// Seed the top-level frame with "A" so a B→A re-entrance is flagged
	// at the first attempt rather than the second.
	ctx := fncall.WithFrame(context.Background(), "A")
	_, err := rt.Execute(ctx, caller.scripts["A"], nil)
	if err == nil {
		t.Fatal("expected cycle detection error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

func TestWeaveCallFunction_CycleDetected_SelfRecursion(t *testing.T) {
	rt := NewRuntime(DefaultConfig())
	caller := &scriptedFunctionCaller{
		runtime: rt,
		scripts: map[string]string{
			"self": `
				function main(input) {
					return weave.callFunction("self", input);
				}
			`,
		},
	}
	rt.SetFunctionCaller(caller)

	ctx := fncall.WithFrame(context.Background(), "self")
	_, err := rt.Execute(ctx, caller.scripts["self"], nil)
	if err == nil {
		t.Fatal("expected cycle detection error for self-recursion")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

func TestWeaveCallFunction_DepthLimit(t *testing.T) {
	rt := NewRuntime(DefaultConfig())
	scripts := map[string]string{}
	// Chain f0 → f1 → ... → f9 (ten levels). With MaxDepth=8 the 9th
	// link (attempting to push the 9th frame) must be rejected.
	for i := 0; i < 10; i++ {
		next := fmt.Sprintf("f%d", i+1)
		scripts[fmt.Sprintf("f%d", i)] = fmt.Sprintf(`
			function main(input) {
				return weave.callFunction(%q, input);
			}
		`, next)
	}
	// Terminal function — never reached, but wired so a missing-ref error
	// can't be mistaken for the depth rejection.
	scripts["f10"] = `
		function main(input) {
			return 1;
		}
	`
	caller := &scriptedFunctionCaller{runtime: rt, scripts: scripts}
	rt.SetFunctionCaller(caller)

	_, err := rt.Execute(context.Background(), scripts["f0"], nil)
	if err == nil {
		t.Fatal("expected depth-limit error")
	}
	if !strings.Contains(err.Error(), "depth") {
		t.Fatalf("expected depth error, got %v", err)
	}
	if fncall.MaxDepth != 8 {
		t.Fatalf("PRD requires MaxDepth=8, got %d", fncall.MaxDepth)
	}
}

func TestWeaveCallFunction_DepthJustUnderLimit(t *testing.T) {
	rt := NewRuntime(DefaultConfig())
	scripts := map[string]string{}
	// Chain fN-1 levels of call, staying strictly below MaxDepth. The
	// terminal function returns 42; every intermediate forwards the return
	// value unchanged so we can assert the whole chain executed.
	const levels = fncall.MaxDepth // == 8 nested calls; depth hits the ceiling
	for i := 0; i < levels-1; i++ {
		next := fmt.Sprintf("g%d", i+1)
		scripts[fmt.Sprintf("g%d", i)] = fmt.Sprintf(`
			function main(input) {
				return weave.callFunction(%q, input);
			}
		`, next)
	}
	scripts[fmt.Sprintf("g%d", levels-1)] = `
		function main(input) {
			return 42;
		}
	`
	caller := &scriptedFunctionCaller{runtime: rt, scripts: scripts}
	rt.SetFunctionCaller(caller)

	result, err := rt.Execute(context.Background(), scripts["g0"], nil)
	if err != nil {
		t.Fatalf("unexpected error at depth %d: %v", levels, err)
	}
	if toFloat64(t, result) != 42 {
		t.Fatalf("expected 42, got %v", result)
	}
}

func TestWeaveCallFunction_UnknownRef(t *testing.T) {
	rt := NewRuntime(DefaultConfig())
	caller := &scriptedFunctionCaller{runtime: rt, scripts: map[string]string{}}
	rt.SetFunctionCaller(caller)

	_, err := rt.Execute(context.Background(), `
		function main(input) {
			return weave.callFunction("missing", {});
		}
	`, nil)
	if err == nil {
		t.Fatal("expected error for unknown function")
	}
	if !strings.Contains(err.Error(), "missing") && !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected missing/not found error, got %v", err)
	}
}

func TestWeaveCallFunction_RefRequired(t *testing.T) {
	rt := NewRuntime(DefaultConfig())
	caller := &scriptedFunctionCaller{runtime: rt, scripts: map[string]string{}}
	rt.SetFunctionCaller(caller)

	cases := []string{
		`function main(input) { return weave.callFunction(); }`,
		`function main(input) { return weave.callFunction(""); }`,
	}
	for _, src := range cases {
		_, err := rt.Execute(context.Background(), src, nil)
		if err == nil {
			t.Fatalf("expected error for src %q", src)
		}
	}
}

func TestWeaveCallFunction_CyclicErrorUnwrapsToSentinel(t *testing.T) {
	rt := NewRuntime(DefaultConfig())
	scripts := map[string]string{
		"self": `
			function main(input) {
				return weave.callFunction("self", input);
			}
		`,
	}
	caller := &scriptedFunctionCaller{runtime: rt, scripts: scripts}
	rt.SetFunctionCaller(caller)

	ctx := fncall.WithFrame(context.Background(), "self")
	_, err := rt.Execute(ctx, scripts["self"], nil)
	if err == nil {
		t.Fatal("expected cycle error")
	}
	// The runtime currently wraps the thrown JS error via wrapGojaError
	// into an opaque "execution error" chain. We still want the sentinel
	// reachable via errors.Is so upstream handlers can dispatch on cause.
	if !errors.Is(err, fncall.ErrCycleDetected) {
		t.Fatalf("expected errors.Is(err, ErrCycleDetected) == true, got %v", err)
	}
}

func TestWeaveCallFunction_DepthErrorUnwrapsToSentinel(t *testing.T) {
	rt := NewRuntime(DefaultConfig())
	scripts := map[string]string{}
	for i := 0; i < 10; i++ {
		next := fmt.Sprintf("d%d", i+1)
		scripts[fmt.Sprintf("d%d", i)] = fmt.Sprintf(`
			function main(input) {
				return weave.callFunction(%q, input);
			}
		`, next)
	}
	scripts["d10"] = `function main(input) { return 1; }`
	caller := &scriptedFunctionCaller{runtime: rt, scripts: scripts}
	rt.SetFunctionCaller(caller)

	_, err := rt.Execute(context.Background(), scripts["d0"], nil)
	if err == nil {
		t.Fatal("expected depth error")
	}
	if !errors.Is(err, fncall.ErrDepthExceeded) {
		t.Fatalf("expected errors.Is(err, ErrDepthExceeded) == true, got %v", err)
	}
}

func TestWeaveCallFunction_WithStackRoundTrip(t *testing.T) {
	ctx := context.Background()
	if got := fncall.StackFromContext(ctx); got != nil {
		t.Fatalf("expected nil stack, got %v", got)
	}

	ctx = fncall.WithFrame(ctx, "one")
	ctx = fncall.WithFrame(ctx, "two")
	got := fncall.StackFromContext(ctx)
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("expected [one two], got %v", got)
	}

	// WithFrame must not mutate the parent slice.
	parent := fncall.WithFrame(context.Background(), "p")
	_ = fncall.WithFrame(parent, "c")
	if got := fncall.StackFromContext(parent); len(got) != 1 || got[0] != "p" {
		t.Fatalf("expected parent=[p], got %v", got)
	}
}

// capturingCaller wraps a FunctionCaller and records the params map seen on
// the first CallFunction invocation so tests can assert param coercion.
type capturingCaller struct {
	inner    FunctionCaller
	captured *map[string]interface{}
}

func (c *capturingCaller) CallFunction(ctx context.Context, ref string, params map[string]interface{}) (interface{}, error) {
	if *c.captured == nil {
		*c.captured = params
	}
	return c.inner.CallFunction(ctx, ref, params)
}

// toFloat64 normalises goja's int64 / float64 polymorphism.
func toFloat64(t *testing.T, v interface{}) float64 {
	t.Helper()
	switch n := v.(type) {
	case int64:
		return float64(n)
	case float64:
		return n
	case int:
		return float64(n)
	default:
		t.Fatalf("expected numeric, got %T (%v)", v, v)
		return 0
	}
}
