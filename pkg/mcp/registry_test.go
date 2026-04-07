package mcp

import (
	"context"
	"errors"
	"testing"
)

func newStubTool(name string) *fakeTool {
	return &fakeTool{
		def: ToolDefinition{
			Name:        name,
			Description: "stub " + name,
			InputSchema: InputSchema{Type: "object", Properties: map[string]PropertySchema{}},
		},
	}
}

func TestRegistry_Register_Twice_Fails(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(newStubTool("alpha")); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := r.Register(newStubTool("alpha"))
	if err == nil {
		t.Fatalf("second Register should have failed")
	}
	if !errors.Is(err, ErrToolAlreadyRegistered) {
		t.Errorf("got %v, want ErrToolAlreadyRegistered", err)
	}
}

func TestRegistry_Call_UnknownTool(t *testing.T) {
	r := NewRegistry()
	_, err := r.Call(context.Background(), "missing", map[string]any{})
	if err == nil {
		t.Fatalf("expected error for unknown tool")
	}
	if !errors.Is(err, ErrToolNotFound) {
		t.Errorf("got %v, want ErrToolNotFound", err)
	}
}

func TestRegistry_List_ReturnsAllTools(t *testing.T) {
	r := NewRegistry()
	for _, name := range []string{"beta", "alpha", "gamma"} {
		if err := r.Register(newStubTool(name)); err != nil {
			t.Fatalf("Register(%s): %v", name, err)
		}
	}
	defs := r.List()
	if len(defs) != 3 {
		t.Fatalf("len(List) = %d, want 3", len(defs))
	}
	// Order should be deterministic (sorted by name).
	want := []string{"alpha", "beta", "gamma"}
	for i, w := range want {
		if defs[i].Name != w {
			t.Errorf("List[%d] = %s, want %s", i, defs[i].Name, w)
		}
	}
}

func TestRegistry_Call_DispatchesToTool(t *testing.T) {
	r := NewRegistry()
	tool := newStubTool("echo")
	if err := r.Register(tool); err != nil {
		t.Fatalf("Register: %v", err)
	}
	_, err := r.Call(context.Background(), "echo", map[string]any{"msg": "hi"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if tool.called != 1 {
		t.Errorf("called = %d, want 1", tool.called)
	}
	if tool.last["msg"] != "hi" {
		t.Errorf("last = %+v", tool.last)
	}
}
