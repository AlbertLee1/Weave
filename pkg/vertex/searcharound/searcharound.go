// Package searcharound implements custom Search Around — the Vertex
// right-click → Search Around → custom function flow that lets users
// register a Function (searchAroundCustom(objectRid, params) →
// neighborRids[]) and have Vertex expand a graph node by invoking it.
//
// This package is HTTP-free and runtime-agnostic: it depends only on an
// Executor interface that the caller wires to the real Function runtime
// (pkg/functions) or a mock in tests.
package searcharound

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Errors surfaced by Service. Tests use errors.Is to assert on shape.
var (
	// ErrInvalidRequest is returned when the input is missing required
	// fields (FunctionRID or ObjectRID).
	ErrInvalidRequest = errors.New("invalid search-around request")
	// ErrInvalidResult is returned when the executor returns a neighbor
	// list containing values that do not look like RIDs.
	ErrInvalidResult = errors.New("invalid search-around result")
)

// Request is the input to a Search Around invocation.
type Request struct {
	FunctionRID string
	ObjectRID   string
	Params      map[string]any
}

// Result is what the executor returns.
type Result struct {
	NeighborRIDs []string
}

// Executor is the runtime contract — given a Request it produces a Result
// (or an error). pkg/functions plugs in a goja-backed implementation; tests
// plug in a stub.
type Executor interface {
	Execute(ctx context.Context, req Request) (Result, error)
}

// Service is the bridge between the HTTP handler / Vertex frontend and the
// underlying Function runtime. Construct with NewService.
type Service struct {
	runner Executor
}

// NewService builds a Service that delegates to runner. Panics on nil
// runner — a nil runner is always a programmer error and never a runtime
// recoverable condition.
func NewService(runner Executor) *Service {
	if runner == nil {
		panic("searcharound: nil Executor")
	}
	return &Service{runner: runner}
}

// Execute validates the request, runs the function, and validates the
// returned neighbor list.
func (s *Service) Execute(ctx context.Context, req Request) (Result, error) {
	if req.FunctionRID == "" || req.ObjectRID == "" {
		return Result{}, ErrInvalidRequest
	}

	res, err := s.runner.Execute(ctx, req)
	if err != nil {
		return Result{}, fmt.Errorf("searcharound: runner: %w", err)
	}

	for _, rid := range res.NeighborRIDs {
		if !looksLikeRID(rid) {
			return Result{}, fmt.Errorf("%w: %q is not a RID", ErrInvalidResult, rid)
		}
	}
	return res, nil
}

// looksLikeRID is a lightweight shape check — full RID validation lives in
// pkg/rid, but importing it here would pull a heavier dep just to reject
// obvious garbage. The runtime contract is that neighbor RIDs come from
// trusted server-side lookups, so a prefix check is sufficient defence.
func looksLikeRID(s string) bool {
	return strings.HasPrefix(s, "ri.") && strings.Count(s, ".") >= 4
}
