package searcharound

import (
	"context"
	"errors"
	"testing"
)

// fakeRunner is an in-memory Executor used to exercise the bridge logic
// without depending on the real Function runtime.
type fakeRunner struct {
	fn        func(ctx context.Context, req Request) (Result, error)
	callCount int
}

func (f *fakeRunner) Execute(ctx context.Context, req Request) (Result, error) {
	f.callCount++
	return f.fn(ctx, req)
}

func TestSearchAround_Given_ValidRids_When_Execute_Then_ReturnsNeighborRids(t *testing.T) {
	runner := &fakeRunner{
		fn: func(_ context.Context, req Request) (Result, error) {
			if req.FunctionRID != "ri.functions.main.function.fn1" {
				t.Fatalf("unexpected fn rid: %s", req.FunctionRID)
			}
			if req.ObjectRID != "ri.ontology.main.object.airport.JFK" {
				t.Fatalf("unexpected object rid: %s", req.ObjectRID)
			}
			return Result{NeighborRIDs: []string{
				"ri.ontology.main.object.airport.LAX",
				"ri.ontology.main.object.airport.SFO",
			}}, nil
		},
	}
	svc := NewService(runner)

	got, err := svc.Execute(context.Background(), Request{
		FunctionRID: "ri.functions.main.function.fn1",
		ObjectRID:   "ri.ontology.main.object.airport.JFK",
		Params:      map[string]any{"depth": 2},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.NeighborRIDs) != 2 {
		t.Fatalf("expected 2 neighbors, got %d", len(got.NeighborRIDs))
	}
	if got.NeighborRIDs[0] != "ri.ontology.main.object.airport.LAX" {
		t.Fatalf("wrong neighbor at 0: %s", got.NeighborRIDs[0])
	}
}

func TestSearchAround_Given_EmptyFunctionRID_When_Execute_Then_ReturnsErrInvalidRequest(t *testing.T) {
	svc := NewService(&fakeRunner{})
	_, err := svc.Execute(context.Background(), Request{
		ObjectRID: "ri.ontology.main.object.airport.JFK",
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestSearchAround_Given_EmptyObjectRID_When_Execute_Then_ReturnsErrInvalidRequest(t *testing.T) {
	svc := NewService(&fakeRunner{})
	_, err := svc.Execute(context.Background(), Request{
		FunctionRID: "ri.functions.main.function.fn1",
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestSearchAround_Given_RunnerError_When_Execute_Then_PropagatesError(t *testing.T) {
	want := errors.New("runtime crashed")
	runner := &fakeRunner{fn: func(_ context.Context, _ Request) (Result, error) {
		return Result{}, want
	}}
	svc := NewService(runner)
	_, err := svc.Execute(context.Background(), Request{
		FunctionRID: "ri.functions.main.function.fn1",
		ObjectRID:   "ri.ontology.main.object.airport.JFK",
	})
	if !errors.Is(err, want) {
		t.Fatalf("expected wrapped runtime error, got %v", err)
	}
}

func TestSearchAround_Given_NonRidNeighbor_When_Execute_Then_ReturnsErrInvalidResult(t *testing.T) {
	runner := &fakeRunner{fn: func(_ context.Context, _ Request) (Result, error) {
		return Result{NeighborRIDs: []string{"not-a-rid"}}, nil
	}}
	svc := NewService(runner)
	_, err := svc.Execute(context.Background(), Request{
		FunctionRID: "ri.functions.main.function.fn1",
		ObjectRID:   "ri.ontology.main.object.airport.JFK",
	})
	if !errors.Is(err, ErrInvalidResult) {
		t.Fatalf("expected ErrInvalidResult, got %v", err)
	}
}

func TestSearchAround_Given_Execute_When_RunnerCalled_Then_OnlyOnce(t *testing.T) {
	runner := &fakeRunner{fn: func(_ context.Context, _ Request) (Result, error) {
		return Result{NeighborRIDs: []string{}}, nil
	}}
	svc := NewService(runner)
	_, _ = svc.Execute(context.Background(), Request{
		FunctionRID: "ri.functions.main.function.fn1",
		ObjectRID:   "ri.ontology.main.object.airport.JFK",
	})
	if runner.callCount != 1 {
		t.Fatalf("expected runner invoked once, got %d", runner.callCount)
	}
}

func TestSearchAround_Given_NilRunner_When_NewService_Then_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil runner")
		}
	}()
	NewService(nil)
}
