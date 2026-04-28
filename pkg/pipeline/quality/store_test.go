package quality

import (
	"context"
	"testing"
	"time"
)

func mkViolation(id, rule, pipeline, run string, when time.Time) *Violation {
	return &Violation{
		ID:         id,
		PipelineID: pipeline,
		RunID:      run,
		RuleName:   rule,
		RuleType:   RuleNotNull,
		Field:      "f",
		Reason:     "x",
		DetectedAt: when,
	}
}

func TestMemoryViolationStore_InsertAndList(t *testing.T) {
	s := NewMemoryViolationStore()
	ctx := context.Background()
	t0 := time.Date(2026, 4, 28, 9, 0, 0, 0, time.UTC)
	a := mkViolation("a", "r1", "p1", "run1", t0)
	b := mkViolation("b", "r2", "p1", "run1", t0.Add(time.Second))
	c := mkViolation("c", "r1", "p2", "run2", t0.Add(2*time.Second))
	if err := s.InsertViolations(ctx, []*Violation{a, b, c}); err != nil {
		t.Fatal(err)
	}

	all, err := s.ListViolations(ctx, ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(all))
	}
	if all[0].ID != "c" || all[1].ID != "b" || all[2].ID != "a" {
		t.Errorf("expected newest-first ordering, got %v / %v / %v", all[0].ID, all[1].ID, all[2].ID)
	}
}

func TestMemoryViolationStore_FilterByPipeline(t *testing.T) {
	s := NewMemoryViolationStore()
	ctx := context.Background()
	t0 := time.Date(2026, 4, 28, 9, 0, 0, 0, time.UTC)
	_ = s.InsertViolation(ctx, mkViolation("a", "r1", "p1", "run1", t0))
	_ = s.InsertViolation(ctx, mkViolation("b", "r1", "p2", "run2", t0))
	out, _ := s.ListViolations(ctx, ListFilter{PipelineID: "p1"})
	if len(out) != 1 || out[0].PipelineID != "p1" {
		t.Fatalf("expected one row for p1, got %+v", out)
	}
}

func TestMemoryViolationStore_FilterByRunAndRule(t *testing.T) {
	s := NewMemoryViolationStore()
	ctx := context.Background()
	t0 := time.Date(2026, 4, 28, 9, 0, 0, 0, time.UTC)
	_ = s.InsertViolation(ctx, mkViolation("a", "r1", "p1", "run1", t0))
	_ = s.InsertViolation(ctx, mkViolation("b", "r2", "p1", "run1", t0))
	_ = s.InsertViolation(ctx, mkViolation("c", "r1", "p1", "run2", t0))
	out, _ := s.ListViolations(ctx, ListFilter{RunID: "run1", RuleName: "r1"})
	if len(out) != 1 || out[0].ID != "a" {
		t.Fatalf("expected exactly row a, got %+v", out)
	}
}

func TestMemoryViolationStore_Limit(t *testing.T) {
	s := NewMemoryViolationStore()
	ctx := context.Background()
	t0 := time.Date(2026, 4, 28, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		_ = s.InsertViolation(ctx, mkViolation("v"+string(rune('a'+i)), "r", "p", "run", t0.Add(time.Duration(i)*time.Second)))
	}
	out, _ := s.ListViolations(ctx, ListFilter{Limit: 2})
	if len(out) != 2 {
		t.Fatalf("expected limit=2 to truncate, got %d", len(out))
	}
}

func TestMemoryViolationStore_RejectsNilOrEmptyID(t *testing.T) {
	s := NewMemoryViolationStore()
	if err := s.InsertViolation(context.Background(), nil); err == nil {
		t.Error("expected error inserting nil")
	}
	if err := s.InsertViolation(context.Background(), &Violation{}); err == nil {
		t.Error("expected error inserting empty-id violation")
	}
}
