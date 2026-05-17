package cel

import (
	"errors"
	"strings"
	"testing"
)

// TestUS487_Compile_PRDExample_UserDeptObjectDept_LevelClearance pins the
// literal example from PRD US-487: a CEL expression that joins fields
// across the user.* and object.* bindings. Compile must accept it and the
// eval verdict must flip true / false based on the binding contents.
func TestUS487_Compile_PRDExample_UserDeptObjectDept_LevelClearance(t *testing.T) {
	prg, err := Compile(`user.dept == object.dept && object.level <= user.clearance`)
	if err != nil {
		t.Fatalf("Compile PRD example: %v", err)
	}
	if prg.Source() == "" {
		t.Fatalf("Source() should echo the trimmed expression")
	}

	// Same dept, level within clearance → true.
	got, err := prg.Eval(Binding{
		User:   map[string]any{"dept": "eng", "clearance": 3},
		Object: map[string]any{"dept": "eng", "level": 2},
	})
	if err != nil {
		t.Fatalf("Eval allow: %v", err)
	}
	if !got {
		t.Fatalf("expected true (same dept, level <= clearance), got false")
	}

	// Different dept → false even though level is within clearance.
	got, err = prg.Eval(Binding{
		User:   map[string]any{"dept": "ops", "clearance": 3},
		Object: map[string]any{"dept": "eng", "level": 2},
	})
	if err != nil {
		t.Fatalf("Eval reject by dept: %v", err)
	}
	if got {
		t.Fatalf("expected false (different dept), got true")
	}

	// Same dept but level exceeds clearance → false.
	got, err = prg.Eval(Binding{
		User:   map[string]any{"dept": "eng", "clearance": 1},
		Object: map[string]any{"dept": "eng", "level": 5},
	})
	if err != nil {
		t.Fatalf("Eval reject by level: %v", err)
	}
	if got {
		t.Fatalf("expected false (level > clearance), got true")
	}
}

// TestUS487_Compile_EmptyExpression_ReturnsErrEmpty pins the contract that
// an empty / whitespace-only expression is not a valid CEL gate.
func TestUS487_Compile_EmptyExpression_ReturnsErrEmpty(t *testing.T) {
	for _, src := range []string{"", "   ", "\n\t  "} {
		if _, err := Compile(src); !errors.Is(err, ErrEmptyExpression) {
			t.Fatalf("Compile(%q): want ErrEmptyExpression, got %v", src, err)
		}
	}
}

// TestUS487_Compile_NonBool_ReturnsTypeError verifies CEL's type-checker
// rejects expressions whose top-level return type is not bool. Row policy
// gates have one job — return a verdict — so an int / string return is a
// caller bug we must surface at admin-create time.
func TestUS487_Compile_NonBool_ReturnsTypeError(t *testing.T) {
	cases := []string{
		`1 + 2`,
		`"some string"`,
		`user.dept`,
	}
	for _, src := range cases {
		_, err := Compile(src)
		if err == nil {
			t.Fatalf("Compile(%q): expected non-bool error, got nil", src)
		}
		if !strings.Contains(err.Error(), "must return bool") {
			t.Fatalf("Compile(%q): expected 'must return bool' error, got %v", src, err)
		}
	}
}

// TestUS487_Compile_ParseError_ReturnsWrappedIssues exercises the parse /
// type-check error path — referencing an unknown variable is the cheapest
// way to force CEL's checker to emit issues. Sanity-check that the error
// surfaces the offending identifier so admins can debug.
func TestUS487_Compile_ParseError_ReturnsWrappedIssues(t *testing.T) {
	_, err := Compile(`unknownIdent && object.x == 1`)
	if err == nil {
		t.Fatalf("expected parse / check error, got nil")
	}
	if !strings.Contains(err.Error(), "unknownIdent") {
		t.Fatalf("expected error to mention 'unknownIdent', got: %v", err)
	}
}

