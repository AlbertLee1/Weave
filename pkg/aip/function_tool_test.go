package aip

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// stubFunctionInvoker is a minimal FunctionInvoker for tests.
type stubFunctionInvoker struct {
	calledRID string
	gotParams map[string]interface{}
	result    interface{}
	err       error
}

func (s *stubFunctionInvoker) Invoke(_ context.Context, rid string, params map[string]interface{}) (interface{}, error) {
	s.calledRID = rid
	s.gotParams = params
	return s.result, s.err
}

func TestFunctionToolHandler_DefinitionEchoesRecord(t *testing.T) {
	rec := &ToolRecord{
		Name:        "lookup",
		Description: "look up an object",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}}}`),
	}
	h := NewFunctionToolHandler(rec, nil)
	def := h.Definition()
	if def.Name != "lookup" || def.Description != "look up an object" {
		t.Errorf("Definition() = %+v", def)
	}
	if string(def.Parameters) != string(rec.Parameters) {
		t.Errorf("Definition().Parameters = %q want %q", def.Parameters, rec.Parameters)
	}
}

func TestFunctionToolHandler_ExecuteDispatchesToInvoker(t *testing.T) {
	inv := &stubFunctionInvoker{result: "hello"}
	rec := &ToolRecord{Name: "lookup", HandlerFunctionRID: "ri.functions.main.fn.abc"}
	h := NewFunctionToolHandler(rec, inv)
	args := json.RawMessage(`{"id":"42"}`)
	got, err := h.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got != "hello" {
		t.Errorf("Execute = %q want hello", got)
	}
	if inv.calledRID != "ri.functions.main.fn.abc" {
		t.Errorf("Invoke RID = %q", inv.calledRID)
	}
	if v, _ := inv.gotParams["id"].(string); v != "42" {
		t.Errorf("Invoke params id = %v", inv.gotParams)
	}
}

func TestFunctionToolHandler_ExecuteEmptyArgsPassesEmptyMap(t *testing.T) {
	inv := &stubFunctionInvoker{result: ""}
	rec := &ToolRecord{Name: "noop", HandlerFunctionRID: "ri.functions.main.fn.x"}
	h := NewFunctionToolHandler(rec, inv)
	if _, err := h.Execute(context.Background(), nil); err != nil {
		t.Fatalf("Execute(nil): %v", err)
	}
	if inv.gotParams == nil {
		t.Errorf("expected non-nil params, got nil")
	}
}

func TestFunctionToolHandler_ExecuteRejectsInvalidArgs(t *testing.T) {
	inv := &stubFunctionInvoker{}
	rec := &ToolRecord{Name: "bad", HandlerFunctionRID: "ri.functions.main.fn.x"}
	h := NewFunctionToolHandler(rec, inv)
	if _, err := h.Execute(context.Background(), json.RawMessage(`not json`)); err == nil {
		t.Fatal("expected error from bad json args")
	}
}

func TestFunctionToolHandler_NoHandlerFunction(t *testing.T) {
	rec := &ToolRecord{Name: "lookup"} // empty HandlerFunctionRID
	h := NewFunctionToolHandler(rec, &stubFunctionInvoker{result: "ok"})
	_, err := h.Execute(context.Background(), nil)
	if !errors.Is(err, ErrToolHandlerNotConfigured) {
		t.Fatalf("want ErrToolHandlerNotConfigured, got %v", err)
	}
}

func TestFunctionToolHandler_NoInvoker(t *testing.T) {
	rec := &ToolRecord{Name: "lookup", HandlerFunctionRID: "ri.functions.main.fn.x"}
	h := NewFunctionToolHandler(rec, nil)
	_, err := h.Execute(context.Background(), nil)
	if !errors.Is(err, ErrToolHandlerNotConfigured) {
		t.Fatalf("want ErrToolHandlerNotConfigured, got %v", err)
	}
}

func TestFunctionToolHandler_PropagatesInvokerError(t *testing.T) {
	inv := &stubFunctionInvoker{err: errors.New("boom")}
	rec := &ToolRecord{Name: "fail", HandlerFunctionRID: "ri.functions.main.fn.x"}
	h := NewFunctionToolHandler(rec, inv)
	_, err := h.Execute(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected wrapped error, got %v", err)
	}
}

func TestFunctionToolHandler_ResultStringification(t *testing.T) {
	cases := []struct {
		name   string
		result interface{}
		want   string
	}{
		{"plain-string", "hello", "hello"},
		{"int", 42, "42"},
		{"map", map[string]interface{}{"k": "v"}, `{"k":"v"}`},
		{"slice", []interface{}{1, 2}, "[1,2]"},
		{"nil", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inv := &stubFunctionInvoker{result: tc.result}
			rec := &ToolRecord{Name: "x", HandlerFunctionRID: "ri.functions.main.fn.x"}
			h := NewFunctionToolHandler(rec, inv)
			got, err := h.Execute(context.Background(), nil)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if got != tc.want {
				t.Errorf("Execute() = %q want %q", got, tc.want)
			}
		})
	}
}

func TestLoadCatalogIntoRegistry(t *testing.T) {
	cat := NewMemoryToolCatalog()
	_ = cat.CreateTool(context.Background(), &ToolRecord{
		Name:               "enabled_tool",
		HandlerFunctionRID: "ri.functions.main.fn.a",
		Enabled:            true,
	})
	_ = cat.CreateTool(context.Background(), &ToolRecord{
		Name:               "disabled_tool",
		HandlerFunctionRID: "ri.functions.main.fn.b",
		Enabled:            false,
	})
	reg := NewToolRegistry()
	reg.Register(&EchoToolHandler{})
	inv := &stubFunctionInvoker{result: "ok"}
	if err := LoadCatalogIntoRegistry(context.Background(), reg, cat, inv); err != nil {
		t.Fatalf("LoadCatalogIntoRegistry: %v", err)
	}
	names := reg.Names()
	wantPresent := map[string]bool{"echo": true, "enabled_tool": true}
	wantAbsent := map[string]bool{"disabled_tool": true}
	for _, n := range names {
		if wantAbsent[n] {
			t.Errorf("disabled tool %q should not be registered", n)
		}
	}
	for n := range wantPresent {
		found := false
		for _, got := range names {
			if got == n {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected tool %q in registry, got %v", n, names)
		}
	}
}
