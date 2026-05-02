// Package celmask compiles and evaluates US-376 cell-mask CEL expressions
// against a (user, row) binding. Programs are compiled once at policy load
// time (by cellsec.Engine.Reload) so the per-cell evaluate path is a flat
// map lookup plus a single cel.Program.Eval — fast enough to clear the PRD
// 1000-row × 10-column < 50 ms gate.
//
// Bindings exposed to the expression:
//
//	user.id          string
//	user.email       string
//	user.roles       list<string>
//	user.markings    list<string>
//	user.attributes  map<string, dyn>      (full Attributes map)
//	row              map<string, dyn>      (object Properties map)
//
// The expression must return a bool. true → MASK applies (caller receives
// the masked value). false → caller sees the clear value. This direction
// matches Foundry's "mask predicates" model: the expression is the trigger
// condition for the mask, not an allow-list. Empty expressions are handled
// by cellsec.Engine via the legacy AppliesTo allow-list path.
//
// PRD example (descriptive — accepted CEL syntax):
//
//	"PII" in user.markings || row.country == "CN"
package celmask

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
)

// ErrEmptyExpression is returned by Compile when expression is blank. Callers
// should branch on this rather than passing an empty string through; the
// engine treats empty as "no CEL gate, fall back to AppliesTo".
var ErrEmptyExpression = errors.New("celmask: expression is empty")

// Program is a compiled, type-checked CEL program ready for repeated Eval
// calls. Safe for concurrent use.
type Program struct {
	src string
	prg cel.Program
}

// Source returns the original expression string.
func (p *Program) Source() string {
	if p == nil {
		return ""
	}
	return p.src
}

// UserView is the canonical "user" binding shape passed to Eval. Keeping it
// a plain struct (rather than an opaque interface) lets the engine populate
// it from auth.User in one place and gives tests a stable wire shape.
type UserView struct {
	ID         string
	Email      string
	Roles      []string
	Markings   []string
	Attributes map[string]any
}

// AsMap returns the bindings shape the CEL runtime expects. Exposed for
// tests and benchmarks; engine code calls Eval directly.
func (u UserView) AsMap() map[string]any {
	roles := stringSliceCopy(u.Roles)
	markings := stringSliceCopy(u.Markings)
	attrs := u.Attributes
	if attrs == nil {
		attrs = map[string]any{}
	}
	return map[string]any{
		"id":         u.ID,
		"email":      u.Email,
		"roles":      roles,
		"markings":   markings,
		"attributes": attrs,
	}
}

// celEnv is the shared environment instance — building the env is the
// expensive part of cel-go (compiles type-checker tables) so we do it once
// at package init and reuse for every Compile.
var celEnv *cel.Env

func init() {
	env, err := cel.NewEnv(
		cel.Variable("user", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("row", cel.MapType(cel.StringType, cel.DynType)),
	)
	if err != nil {
		panic(fmt.Sprintf("celmask: build env: %v", err))
	}
	celEnv = env
}

// Compile parses, type-checks and lowers expression into a reusable Program.
// Returns ErrEmptyExpression for blank input and a wrapped error for any
// parse / check / program-construction failure. Programs are immutable and
// safe for concurrent use across goroutines.
func Compile(expression string) (*Program, error) {
	src := strings.TrimSpace(expression)
	if src == "" {
		return nil, ErrEmptyExpression
	}
	ast, iss := celEnv.Compile(src)
	if iss != nil && iss.Err() != nil {
		return nil, fmt.Errorf("celmask: compile %q: %w", src, iss.Err())
	}
	if ast.OutputType() != cel.BoolType {
		return nil, fmt.Errorf("celmask: expression %q must return bool, got %s", src, ast.OutputType().String())
	}
	prg, err := celEnv.Program(ast,
		cel.EvalOptions(cel.OptOptimize),
	)
	if err != nil {
		return nil, fmt.Errorf("celmask: program %q: %w", src, err)
	}
	return &Program{src: src, prg: prg}, nil
}

// Eval runs the compiled program against the user/row binding and returns
// the boolean verdict. Errors from missing bindings, type mismatches inside
// the expression, or runtime panics inside cel-go surface as a wrapped
// error; callers should treat any error as "fail closed" — i.e., apply the
// mask — to avoid leaking clear values when policy evaluation breaks.
func (p *Program) Eval(user UserView, row map[string]any) (bool, error) {
	if p == nil || p.prg == nil {
		return false, errors.New("celmask: nil program")
	}
	rowBinding := row
	if rowBinding == nil {
		rowBinding = map[string]any{}
	}
	out, _, err := p.prg.Eval(map[string]any{
		"user": user.AsMap(),
		"row":  rowBinding,
	})
	if err != nil {
		return false, fmt.Errorf("celmask: eval %q: %w", p.src, err)
	}
	switch v := out.(type) {
	case types.Bool:
		return bool(v), nil
	}
	val := out.Value()
	if b, ok := val.(bool); ok {
		return b, nil
	}
	return false, fmt.Errorf("celmask: expression %q returned non-bool %T", p.src, val)
}

// Validate parses and type-checks expression without retaining the program.
// Useful for admin-create handlers that want to reject invalid CEL up front
// without paying the program-construction cost.
func Validate(expression string) error {
	_, err := Compile(expression)
	return err
}

func stringSliceCopy(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}