// TestUS487_Compile_OutOfBounds_LongSource_RejectedAtAdminCreate covers
// the PRD literal "表达式越界" guard at the source-length axis: an
// expression whose source exceeds MaxExpressionLength must be rejected
// before CEL parsing kicks in.
func TestUS487_Compile_OutOfBounds_LongSource_RejectedAtAdminCreate(t *testing.T) {
	// Build a syntactically valid bool expression that just chains
	// `&& true` enough times to blow past the small custom limit.
	src := "true"
	for i := 0; i < 100; i++ {
		src += " && true"
	}
	cfg := Config{MaxExpressionLength: 32, MaxASTNodeCount: DefaultMaxASTNodeCount}
	_, err := CompileWithConfig(src, cfg)
	if !errors.Is(err, ErrOutOfBounds) {
		t.Fatalf("expected ErrOutOfBounds, got %v", err)
	}
	if !strings.Contains(err.Error(), "source length") {
		t.Fatalf("expected error message to cite source length, got %v", err)
	}
}

// TestUS487_Compile_OutOfBounds_DeepAST_RejectedAtAdminCreate covers the
// PRD literal "表达式越界" guard on the AST-shape axis: a long-but-still-
// fits-MaxExpressionLength expression with too many nodes (chained ||
// over a list of booleans) must trip ErrOutOfBounds. This is the guard
// that catches "small source, huge fan-out via macros" bombs that the
// pure byte-length check would miss.
func TestUS487_Compile_OutOfBounds_DeepAST_RejectedAtAdminCreate(t *testing.T) {
	src := "true"
	for i := 0; i < 50; i++ {
		src += " || true"
	}
	cfg := Config{MaxExpressionLength: 8192, MaxASTNodeCount: 10}
	_, err := CompileWithConfig(src, cfg)
	if !errors.Is(err, ErrOutOfBounds) {
		t.Fatalf("expected ErrOutOfBounds, got %v", err)
	}
	if !strings.Contains(err.Error(), "AST node count") {
		t.Fatalf("expected error message to cite AST node count, got %v", err)
	}
}

// TestUS487_Config_ZeroValuesFallBackToDefaults checks that callers
// passing Config{} get the safe defaults rather than "no limit" — same
// pattern as pkg/functions / pkg/sqlqueries config zero-value fallback.
func TestUS487_Config_ZeroValuesFallBackToDefaults(t *testing.T) {
	// Source 5KB > Default (4096) → should reject with default config
	// even though the explicit zero would naively mean "no limit".
	big := strings.Repeat("true && ", 1000) + "true"
	if _, err := CompileWithConfig(big, Config{}); !errors.Is(err, ErrOutOfBounds) {
		t.Fatalf("Config{} should fall back to defaults and reject: got %v", err)
	}
}

// TestUS487_Eval_MissingField_FailsClosed proves runtime errors (missing
// binding key) surface as (false, err). A row policy that depends on a
// property the row lacks must NOT silently allow the row — that would be
// the worst kind of policy-failure-open bug.
func TestUS487_Eval_MissingField_FailsClosed(t *testing.T) {
	prg, err := Compile(`object.dept == "eng"`)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	got, err := prg.Eval(Binding{
		User:   map[string]any{"id": "u1"},
		Object: map[string]any{"name": "alice"}, // no "dept"
	})
	if err == nil {
		t.Fatalf("expected error for missing field, got nil")
	}
	if got {
		t.Fatalf("missing field must NOT eval true; got true")
	}
}

// TestUS487_Eval_NilBindings_Substitutes_EmptyMaps_NoNilPanic locks in
// the contract that Eval is safe to call with a nil binding map — useful
// for callers that pass auth.User{Attributes: nil}. The expression
// should still get a chance to evaluate; missing keys are an eval error,
// not a panic.
func TestUS487_Eval_NilBindings_Substitutes_EmptyMaps_NoNilPanic(t *testing.T) {
	prg, err := Compile(`true`)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	got, err := prg.Eval(Binding{User: nil, Object: nil})
	if err != nil {
		t.Fatalf("Eval(nil bindings): %v", err)
	}
	if !got {
		t.Fatalf("expected true (constant) regardless of bindings")
	}
}

