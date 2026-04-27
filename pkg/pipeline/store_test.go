package pipeline

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newTestPipeline(id string) *Pipeline {
	return &Pipeline{
		ID: id,
		Inputs: []Input{
			{Name: "src", Type: "objectset"},
		},
		Outputs: []Output{
			{Name: "sink", Type: "jdbc", Input: "src"},
		},
		Enabled: true,
	}
}

func TestMemoryStore_Create_Get(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	p := newTestPipeline("demo")
	if err := s.CreatePipeline(ctx, p); err != nil {
		t.Fatalf("CreatePipeline: %v", err)
	}
	if p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() {
		t.Fatal("timestamps not stamped")
	}
	got, err := s.GetPipeline(ctx, "demo")
	if err != nil {
		t.Fatalf("GetPipeline: %v", err)
	}
	if got.ID != "demo" {
		t.Fatalf("ID = %q, want %q", got.ID, "demo")
	}
	got.Inputs[0].Name = "mutated"
	again, _ := s.GetPipeline(ctx, "demo")
	if again.Inputs[0].Name == "mutated" {
		t.Fatal("GetPipeline returned a shared pointer")
	}
}

func TestMemoryStore_Create_Duplicate(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	if err := s.CreatePipeline(ctx, newTestPipeline("demo")); err != nil {
		t.Fatalf("first CreatePipeline: %v", err)
	}
	err := s.CreatePipeline(ctx, newTestPipeline("demo"))
	if !errors.Is(err, ErrPipelineAlreadyExists) {
		t.Fatalf("err = %v, want ErrPipelineAlreadyExists", err)
	}
}

func TestMemoryStore_Get_NotFound(t *testing.T) {
	s := NewMemoryStore()
	_, err := s.GetPipeline(context.Background(), "nope")
	if !errors.Is(err, ErrPipelineNotFound) {
		t.Fatalf("err = %v, want ErrPipelineNotFound", err)
	}
}

func TestMemoryStore_List_Filter(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	older := newTestPipeline("a")
	older.CreatedBy = "alice"
	older.CreatedAt = time.Now().Add(-time.Hour)
	older.UpdatedAt = older.CreatedAt
	newer := newTestPipeline("b")
	newer.CreatedBy = "bob"
	newer.CreatedAt = time.Now()
	newer.UpdatedAt = newer.CreatedAt
	if err := s.CreatePipeline(ctx, older); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.CreatePipeline(ctx, newer); err != nil {
		t.Fatalf("create: %v", err)
	}
	all, err := s.ListPipelines(ctx, "")
	if err != nil {
		t.Fatalf("ListPipelines: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("len(all) = %d, want 2", len(all))
	}
	if all[0].ID != "b" {
		t.Fatalf("first id = %q, want %q (newest first)", all[0].ID, "b")
	}
	mine, err := s.ListPipelines(ctx, "alice")
	if err != nil {
		t.Fatalf("ListPipelines(alice): %v", err)
	}
	if len(mine) != 1 || mine[0].ID != "a" {
		t.Fatalf("alice scope = %v, want [a]", mine)
	}
}

func TestMemoryStore_Update_Partial(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	if err := s.CreatePipeline(ctx, newTestPipeline("demo")); err != nil {
		t.Fatalf("create: %v", err)
	}
	name := "Renamed"
	disabled := false
	upd := PipelineUpdate{Name: &name, Enabled: &disabled}
	if err := s.UpdatePipeline(ctx, "demo", upd); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ := s.GetPipeline(ctx, "demo")
	if got.Name != "Renamed" {
		t.Fatalf("Name = %q, want %q", got.Name, "Renamed")
	}
	if got.Enabled {
		t.Fatal("Enabled should now be false")
	}
	// Description / Inputs were not touched: must still hold their
	// original values.
	if len(got.Inputs) != 1 || got.Inputs[0].Name != "src" {
		t.Fatalf("Inputs mutated unexpectedly: %v", got.Inputs)
	}
}

func TestMemoryStore_Update_NotFound(t *testing.T) {
	s := NewMemoryStore()
	err := s.UpdatePipeline(context.Background(), "missing", PipelineUpdate{})
	if !errors.Is(err, ErrPipelineNotFound) {
		t.Fatalf("err = %v, want ErrPipelineNotFound", err)
	}
}

func TestMemoryStore_Delete(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	if err := s.CreatePipeline(ctx, newTestPipeline("demo")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.DeletePipeline(ctx, "demo"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := s.DeletePipeline(ctx, "demo"); !errors.Is(err, ErrPipelineNotFound) {
		t.Fatalf("second Delete err = %v, want ErrPipelineNotFound", err)
	}
}
