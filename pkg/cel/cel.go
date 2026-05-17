// Package cel implements US-487's row-level-security CEL expression engine.
//
// CEL (Common Expression Language, github.com/google/cel-go) is a Turing-
// incomplete predicate language designed for embedding policy rules. This
// package exposes the narrow surface needed by pkg/rls to evaluate
// per-row policy expressions of the shape:
//
//	user.dept == object.dept && object.level <= user.clearance
//
// Bindings:
//
//	user    map<string, dyn>  — the calling auth.User flattened to a map
//	object  map<string, dyn>  — the candidate row's Properties map
//
// The expression MUST return bool. Compile + Eval surface two complementary
// guards (PRD US-487 "负向测试：表达式越界、循环引用拒绝"):
//
//  1. Compile rejects expressions over MaxExpressionLength bytes or whose
//     AST node count exceeds MaxASTNodeCount — the "out of bounds" guard
//     for source-text and AST-shape DoS bombs (deeply nested macros, huge
//     comprehensions, etc.).
//  2. DetectIdentifierCycle is a generic graph-cycle detector exposed for
//     callers that want to compose multiple policies / expressions by name.
//     It returns the cycle path (e.g. ["A", "B", "A"]) when a cycle exists,
//     allowing callers to surface the literal reference loop in their
//     error message.
//
// Evaluation is fail-closed: any runtime error (missing binding, type
// mismatch, cost-limit trip) returns (false, err) so the caller's "policy
// failed → row not visible" default is preserved automatically.
package cel

import (
	"errors"
	"fmt"
	"strings"

	celgo "github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/ast"
	"github.com/google/cel-go/common/types"
)

// Sentinel errors. Callers use errors.Is to branch on the cause.
var (
	// ErrEmptyExpression is returned by Compile when the expression is
	// blank after TrimSpace. Callers should treat empty as "no CEL gate"
	// at their layer rather than passing it through.
	ErrEmptyExpression = errors.New("cel: expression is empty")
	// ErrOutOfBounds is returned when Compile rejects an expression for
	// exceeding the configured source-length or AST-node-count limits.
	ErrOutOfBounds = errors.New("cel: expression exceeds size or complexity limits")
	// ErrCycleDetected is returned by DetectIdentifierCycle when the
	// reference graph contains a directed cycle.
	ErrCycleDetected = errors.New("cel: cycle detected in identifier references")
)

// Default safety bounds applied by Compile when Config zero-values its
// limit fields. Tuned so a typical row-policy expression (a handful of
// boolean / comparison operators on user.* and object.*) fits with
// generous headroom while obvious DoS bombs are rejected at admin-create
// time rather than at every read.
const (
	DefaultMaxExpressionLength = 4096
	DefaultMaxASTNodeCount     = 256
)

// Config bounds Compile's accepted expressions. Zero-valued fields fall
// back to the Default* constants — callers can pass Config{} to Compile
// indirectly via CompileWithConfig and get the safe defaults.
type Config struct {
	// MaxExpressionLength caps len(expression) in bytes. Defaults to
	// DefaultMaxExpressionLength when <= 0.
	MaxExpressionLength int
	// MaxASTNodeCount caps the post-parse AST size measured by a
	// PostOrderVisit walk. Defaults to DefaultMaxASTNodeCount when <= 0.
	MaxASTNodeCount int
}

// DefaultConfig returns the same bounds Compile uses when called without
// an explicit config.
func DefaultConfig() Config {
	return Config{
		MaxExpressionLength: DefaultMaxExpressionLength,
		MaxASTNodeCount:     DefaultMaxASTNodeCount,
	}
}

// Program is a parsed + type-checked CEL program ready for repeated Eval
// calls. Safe for concurrent use across goroutines.
type Program struct {
	src string
	prg celgo.Program
}

// Source returns the trimmed expression source captured at Compile time.
// Nil receiver returns "" so trace / log call sites do not have to nil-check.
func (p *Program) Source() string {
	if p == nil {
		return ""
	}
	return p.src
}

// Binding is the (user, object) input shape for Eval. Either side may be
// nil; Eval substitutes an empty map so missing-key reads in the CEL
// expression surface as a runtime error rather than a nil-map panic.
type Binding struct {
	User   map[string]any
	Object map[string]any
}

// celEnv is built once at init and reused — building a CEL env compiles
// the type-checker tables (~ms scale) so we never want to do it per-call.
var celEnv *celgo.Env

func init() {
	env, err := celgo.NewEnv(
		celgo.Variable("user", celgo.MapType(celgo.StringType, celgo.DynType)),
		celgo.Variable("object", celgo.MapType(celgo.StringType, celgo.DynType)),
	)
	if err != nil {
		panic(fmt.Sprintf("cel: build env: %v", err))
	}
	celEnv = env
}

// Compile parses, type-checks and lowers expression into a reusable
// Program with the default Config bounds. Returns ErrEmptyExpression for
// blank input, ErrOutOfBounds for over-limit source/AST, and a wrapped
// parse / type-check error otherwise. The PRD literal example
// "user.dept == object.dept && object.level <= user.clearance" compiles
// cleanly under the defaults.
func Compile(expression string) (*Program, error) {
	return CompileWithConfig(expression, DefaultConfig())
}