// TestUS487_DetectIdentifierCycle_RejectsTwoNodeLoop pins the smallest
// cycle: A → B → A. The returned slice must close the loop so callers
// can render "A → B → A" in their error.
func TestUS487_DetectIdentifierCycle_RejectsTwoNodeLoop(t *testing.T) {
	refs := map[string][]string{
		"A": {"B"},
		"B": {"A"},
	}
	cycle, err := DetectIdentifierCycle(refs)
	if !errors.Is(err, ErrCycleDetected) {
		t.Fatalf("expected ErrCycleDetected, got %v", err)
	}
	if len(cycle) < 3 {
		t.Fatalf("expected cycle to close (len >= 3), got %v", cycle)
	}
	if cycle[0] != cycle[len(cycle)-1] {
		t.Fatalf("cycle should be closed (first == last), got %v", cycle)
	}
	// Cycle path text must surface in the error message.
	if !strings.Contains(err.Error(), strings.Join(cycle, " → ")) {
		t.Fatalf("error should embed cycle path %v: %v", cycle, err)
	}
}

// TestUS487_DetectIdentifierCycle_RejectsThreeNodeLoop covers the
// transitive case A → B → C → A.
func TestUS487_DetectIdentifierCycle_RejectsThreeNodeLoop(t *testing.T) {
	refs := map[string][]string{
		"A": {"B"},
		"B": {"C"},
		"C": {"A"},
	}
	_, err := DetectIdentifierCycle(refs)
	if !errors.Is(err, ErrCycleDetected) {
		t.Fatalf("expected ErrCycleDetected, got %v", err)
	}
}

// TestUS487_DetectIdentifierCycle_RejectsSelfLoop covers A → A (one
// policy referencing itself directly).
func TestUS487_DetectIdentifierCycle_RejectsSelfLoop(t *testing.T) {
	refs := map[string][]string{"A": {"A"}}
	cycle, err := DetectIdentifierCycle(refs)
	if !errors.Is(err, ErrCycleDetected) {
		t.Fatalf("expected ErrCycleDetected, got %v", err)
	}
	if len(cycle) == 0 || cycle[0] != "A" {
		t.Fatalf("expected cycle starting at A, got %v", cycle)
	}
}

// TestUS487_DetectIdentifierCycle_DAG_NoError proves DAGs are accepted —
// negative-control for the cycle tests above so we know the detector is
// not "always return cycle".
func TestUS487_DetectIdentifierCycle_DAG_NoError(t *testing.T) {
	refs := map[string][]string{
		"A": {"B", "C"},
		"B": {"D"},
		"C": {"D"},
		"D": nil,
	}
	cycle, err := DetectIdentifierCycle(refs)
	if err != nil {
		t.Fatalf("DAG should not error: %v", err)
	}
	if cycle != nil {
		t.Fatalf("DAG should return nil cycle, got %v", cycle)
	}
}

// TestUS487_DetectIdentifierCycle_EmptyGraph_NoError covers the trivial
// edge case so callers (RLS engine on first boot before any policy
// declares Refs) get a clean no-op.
func TestUS487_DetectIdentifierCycle_EmptyGraph_NoError(t *testing.T) {
	if _, err := DetectIdentifierCycle(nil); err != nil {
		t.Fatalf("nil refs should not error: %v", err)
	}
	if _, err := DetectIdentifierCycle(map[string][]string{}); err != nil {
		t.Fatalf("empty refs should not error: %v", err)
	}
}

// TestUS487_Validate_DelegatesToCompile is a tiny doc-test pinning the
// Validate→Compile delegation so future "validate becomes lighter than
// compile" refactors don't accidentally weaken the bounds checks.
func TestUS487_Validate_DelegatesToCompile(t *testing.T) {
	if err := Validate(`object.foo == "bar"`); err != nil {
		t.Fatalf("Validate good expression: %v", err)
	}
	if err := Validate(``); !errors.Is(err, ErrEmptyExpression) {
		t.Fatalf("Validate empty expression: want ErrEmptyExpression, got %v", err)
	}
	if err := Validate(`1 + 1`); err == nil {
		t.Fatalf("Validate non-bool: expected error, got nil")
	}
}

// TestUS487_NilProgram_EvalReturnsError makes sure a nil receiver does
// not panic — handler code paths sometimes hand around a *Program field
// that hasn't been initialized in degraded boot modes.
func TestUS487_NilProgram_EvalReturnsError(t *testing.T) {
	var p *Program
	got, err := p.Eval(Binding{})
	if err == nil {
		t.Fatalf("expected error from nil program, got nil")
	}
	if got {
		t.Fatalf("nil program must NOT allow rows; got true")
	}
}
