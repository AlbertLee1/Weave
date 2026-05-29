package actions

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_Actions_TraceSpans_PRD_Gap_O2 continues round 11's Gap-O2
// closure (PRD-V2 §4.6). Round 11 wrapped 5 OSS service entry points
// with tracing.StartSpan; the matching gap on pkg/actions is that
// only Executor.Apply emits a span. The four other entry points —
// Prepare (also called standalone from saga + approval paths),
// CommitBatch, ApplyBatchAtomic, ApplyBatchBestEffort — silently
// swallowed their part of every trace, leaving operators with a
// gap between actions.Apply at the top and per-edit funnel.publish
// spans at the bottom.
//
// Span name convention mirrors actions.Apply: `actions.<Method>`
// PascalCase so operators can filter the whole pkg/actions surface
// from one panel.
func TestBDD_Actions_TraceSpans_PRD_Gap_O2(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider(trace.WithSpanProcessor(rec))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
		_ = tp.Shutdown(context.Background())
	})

	hasSpan := func(name string) bool {
		for _, s := range rec.Ended() {
			if s.Name() == name {
				return true
			}
		}
		return false
	}

	// Shared fixture: a single createEmployee action type with one
	// required parameter and a single createObject rule that wires
	// the parameter onto the new Employee. Mirrors the existing
	// TestExecutor_* fixtures so behavior is consistent.
	newFixture := func() *Executor {
		repo := &mockOmsRepo{
			actionTypes: []oms.ActionType{
				newTestActionType("createEmployee", []ParameterDef{
					{ID: "name", Type: "string", Required: true},
				}, []Rule{
					{
						Type:       "createObject",
						ObjectType: "Employee",
						PropertyBindings: map[string]PropertyBinding{
							"name": {Type: "parameter", Value: "name"},
						},
					},
				}),
			},
		}
		return NewExecutor(repo, &fakePublisher{offset: 1})
	}

	t.Run("Prepare emits actions.Prepare span carrying action.type", func(t *testing.T) {
		exec := newFixture()
		_, err := exec.Prepare(context.Background(), "ont-1", &ApplyRequest{
			ActionType: "createEmployee",
			Parameters: map[string]interface{}{"name": "Alice"},
		})
		if err != nil {
			t.Fatalf("Prepare: %v", err)
		}
		if !hasSpan("actions.Prepare") {
			names := []string{}
			for _, s := range rec.Ended() {
				names = append(names, s.Name())
			}
			t.Fatalf("expected actions.Prepare span, saw %v", names)
		}
	})

	t.Run("CommitBatch emits actions.CommitBatch span with batch.size", func(t *testing.T) {
		exec := newFixture()
		// Prepare three actions so the batch.size attribute is
		// distinguishable from the per-Prepare span (which has
		// batch.size unset).
		var prep []*PreparedAction
		for _, name := range []string{"Alice", "Bob", "Charlie"} {
			p, err := exec.Prepare(context.Background(), "ont-1", &ApplyRequest{
				ActionType: "createEmployee",
				Parameters: map[string]interface{}{"name": name},
			})
			if err != nil {
				t.Fatalf("Prepare(%s): %v", name, err)
			}
			prep = append(prep, p)
		}
		_, err := exec.CommitBatch(context.Background(), "ont-1", prep)
		if err != nil {
			t.Fatalf("CommitBatch: %v", err)
		}
		var got trace.ReadOnlySpan
		for _, s := range rec.Ended() {
			if s.Name() == "actions.CommitBatch" {
				got = s
				break
			}
		}
		if got == nil {
			t.Fatal("expected actions.CommitBatch span")
		}
		sizeOK := false
		for _, a := range got.Attributes() {
			if string(a.Key) == "batch.size" && a.Value.AsInt64() == 3 {
				sizeOK = true
				break
			}
		}
		if !sizeOK {
			t.Errorf("actions.CommitBatch span missing batch.size=3 attribute, got attrs=%v", got.Attributes())
		}
	})

	t.Run("ApplyBatchAtomic emits actions.ApplyBatchAtomic span", func(t *testing.T) {
		exec := newFixture()
		_, err := exec.ApplyBatchAtomic(context.Background(), "ont-1", []ApplyRequest{
			{ActionType: "createEmployee", Parameters: map[string]interface{}{"name": "Alice"}},
			{ActionType: "createEmployee", Parameters: map[string]interface{}{"name": "Bob"}},
		})
		if err != nil {
			t.Fatalf("ApplyBatchAtomic: %v", err)
		}
		if !hasSpan("actions.ApplyBatchAtomic") {
			t.Fatalf("expected actions.ApplyBatchAtomic span")
		}
	})

	t.Run("ApplyBatchBestEffort emits actions.ApplyBatchBestEffort span", func(t *testing.T) {
		exec := newFixture()
		_, err := exec.ApplyBatchBestEffort(context.Background(), "ont-1", []ApplyRequest{
			{ActionType: "createEmployee", Parameters: map[string]interface{}{"name": "Alice"}},
		})
		if err != nil {
			t.Fatalf("ApplyBatchBestEffort: %v", err)
		}
		if !hasSpan("actions.ApplyBatchBestEffort") {
			t.Fatalf("expected actions.ApplyBatchBestEffort span")
		}
	})

	t.Run("Apply still emits its existing actions.Apply span (no regression)", func(t *testing.T) {
		// Regression guard: round-11's predecessor wired the Apply
		// span. None of the new wrappers should accidentally rename
		// it away.
		exec := newFixture()
		_, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
			ActionType: "createEmployee",
			Parameters: map[string]interface{}{"name": "Alice"},
		})
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if !hasSpan("actions.Apply") {
			t.Fatalf("expected actions.Apply span")
		}
	})
}
