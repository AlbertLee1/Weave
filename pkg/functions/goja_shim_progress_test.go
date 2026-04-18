package functions

import (
	"context"
	"sync"
	"testing"
)

// captureProgressReporter records Report calls for assertions.
type captureProgressReporter struct {
	mu      sync.Mutex
	reports []ProgressReport
}

type ProgressReport struct {
	Percent int
	Message string
}

func (c *captureProgressReporter) Report(_ context.Context, percent int, msg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reports = append(c.reports, ProgressReport{Percent: percent, Message: msg})
}

func (c *captureProgressReporter) snapshot() []ProgressReport {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]ProgressReport, len(c.reports))
	copy(out, c.reports)
	return out
}

// TestWeaveReportProgress_HappyPath verifies the shim forwards percent + msg
// to the ProgressReporter carried on ctx.
func TestWeaveReportProgress_HappyPath(t *testing.T) {
	rt := NewRuntime(DefaultConfig())
	rep := &captureProgressReporter{}
	ctx := WithProgressReporter(context.Background(), rep)

	_, err := rt.Execute(ctx, `
		function main(input) {
			weave.reportProgress(25, "quarter");
			weave.reportProgress(50, "halfway");
			return 1;
		}
	`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := rep.snapshot()
	if len(got) != 2 {
		t.Fatalf("expected 2 reports, got %d: %+v", len(got), got)
	}
	if got[0].Percent != 25 || got[0].Message != "quarter" {
		t.Fatalf("report 0: want {25,quarter} got %+v", got[0])
	}
	if got[1].Percent != 50 || got[1].Message != "halfway" {
		t.Fatalf("report 1: want {50,halfway} got %+v", got[1])
	}
}

// TestWeaveReportProgress_ClampsRange verifies negative/out-of-range percent
// values are clamped to [0,100] before the reporter sees them. Keeps the
// callback contract predictable for SDK consumers.
func TestWeaveReportProgress_ClampsRange(t *testing.T) {
	rt := NewRuntime(DefaultConfig())
	rep := &captureProgressReporter{}
	ctx := WithProgressReporter(context.Background(), rep)

	_, err := rt.Execute(ctx, `
		function main() {
			weave.reportProgress(-5, "below");
			weave.reportProgress(1000, "above");
			weave.reportProgress(75.7, "float");
			return 1;
		}
	`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := rep.snapshot()
	if len(got) != 3 {
		t.Fatalf("expected 3 reports, got %d", len(got))
	}
	if got[0].Percent != 0 {
		t.Fatalf("clamp low: want 0, got %d", got[0].Percent)
	}
	if got[1].Percent != 100 {
		t.Fatalf("clamp high: want 100, got %d", got[1].Percent)
	}
	if got[2].Percent != 75 {
		t.Fatalf("float truncation: want 75, got %d", got[2].Percent)
	}
}

// TestWeaveReportProgress_NoReporterNoop verifies that when no reporter is on
// ctx the JS call is a no-op (doesn't throw, doesn't panic). Callers that
// reuse the same function in sync and async paths should not have to guard
// reportProgress calls.
func TestWeaveReportProgress_NoReporterNoop(t *testing.T) {
	rt := NewRuntime(DefaultConfig())

	result, err := rt.Execute(context.Background(), `
		function main() {
			weave.reportProgress(42, "ignored");
			return "ok";
		}
	`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Fatalf("expected ok, got %v", result)
	}
}

// TestWeaveReportProgress_OptionalMessage verifies the second argument is
// optional (common for quick-and-dirty progress updates).
func TestWeaveReportProgress_OptionalMessage(t *testing.T) {
	rt := NewRuntime(DefaultConfig())
	rep := &captureProgressReporter{}
	ctx := WithProgressReporter(context.Background(), rep)

	_, err := rt.Execute(ctx, `
		function main() {
			weave.reportProgress(30);
			return 1;
		}
	`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := rep.snapshot()
	if len(got) != 1 || got[0].Percent != 30 || got[0].Message != "" {
		t.Fatalf("expected {30,\"\"}, got %+v", got)
	}
}

// TestWeaveReportProgress_CoexistsWithCallFunction ensures progress reporting
// works alongside weave.callFunction — both live on the same `weave` global
// so the registration paths must not clobber each other.
func TestWeaveReportProgress_CoexistsWithCallFunction(t *testing.T) {
	rt := NewRuntime(DefaultConfig())
	caller := &scriptedFunctionCaller{
		runtime: rt,
		scripts: map[string]string{
			"double": `function main(input) { return input.v * 2; }`,
		},
	}
	rt.SetFunctionCaller(caller)
	rep := &captureProgressReporter{}
	ctx := WithProgressReporter(context.Background(), rep)

	result, err := rt.Execute(ctx, `
		function main() {
			weave.reportProgress(10, "before call");
			var r = weave.callFunction("double", {v: 5});
			weave.reportProgress(100, "done");
			return r;
		}
	`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if toFloat64(t, result) != 10 {
		t.Fatalf("expected 10, got %v", result)
	}
	got := rep.snapshot()
	if len(got) != 2 {
		t.Fatalf("expected 2 reports, got %d", len(got))
	}
}

// TestProgressReporterFromContext_Absent returns nil for a bare context so
// callers can test and no-op cleanly.
func TestProgressReporterFromContext_Absent(t *testing.T) {
	if r := ProgressReporterFromContext(context.Background()); r != nil {
		t.Fatalf("expected nil reporter, got %T", r)
	}
}
