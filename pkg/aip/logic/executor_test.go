package logic

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/aip"
)

// stubProviderResolver lets tests register a single fake aip.Provider
// without standing up the full aip.Registry.
type stubProviderResolver struct {
	provider aip.Provider
}

func (s *stubProviderResolver) Get(name string) (aip.Provider, error) {
	if s == nil || s.provider == nil {
		return nil, fmt.Errorf("no provider for %q", name)
	}
	if s.provider.Name() != name {
		return nil, fmt.Errorf("no provider for %q", name)
	}
	return s.provider, nil
}

// fakeProvider records every Complete call and returns a canned reply.
type fakeProvider struct {
	name  string
	reply string
	calls []aip.ChatRequest
	err   error
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Complete(_ context.Context, req aip.ChatRequest) (*aip.ChatResponse, error) {
	f.calls = append(f.calls, req)
	if f.err != nil {
		return nil, f.err
	}
	return &aip.ChatResponse{Content: f.reply, Model: req.Model, TokenCount: len(f.reply) / 4}, nil
}

func TestExecutor_LinearFlowLLMThenOutput(t *testing.T) {
	prov := &fakeProvider{name: "mock", reply: "hello world"}
	exec := NewExecutor(&stubProviderResolver{provider: prov}, NewMapToolRegistry())

	flow := &Flow{
		ID:   "flow_simple",
		Name: "simple",
		Nodes: []Node{
			{ID: "summary", Type: NodeTypeLLM, Config: map[string]any{
				"provider":       "mock",
				"promptTemplate": "Summarize: {{input.text}}",
			}},
			{ID: "out", Type: NodeTypeOutput, Config: map[string]any{
				"keys": []any{"summary.content"},
			}},
		},
		Edges: []Edge{{From: "summary", To: "out"}},
	}

	run, err := exec.Execute(context.Background(), flow, map[string]any{"text": "hello"})
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if run.Status != RunStatusSuccess {
		t.Fatalf("expected success, got %q (err=%s)", run.Status, run.Error)
	}
	if len(prov.calls) != 1 {
		t.Fatalf("expected 1 LLM call, got %d", len(prov.calls))
	}
	if got := prov.calls[0].Messages[len(prov.calls[0].Messages)-1].Content; got != "Summarize: hello" {
		t.Errorf("prompt substitution failed: got %q", got)
	}
	if v, _ := run.Output["summary.content"].(string); v != "hello world" {
		t.Errorf("output[summary.content] = %v, want %q", run.Output, "hello world")
	}
}

func TestExecutor_DAGOrder_Diamond(t *testing.T) {
	prov := &fakeProvider{name: "mock", reply: "x"}
	exec := NewExecutor(&stubProviderResolver{provider: prov}, NewMapToolRegistry())

	flow := &Flow{
		ID: "flow_diamond",
		Nodes: []Node{
			{ID: "a", Type: NodeTypeLLM, Config: map[string]any{"provider": "mock", "promptTemplate": "first"}},
			{ID: "b", Type: NodeTypeTool, Config: map[string]any{"tool": "echo", "params": map[string]any{"text": "{{a.content}}"}}},
			{ID: "c", Type: NodeTypeTool, Config: map[string]any{"tool": "echo", "params": map[string]any{"text": "after-a"}}},
			{ID: "d", Type: NodeTypeOutput, Config: map[string]any{"keys": []any{"b.text", "c.text"}}},
		},
		Edges: []Edge{
			{From: "a", To: "b"},
			{From: "a", To: "c"},
			{From: "b", To: "d"},
			{From: "c", To: "d"},
		},
	}

	run, err := exec.Execute(context.Background(), flow, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if run.Output["b.text"] != "x" {
		t.Errorf("b output: %v", run.Output)
	}
	if run.Output["c.text"] != "after-a" {
		t.Errorf("c output: %v", run.Output)
	}
}

func TestExecutor_IfNodeBranchesAndSkipsDownstream(t *testing.T) {
	prov := &fakeProvider{name: "mock", reply: "ignored"}
	tools := NewMapToolRegistry()
	exec := NewExecutor(&stubProviderResolver{provider: prov}, tools)

	flow := &Flow{
		ID: "flow_if",
		Nodes: []Node{
			{ID: "decide", Type: NodeTypeIf, Config: map[string]any{
				"condition": "{{input.threshold}} > 5",
			}},
			{ID: "trueBranch", Type: NodeTypeTool, Config: map[string]any{
				"tool": "echo", "params": map[string]any{"text": "yes"},
			}},
			{ID: "falseBranch", Type: NodeTypeTool, Config: map[string]any{
				"tool": "echo", "params": map[string]any{"text": "no"},
			}},
			{ID: "out", Type: NodeTypeOutput, Config: map[string]any{
				"keys": []any{"trueBranch.text", "falseBranch.text"},
			}},
		},
		Edges: []Edge{
			{From: "decide", To: "trueBranch", Branch: BranchTrue},
			{From: "decide", To: "falseBranch", Branch: BranchFalse},
			{From: "trueBranch", To: "out"},
			{From: "falseBranch", To: "out"},
		},
	}

	t.Run("trueBranch", func(t *testing.T) {
		run, err := exec.Execute(context.Background(), flow, map[string]any{"threshold": 10})
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if run.Status != RunStatusSuccess {
			t.Fatalf("status=%q err=%q", run.Status, run.Error)
		}
		if got, _ := run.Output["trueBranch.text"].(string); got != "yes" {
			t.Errorf("expected yes, got %v", run.Output)
		}
		if _, has := run.Output["falseBranch.text"]; has {
			t.Errorf("false branch should be skipped, but appeared in output: %v", run.Output)
		}
		var skippedFalse bool
		for _, te := range run.Trace {
			if te.NodeID == "falseBranch" && te.Status == TraceStatusSkipped {
				skippedFalse = true
			}
		}
		if !skippedFalse {
			t.Errorf("expected false branch trace=skipped; trace=%+v", run.Trace)
		}
	})

	t.Run("falseBranch", func(t *testing.T) {
		run, err := exec.Execute(context.Background(), flow, map[string]any{"threshold": 1})
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if got, _ := run.Output["falseBranch.text"].(string); got != "no" {
			t.Errorf("expected no, got %v", run.Output)
		}
		if _, has := run.Output["trueBranch.text"]; has {
			t.Errorf("true branch should be skipped: %v", run.Output)
		}
	})
}

func TestExecutor_ToolNotRegisteredFails(t *testing.T) {
	exec := NewExecutor(nil, NewMapToolRegistry())
	flow := &Flow{
		ID: "flow_tool_missing",
		Nodes: []Node{
			{ID: "n1", Type: NodeTypeTool, Config: map[string]any{"tool": "doesnotexist"}},
			{ID: "out", Type: NodeTypeOutput, Config: map[string]any{}},
		},
		Edges: []Edge{{From: "n1", To: "out"}},
	}
	run, err := exec.Execute(context.Background(), flow, nil)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, ErrToolNotFound) {
		t.Errorf("expected ErrToolNotFound, got %v", err)
	}
	if run.Status != RunStatusFailed {
		t.Errorf("expected status=failed, got %q", run.Status)
	}
	if run.Error == "" || !strings.Contains(run.Error, "doesnotexist") {
		t.Errorf("expected error to mention tool name; got %q", run.Error)
	}
}

func TestExecutor_ProviderErrorPropagates(t *testing.T) {
	prov := &fakeProvider{name: "mock", err: errors.New("upstream went sideways")}
	exec := NewExecutor(&stubProviderResolver{provider: prov}, NewMapToolRegistry())
	flow := &Flow{
		ID: "flow_err",
		Nodes: []Node{
			{ID: "n1", Type: NodeTypeLLM, Config: map[string]any{"provider": "mock", "promptTemplate": "x"}},
			{ID: "out", Type: NodeTypeOutput, Config: map[string]any{}},
		},
		Edges: []Edge{{From: "n1", To: "out"}},
	}
	run, err := exec.Execute(context.Background(), flow, nil)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if run.Status != RunStatusFailed {
		t.Errorf("expected failed status, got %q", run.Status)
	}
	if !strings.Contains(run.Error, "upstream") {
		t.Errorf("expected upstream error to appear; got %q", run.Error)
	}
}

func TestExecutor_OutputDefaultReturnsAllNodes(t *testing.T) {
	prov := &fakeProvider{name: "mock", reply: "x"}
	exec := NewExecutor(&stubProviderResolver{provider: prov}, NewMapToolRegistry())
	flow := &Flow{
		ID: "flow_default_output",
		Nodes: []Node{
			{ID: "a", Type: NodeTypeLLM, Config: map[string]any{"provider": "mock", "promptTemplate": "p"}},
		},
		Edges: nil,
	}
	run, err := exec.Execute(context.Background(), flow, map[string]any{"in": 1})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	// No output node ⇒ default output mirrors state.
	if _, ok := run.Output["a"].(map[string]any); !ok {
		t.Errorf("expected default output to include node a; got %v", run.Output)
	}
	if _, has := run.Output["input"]; has {
		t.Errorf("default output must not leak the input alias")
	}
}

func TestExecutor_RejectsCycle(t *testing.T) {
	exec := NewExecutor(nil, nil)
	flow := &Flow{
		ID: "f",
		Nodes: []Node{
			{ID: "a", Type: NodeTypeOutput, Config: map[string]any{}},
			{ID: "b", Type: NodeTypeOutput, Config: map[string]any{}},
		},
		Edges: []Edge{{From: "a", To: "b"}, {From: "b", To: "a"}},
	}
	run, err := exec.Execute(context.Background(), flow, nil)
	if err == nil {
		t.Fatalf("expected error, got run=%+v", run)
	}
}
