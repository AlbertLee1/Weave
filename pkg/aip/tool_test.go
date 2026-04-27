package aip

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestToolRegistry_RegisterAndGet(t *testing.T) {
	r := NewToolRegistry()
	r.Register(&EchoToolHandler{})
	got, err := r.Get("echo")
	if err != nil {
		t.Fatalf("Get(echo): %v", err)
	}
	if got.Definition().Name != "echo" {
		t.Errorf("Definition().Name = %q want echo", got.Definition().Name)
	}
}

func TestToolRegistry_GetMissing(t *testing.T) {
	r := NewToolRegistry()
	if _, err := r.Get("missing"); !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("expected ErrToolNotFound, got %v", err)
	}
}

func TestToolRegistry_NilSafe(t *testing.T) {
	var r *ToolRegistry
	if _, err := r.Get("echo"); !errors.Is(err, ErrToolNotFound) {
		t.Errorf("nil registry Get should return ErrToolNotFound, got %v", err)
	}
	if defs := r.Definitions(); defs != nil {
		t.Errorf("nil registry Definitions should be nil, got %v", defs)
	}
	r.Register(&EchoToolHandler{}) // no panic
}

func TestToolRegistry_DefinitionsSorted(t *testing.T) {
	r := NewToolRegistry()
	r.Register(stubTool{name: "zeta"})
	r.Register(stubTool{name: "alpha"})
	r.Register(stubTool{name: "mike"})
	defs := r.Definitions()
	if len(defs) != 3 {
		t.Fatalf("expected 3, got %d", len(defs))
	}
	for i := 1; i < len(defs); i++ {
		if defs[i].Name < defs[i-1].Name {
			t.Errorf("Definitions out of order: %v", defs)
		}
	}
}

func TestEchoToolHandler_Execute(t *testing.T) {
	h := &EchoToolHandler{}
	args, _ := json.Marshal(map[string]string{"text": "hello"})
	got, err := h.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got != "hello" {
		t.Errorf("Execute returned %q, want hello", got)
	}
}

func TestEchoToolHandler_EmptyArgs(t *testing.T) {
	h := &EchoToolHandler{}
	got, err := h.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got != "" {
		t.Errorf("Execute(nil) = %q, want empty", got)
	}
}

// stubTool is a minimal ToolHandler used to verify the registry's
// ordering / lookup contract without depending on EchoToolHandler.
type stubTool struct {
	name string
}

func (s stubTool) Definition() ToolDef        { return ToolDef{Name: s.name} }
func (stubTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "", nil
}
