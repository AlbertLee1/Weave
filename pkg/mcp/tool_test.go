package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

func TestTool_ValidateArgs_Required(t *testing.T) {
	def := ToolDefinition{
		Name: "test_tool",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"name": {Type: "string", Description: "name"},
				"age":  {Type: "integer", Description: "age"},
			},
			Required: []string{"name"},
		},
	}
	// Missing required.
	if err := def.ValidateArgs(map[string]any{"age": 5}); err == nil {
		t.Fatalf("expected error for missing required arg")
	}
	// Present required.
	if err := def.ValidateArgs(map[string]any{"name": "alice"}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTool_ValidateArgs_TypeMismatch(t *testing.T) {
	def := ToolDefinition{
		Name: "test_tool",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"name":     {Type: "string"},
				"count":    {Type: "integer"},
				"flag":     {Type: "boolean"},
				"meta":     {Type: "object"},
				"settings": {Type: "object"},
			},
			Required: []string{"name"},
		},
	}
	// String got integer.
	if err := def.ValidateArgs(map[string]any{"name": 42}); err == nil {
		t.Errorf("expected error for string field with int value")
	}
	// Integer got string.
	if err := def.ValidateArgs(map[string]any{"name": "x", "count": "five"}); err == nil {
		t.Errorf("expected error for integer field with string value")
	}
	// Boolean got string.
	if err := def.ValidateArgs(map[string]any{"name": "x", "flag": "true"}); err == nil {
		t.Errorf("expected error for boolean field with string value")
	}
	// Object got string.
	if err := def.ValidateArgs(map[string]any{"name": "x", "meta": "not an object"}); err == nil {
		t.Errorf("expected error for object field with string value")
	}
	// Valid integer (json.Number/float64 path).
	if err := def.ValidateArgs(map[string]any{"name": "x", "count": float64(7)}); err != nil {
		t.Errorf("unexpected error for valid float64 int: %v", err)
	}
}

// fakeTool is a tool used to verify Tool interface plumbing.
type fakeTool struct {
	def    ToolDefinition
	called int
	last   map[string]any
}

func (f *fakeTool) Definition() ToolDefinition { return f.def }
func (f *fakeTool) Call(ctx context.Context, args map[string]any) (*ToolResult, error) {
	f.called++
	f.last = args
	return &ToolResult{Content: []Content{{Type: "text", Text: "ok"}}}, nil
}

func TestTool_InterfaceCall(t *testing.T) {
	tool := &fakeTool{def: ToolDefinition{Name: "demo"}}
	res, err := tool.Call(context.Background(), map[string]any{"x": 1})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if tool.called != 1 {
		t.Errorf("called = %d", tool.called)
	}
	if len(res.Content) != 1 || res.Content[0].Text != "ok" {
		t.Errorf("Content = %+v", res.Content)
	}
	// Definition is JSON-marshalable.
	if _, err := json.Marshal(tool.Definition()); err != nil {
		t.Errorf("definition marshal: %v", err)
	}
}
