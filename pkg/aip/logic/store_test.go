package logic

import (
	"context"
	"errors"
	"testing"
)

func newTestFlow(id string) *Flow {
	return &Flow{
		ID:        id,
		Name:      "test",
		Nodes:     []Node{{ID: "n1", Type: NodeTypeOutput, Config: map[string]any{"keys": []any{}}}},
		Edges:     nil,
		CreatedBy: "user:alice@example.com",
	}
}

func TestMemoryStore_CreateAndGet(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	f := newTestFlow("flow_a")
	if err := s.CreateFlow(ctx, f); err != nil {
		t.Fatalf("CreateFlow: %v", err)
	}
	got, err := s.GetFlow(ctx, "flow_a")
	if err != nil {
		t.Fatalf("GetFlow: %v", err)
	}
	if got.ID != "flow_a" {
		t.Errorf("id mismatch")
	}
	if err := s.CreateFlow(ctx, f); !errors.Is(err, ErrFlowAlreadyExists) {
		t.Errorf("expected ErrFlowAlreadyExists, got %v", err)
	}
}

func TestMemoryStore_GetNotFound(t *testing.T) {
	s := NewMemoryStore()
	if _, err := s.GetFlow(context.Background(), "missing"); !errors.Is(err, ErrFlowNotFound) {
		t.Errorf("expected ErrFlowNotFound, got %v", err)
	}
}

func TestMemoryStore_ListByOwner(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	a := newTestFlow("flow_a")
	b := newTestFlow("flow_b")
	b.CreatedBy = "user:bob@example.com"
	if err := s.CreateFlow(ctx, a); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if err := s.CreateFlow(ctx, b); err != nil {
		t.Fatalf("create b: %v", err)
	}

	all, _ := s.ListFlows(ctx, "")
	if len(all) != 2 {
		t.Fatalf("expected 2 flows, got %d", len(all))
	}

	mine, _ := s.ListFlows(ctx, "user:alice@example.com")
	if len(mine) != 1 || mine[0].ID != "flow_a" {
		t.Fatalf("expected only flow_a for alice, got %+v", mine)
	}
}

func TestMemoryStore_PartialUpdate(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	f := newTestFlow("flow_a")
	if err := s.CreateFlow(ctx, f); err != nil {
		t.Fatalf("create: %v", err)
	}
	newName := "renamed"
	if err := s.UpdateFlow(ctx, "flow_a", FlowUpdate{Name: &newName}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := s.GetFlow(ctx, "flow_a")
	if got.Name != "renamed" {
		t.Errorf("name not updated: %q", got.Name)
	}
	if got.Description != "" {
		t.Errorf("description should remain empty: %q", got.Description)
	}
}

func TestMemoryStore_RunsCascadeOnDelete(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	if err := s.CreateFlow(ctx, newTestFlow("f1")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.AppendRun(ctx, &Run{FlowID: "f1", Status: RunStatusSuccess}); err != nil {
		t.Fatalf("append run: %v", err)
	}
	if err := s.AppendRun(ctx, &Run{FlowID: "f1", Status: RunStatusFailed}); err != nil {
		t.Fatalf("append run 2: %v", err)
	}
	runs, err := s.ListRuns(ctx, "f1", 0)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 2 || runs[0].ID < runs[1].ID {
		t.Fatalf("expected 2 runs newest-first, got %+v", runs)
	}
	if err := s.DeleteFlow(ctx, "f1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.ListRuns(ctx, "f1", 0); !errors.Is(err, ErrFlowNotFound) {
		t.Errorf("expected ErrFlowNotFound after delete, got %v", err)
	}
}

func TestMemoryStore_AppendRunRejectsUnknownFlow(t *testing.T) {
	s := NewMemoryStore()
	if err := s.AppendRun(context.Background(), &Run{FlowID: "missing"}); !errors.Is(err, ErrFlowNotFound) {
		t.Errorf("expected ErrFlowNotFound, got %v", err)
	}
}

func TestMemoryStore_ListRunsLimit(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	if err := s.CreateFlow(ctx, newTestFlow("f1")); err != nil {
		t.Fatalf("create: %v", err)
	}
	for i := 0; i < 5; i++ {
		if err := s.AppendRun(ctx, &Run{FlowID: "f1"}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	runs, _ := s.ListRuns(ctx, "f1", 2)
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}
	if runs[0].ID < runs[1].ID {
		t.Errorf("expected newest first")
	}
}
