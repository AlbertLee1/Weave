package main

import (
	"context"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

func TestGojaCommitJobRunner_LintsValidSource(t *testing.T) {
	r := newGojaCommitJobRunner()
	res := r.RunCommitJob(context.Background(), oms.CommitJobRunInput{
		FunctionRID: "rid",
		CommitSha:   "abc",
		SourceCode:  "function main(input) { return 1; }",
	})
	if res.Status != oms.CommitJobStatusSuccess {
		t.Fatalf("want success, got %q (err=%q lint=%q)", res.Status, res.ErrorMessage, res.LintOutput)
	}
	if !strings.Contains(res.LintOutput, "lint ok") {
		t.Fatalf("expected lint ok, got %q", res.LintOutput)
	}
}

func TestGojaCommitJobRunner_FailsOnSyntaxError(t *testing.T) {
	r := newGojaCommitJobRunner()
	res := r.RunCommitJob(context.Background(), oms.CommitJobRunInput{
		SourceCode: "function ( {",
	})
	if res.Status != oms.CommitJobStatusFailure {
		t.Fatalf("want failure, got %q", res.Status)
	}
	if res.ErrorMessage == "" {
		t.Fatalf("expected error message to be populated")
	}
}

func TestGojaCommitJobRunner_RunsDeclaredTest(t *testing.T) {
	r := newGojaCommitJobRunner()
	src := `
function add(a, b) { return a + b; }
function test() {
  expect(add(2, 3)).toBe(5);
}
`
	res := r.RunCommitJob(context.Background(), oms.CommitJobRunInput{SourceCode: src})
	if res.Status != oms.CommitJobStatusSuccess {
		t.Fatalf("want success, got %q (err=%q test=%q)", res.Status, res.ErrorMessage, res.TestOutput)
	}
	if !strings.Contains(res.TestOutput, "test ok") {
		t.Fatalf("expected test ok, got %q", res.TestOutput)
	}
}

func TestGojaCommitJobRunner_FailsWhenAssertionFails(t *testing.T) {
	r := newGojaCommitJobRunner()
	src := `
function test() {
  expect(1).toBe(2);
}
`
	res := r.RunCommitJob(context.Background(), oms.CommitJobRunInput{SourceCode: src})
	if res.Status != oms.CommitJobStatusFailure {
		t.Fatalf("want failure, got %q (test=%q)", res.Status, res.TestOutput)
	}
	if !strings.Contains(strings.ToLower(res.ErrorMessage), "test threw") {
		t.Fatalf("expected error to mention test failure, got %q", res.ErrorMessage)
	}
}

func TestGojaCommitJobRunner_NoTestFunctionSucceeds(t *testing.T) {
	r := newGojaCommitJobRunner()
	src := "function helper(x) { return x + 1; }"
	res := r.RunCommitJob(context.Background(), oms.CommitJobRunInput{SourceCode: src})
	if res.Status != oms.CommitJobStatusSuccess {
		t.Fatalf("want success, got %q", res.Status)
	}
	if !strings.Contains(res.TestOutput, "no test() function declared") {
		t.Fatalf("expected skipped test output, got %q", res.TestOutput)
	}
}

func TestGojaCommitJobRunner_EmptySourceSkipped(t *testing.T) {
	r := newGojaCommitJobRunner()
	res := r.RunCommitJob(context.Background(), oms.CommitJobRunInput{SourceCode: "  \n  "})
	if res.Status != oms.CommitJobStatusSkipped {
		t.Fatalf("want skipped, got %q", res.Status)
	}
}

func TestGojaCommitJobRunner_TruncatesOversizeOutput(t *testing.T) {
	r := newGojaCommitJobRunner()
	r.outputCap = 32 // small enough to force truncation
	huge := strings.Repeat("x", 200)
	src := "function test() { throw new Error('" + huge + "'); }"
	res := r.RunCommitJob(context.Background(), oms.CommitJobRunInput{SourceCode: src})
	if res.Status != oms.CommitJobStatusFailure {
		t.Fatalf("want failure, got %q", res.Status)
	}
	if !strings.HasSuffix(res.ErrorMessage, "...[truncated]") {
		t.Fatalf("expected truncation suffix, got %q", res.ErrorMessage)
	}
}

func TestContainsTestDeclaration(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{"function test", "function test() {}", true},
		{"function test space", "function test () {}", true},
		{"const test", "const test = () => {};", true},
		{"let test", "let test = function() {};", true},
		{"var test", "var test = function() {};", true},
		{"test = function", "test = function() {};", true},
		{"no test", "function helper() {}", false},
		{"empty", "", false},
		{"comment only", "// nothing here", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := containsTestDeclaration(c.src); got != c.want {
				t.Fatalf("got %v want %v for %q", got, c.want, c.src)
			}
		})
	}
}
