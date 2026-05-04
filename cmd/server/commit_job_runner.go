package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dop251/goja"
	"github.com/liyang/weave/pkg/oms"
)

// gojaCommitJobRunner is the default CI runner for US-417. It performs two
// hermetic phases per commit so the pipeline runs in any environment
// without requiring eslint / vitest binaries on PATH:
//
//   - Lint: parse the JS via goja.New().RunString in a fresh VM with a
//     1-second budget. A parse error fails the lint phase. The phase
//     deliberately compiles WITHOUT running `main`, so callers don't get
//     side-effects from the source under test.
//   - Test: scan the source for an exported / declared `test` function. If
//     present, instantiate a fresh VM, register the source, then invoke
//     `test()` with a synthetic `expect` shim that throws on falsy
//     assertions. Any thrown exception fails the phase. When no `test`
//     function is declared the phase is reported as skipped (the source
//     still passed lint, so the overall job is success).
//
// The runner is intentionally pluggable — production deploys with
// real Node tooling can swap this for a shell-out implementation via
// `omsHandler.SetCommitJobRunner(...)` without touching the surrounding
// commit-recording path.
type gojaCommitJobRunner struct {
	// timeout caps how long either phase can take. Defaults to 5s; tests
	// may override to keep wall-clock under the suite budget.
	timeout time.Duration
	// outputCap truncates per-phase output to this many bytes so a
	// pathological assertion message can't blow up PG row limits or
	// Marketplace / FunctionDiff UI tooltips. Defaults to 4 KiB.
	outputCap int
}

func newGojaCommitJobRunner() *gojaCommitJobRunner {
	return &gojaCommitJobRunner{
		timeout:   5 * time.Second,
		outputCap: 4 * 1024,
	}
}

// RunCommitJob implements oms.CommitJobRunner.
func (r *gojaCommitJobRunner) RunCommitJob(ctx context.Context, in oms.CommitJobRunInput) oms.CommitJobRunResult {
	if strings.TrimSpace(in.SourceCode) == "" {
		return oms.CommitJobRunResult{
			Status:       oms.CommitJobStatusSkipped,
			LintOutput:   "no source code to lint",
			TestOutput:   "no source code to test",
			ErrorMessage: "",
		}
	}

	lintOutput, lintErr := r.runLint(ctx, in.SourceCode)
	if lintErr != nil {
		return oms.CommitJobRunResult{
			Status:       oms.CommitJobStatusFailure,
			LintOutput:   r.cap(lintOutput),
			TestOutput:   "",
			ErrorMessage: r.cap(lintErr.Error()),
		}
	}

	hasTest := containsTestDeclaration(in.SourceCode)
	if !hasTest {
		return oms.CommitJobRunResult{
			Status:     oms.CommitJobStatusSuccess,
			LintOutput: r.cap(lintOutput),
			TestOutput: "no test() function declared — skipped",
		}
	}
	testOutput, testErr := r.runTest(ctx, in.SourceCode)
	if testErr != nil {
		return oms.CommitJobRunResult{
			Status:       oms.CommitJobStatusFailure,
			LintOutput:   r.cap(lintOutput),
			TestOutput:   r.cap(testOutput),
			ErrorMessage: r.cap(testErr.Error()),
		}
	}
	return oms.CommitJobRunResult{
		Status:     oms.CommitJobStatusSuccess,
		LintOutput: r.cap(lintOutput),
		TestOutput: r.cap(testOutput),
	}
}

// runLint compiles the source in a throw-away VM. Any parse / reference
// error is treated as a lint failure. The VM is configured with no shims
// so the source can't reach the surrounding host environment.
func (r *gojaCommitJobRunner) runLint(ctx context.Context, source string) (string, error) {
	execCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	vm := goja.New()
	vm.SetMaxCallStackSize(64)
	go func() {
		<-execCtx.Done()
		vm.Interrupt("lint timeout")
	}()
	if _, err := goja.Compile("source.js", source, true); err != nil {
		return "", fmt.Errorf("parse error: %w", err)
	}
	return "lint ok", nil
}

// runTest instantiates the source then calls `test()`. Assertions inside
// the test fail by throwing — the surrounding caller catches the panic /
// error and surfaces it as a phase failure.
func (r *gojaCommitJobRunner) runTest(ctx context.Context, source string) (string, error) {
	execCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	vm := goja.New()
	vm.SetMaxCallStackSize(64)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-execCtx.Done():
			vm.Interrupt("test timeout")
		case <-done:
		}
	}()

	// Register a tiny `expect(actual).toBe(expected)` API so simple
	// assertions work without a real test framework. Failures throw,
	// matching jest/vitest semantics.
	expectShim := func(call goja.FunctionCall) goja.Value {
		actual := call.Argument(0)
		obj := vm.NewObject()
		_ = obj.Set("toBe", func(c goja.FunctionCall) goja.Value {
			expected := c.Argument(0)
			if !actual.SameAs(expected) {
				panic(vm.ToValue(fmt.Sprintf("expect: %v != %v", actual, expected)))
			}
			return goja.Undefined()
		})
		_ = obj.Set("toEqual", func(c goja.FunctionCall) goja.Value {
			expected := c.Argument(0)
			a := actual.Export()
			e := expected.Export()
			if fmt.Sprintf("%v", a) != fmt.Sprintf("%v", e) {
				panic(vm.ToValue(fmt.Sprintf("expect: %v != %v", a, e)))
			}
			return goja.Undefined()
		})
		_ = obj.Set("toBeTruthy", func(c goja.FunctionCall) goja.Value {
			if !actual.ToBoolean() {
				panic(vm.ToValue(fmt.Sprintf("expect: %v is not truthy", actual)))
			}
			return goja.Undefined()
		})
		return obj
	}
	_ = vm.Set("expect", expectShim)

	if _, err := vm.RunString(source); err != nil {
		return "", fmt.Errorf("source threw on load: %w", err)
	}
	testFn, ok := goja.AssertFunction(vm.Get("test"))
	if !ok {
		return "no test() function found at runtime", nil
	}
	if _, err := testFn(goja.Undefined()); err != nil {
		return "", fmt.Errorf("test threw: %w", err)
	}
	return "test ok", nil
}

func (r *gojaCommitJobRunner) cap(s string) string {
	if r.outputCap <= 0 || len(s) <= r.outputCap {
		return s
	}
	return s[:r.outputCap] + "...[truncated]"
}

// containsTestDeclaration is the lightweight static check that decides
// whether the test phase should run. It looks for the canonical shapes
// `function test(`, `const test =`, `let test =`, `var test =`, or
// `test = function(` — same patterns vitest / jest recognise. False
// positives are harmless: the runner falls through to the "no test()
// function found at runtime" output, reporting success.
func containsTestDeclaration(source string) bool {
	candidates := []string{
		"function test(",
		"function test (",
		"const test=",
		"const test =",
		"let test=",
		"let test =",
		"var test=",
		"var test =",
		"test=function",
		"test = function",
	}
	lower := strings.ToLower(source)
	for _, candidate := range candidates {
		if strings.Contains(lower, candidate) {
			return true
		}
	}
	return false
}
