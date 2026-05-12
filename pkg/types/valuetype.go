package types

import (
	"errors"
	"fmt"
)

// ErrValueTypeCycle is returned by ResolveValueType when a cycle is detected
// while expanding a chain of ValueType references. The wrapped path captures
// the cycle for diagnostics: e.g. "EmailV2 -> EmailV1 -> EmailV2".
var ErrValueTypeCycle = errors.New("value type reference cycle")

// ErrValueTypeNotFound is returned when a referenced ValueType apiName is not
// present in the registry passed to ResolveValueType.
var ErrValueTypeNotFound = errors.New("value type not found")

// ErrValueTypeDepthExceeded is returned when reference expansion exceeds the
// configured depth limit (defends against accidental deep aliasing).
var ErrValueTypeDepthExceeded = errors.New("value type reference depth exceeded")

// DefaultMaxValueTypeDepth is the inclusive ceiling on reference indirection.
// 32 is comfortably above realistic alias chains and well below a stack risk.
const DefaultMaxValueTypeDepth = 32

// ValueTypeDef is a reusable, named alias for a BaseType paired with optional
// constraints. The BaseType field may carry either a real BaseType (e.g. "string")
// or the apiName of another ValueTypeDef — ResolveValueType walks the chain.
//
// This mirrors pkg/oms.ValueType but is decoupled from persistence so the
// type-system primitives (coerce / validate / merge) can be exercised in
// pkg/types unit tests without dragging in a DB.
type ValueTypeDef struct {
	APIName     string
	BaseType    string
	Constraints []byte
}

// ResolvedValueType is the terminal form produced by ResolveValueType: an
// actual BaseType plus the constraint blobs accumulated along the chain
// (outermost-first so callers can layer them).
type ResolvedValueType struct {
	BaseType    BaseType
	Constraints [][]byte
}

// ResolveValueType walks the alias chain starting at apiName until it lands on
// a real BaseType. Each step looks the current name up in registry; if the
// step's BaseType is itself a registered apiName, recursion continues.
//
// Cycle detection: every visited apiName is tracked in a path slice; a repeat
// returns ErrValueTypeCycle with the offending path embedded in the message
// (e.g. "ValueTypeDef[A -> B -> A]: value type reference cycle").
//
// Depth limit: maxDepth ≤ 0 falls back to DefaultMaxValueTypeDepth.
func ResolveValueType(apiName string, registry map[string]ValueTypeDef, maxDepth int) (ResolvedValueType, error) {
	if maxDepth <= 0 {
		maxDepth = DefaultMaxValueTypeDepth
	}
	var (
		path        = make([]string, 0, 4)
		seen        = make(map[string]int, 4)
		constraints [][]byte
		current     = apiName
	)
	for i := 0; i <= maxDepth; i++ {
		if pos, hit := seen[current]; hit {
			cycle := append([]string{}, path[pos:]...)
			cycle = append(cycle, current)
			return ResolvedValueType{}, fmt.Errorf("ValueTypeDef[%s]: %w", joinPath(cycle), ErrValueTypeCycle)
		}
		def, ok := registry[current]
		if !ok {
			return ResolvedValueType{}, fmt.Errorf("ValueTypeDef[%s]: %w", current, ErrValueTypeNotFound)
		}
		seen[current] = len(path)
		path = append(path, current)
		if len(def.Constraints) > 0 {
			constraints = append(constraints, def.Constraints)
		}
		if bt := BaseType(def.BaseType); bt.IsValid() {
			return ResolvedValueType{BaseType: bt, Constraints: constraints}, nil
		}
		if def.BaseType == "" {
			return ResolvedValueType{}, fmt.Errorf("ValueTypeDef[%s]: empty baseType", current)
		}
		current = def.BaseType
	}
	return ResolvedValueType{}, fmt.Errorf("ValueTypeDef[%s -> %s]: %w (limit=%d)", joinPath(path), current, ErrValueTypeDepthExceeded, maxDepth)
}

func joinPath(p []string) string {
	out := ""
	for i, name := range p {
		if i > 0 {
			out += " -> "
		}
		out += name
	}
	return out
}