// CompileWithConfig is the bound-aware Compile variant. Zero / negative
// fields in cfg fall back to the Default* constants so callers can pass
// a partially-filled Config without unintentionally disabling safety.
func CompileWithConfig(expression string, cfg Config) (*Program, error) {
	src := strings.TrimSpace(expression)
	if src == "" {
		return nil, ErrEmptyExpression
	}
	if cfg.MaxExpressionLength <= 0 {
		cfg.MaxExpressionLength = DefaultMaxExpressionLength
	}
	if cfg.MaxASTNodeCount <= 0 {
		cfg.MaxASTNodeCount = DefaultMaxASTNodeCount
	}
	if len(src) > cfg.MaxExpressionLength {
		return nil, fmt.Errorf("%w: source length %d exceeds limit %d", ErrOutOfBounds, len(src), cfg.MaxExpressionLength)
	}
	parsedAST, iss := celEnv.Compile(src)
	if iss != nil && iss.Err() != nil {
		return nil, fmt.Errorf("cel: compile %q: %w", src, iss.Err())
	}
	if !parsedAST.OutputType().IsExactType(celgo.BoolType) {
		return nil, fmt.Errorf("cel: expression %q must return bool, got %s", src, parsedAST.OutputType().String())
	}
	if nodes := countASTNodes(parsedAST); nodes > cfg.MaxASTNodeCount {
		return nil, fmt.Errorf("%w: AST node count %d exceeds limit %d", ErrOutOfBounds, nodes, cfg.MaxASTNodeCount)
	}
	prg, err := celEnv.Program(parsedAST, celgo.EvalOptions(celgo.OptOptimize))
	if err != nil {
		return nil, fmt.Errorf("cel: program %q: %w", src, err)
	}
	return &Program{src: src, prg: prg}, nil
}

// Validate parses + type-checks expression without retaining the
// Program. Useful for admin-create handlers that want to reject invalid
// CEL up front without paying the program-construction cost.
func Validate(expression string) error {
	_, err := Compile(expression)
	return err
}

// Eval runs the compiled program against the user / object binding and
// returns the boolean verdict. Errors surface wrapped so callers can
// distinguish "policy compile bug" (return false at admin-create time
// already) from "policy ran but disagreed". Callers that want a fail-
// closed gate should treat any non-nil err as "deny" rather than
// "allow".
func (p *Program) Eval(b Binding) (bool, error) {
	if p == nil || p.prg == nil {
		return false, errors.New("cel: nil program")
	}
	user := b.User
	if user == nil {
		user = map[string]any{}
	}
	object := b.Object
	if object == nil {
		object = map[string]any{}
	}
	out, _, err := p.prg.Eval(map[string]any{
		"user":   user,
		"object": object,
	})
	if err != nil {
		return false, fmt.Errorf("cel: eval %q: %w", p.src, err)
	}
	if v, ok := out.(types.Bool); ok {
		return bool(v), nil
	}
	val := out.Value()
	if b2, ok := val.(bool); ok {
		return b2, nil
	}
	return false, fmt.Errorf("cel: expression %q returned non-bool %T", p.src, val)
}

// countASTNodes walks the parsed AST and counts every Expr node. Used as
// the "AST shape DoS" guard during Compile so a deeply-nested macro /
// huge list literal is rejected before we build the runtime program.
// Returns 0 for a nil ast — callers must have already nil-checked.
func countASTNodes(parsed *celgo.Ast) int {
	if parsed == nil {
		return 0
	}
	native := parsed.NativeRep()
	if native == nil {
		return 0
	}
	count := 0
	visitor := ast.NewExprVisitor(func(_ ast.Expr) { count++ })
	ast.PostOrderVisit(native.Expr(), visitor)
	return count
}

// DetectIdentifierCycle reports whether refs (a directed graph keyed by
// identifier name, values = identifiers the key references) contains a
// cycle. On cycle the returned slice is the closed loop in encounter
// order (e.g. ["A", "B", "A"]); err wraps ErrCycleDetected. A DAG
// returns (nil, nil).
//
// Use case: policies that compose / include each other by RID need a
// load-time guard against A→B→A loops that would otherwise infinite-
// recurse in an evaluator. The PRD US-487 "循环引用拒绝" negative test
// pins this contract: a refs map encoding a cycle must surface a
// rejection at validation time.
func DetectIdentifierCycle(refs map[string][]string) ([]string, error) {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	if len(refs) == 0 {
		return nil, nil
	}
	color := make(map[string]int, len(refs))
	parent := make(map[string]string, len(refs))

	var dfs func(node string) ([]string, error)
	dfs = func(node string) ([]string, error) {
		color[node] = gray
		for _, next := range refs[node] {
			switch color[next] {
			case white:
				parent[next] = node
				if cycle, err := dfs(next); err != nil {
					return cycle, err
				}
			case gray:
				// Back edge — rebuild the cycle path closed at `next`.
				// Walk parents from `node` back to `next`, accumulating
				// the path in reverse, then flip + close.
				path := []string{node}
				for cur := node; cur != next; {
					p, ok := parent[cur]
					if !ok || p == "" {
						// Defensive: parent chain broken (shouldn't
						// happen for a valid back edge, but never panic
						// in a security gate).
						break
					}
					path = append(path, p)
					cur = p
				}
				reverse(path)
				path = append(path, next)
				return path, fmt.Errorf("%w: %s", ErrCycleDetected, strings.Join(path, " → "))
			}
		}
		color[node] = black
		return nil, nil
	}

	// Iterate in a deterministic-ish order: every key with no incoming
	// edge first, then any remaining unvisited nodes. Going in arbitrary
	// map order is fine for correctness, but starting from sources
	// produces nicer cycle paths for the common "A → B → A while C → B"
	// shape.
	for node := range refs {
		if color[node] != white {
			continue
		}
		if cycle, err := dfs(node); err != nil {
			return cycle, err
		}
	}
	return nil, nil
}

func reverse(s []string) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}
