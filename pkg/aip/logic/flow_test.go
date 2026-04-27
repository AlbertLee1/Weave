package logic

import (
	"strings"
	"testing"
)

func TestValidateFlowID(t *testing.T) {
	cases := []struct {
		id      string
		wantErr bool
	}{
		{"flow_abc", false},
		{"summarise.v1", false},
		{"a-b_c.d", false},
		{"", true},
		{"contains space", true},
		{"with/slash", true},
		{strings.Repeat("a", 129), true},
	}
	for _, tc := range cases {
		err := ValidateFlowID(tc.id)
		if (err != nil) != tc.wantErr {
			t.Errorf("ValidateFlowID(%q) err=%v wantErr=%v", tc.id, err, tc.wantErr)
		}
	}
}

func TestValidateNodeType(t *testing.T) {
	cases := map[string]bool{
		NodeTypeLLM:    true,
		NodeTypeTool:   true,
		NodeTypeIf:     true,
		NodeTypeOutput: true,
		"unknown":      false,
		"":             false,
	}
	for name, want := range cases {
		got := IsKnownNodeType(name)
		if got != want {
			t.Errorf("IsKnownNodeType(%q)=%v want=%v", name, got, want)
		}
	}
}

func TestFlowValidate_HappyPath(t *testing.T) {
	f := &Flow{
		ID: "flow_test",
		Nodes: []Node{
			{ID: "n1", Type: NodeTypeLLM, Config: map[string]any{"provider": "mock", "promptTemplate": "Hi"}},
			{ID: "n2", Type: NodeTypeOutput, Config: map[string]any{"keys": []any{"n1"}}},
		},
		Edges: []Edge{
			{From: "n1", To: "n2"},
		},
	}
	if err := f.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestFlowValidate_RejectsDuplicateNodeID(t *testing.T) {
	f := &Flow{
		ID: "flow_dup",
		Nodes: []Node{
			{ID: "n1", Type: NodeTypeLLM, Config: map[string]any{"provider": "mock"}},
			{ID: "n1", Type: NodeTypeOutput, Config: map[string]any{"keys": []any{}}},
		},
	}
	if err := f.Validate(); err == nil {
		t.Fatal("expected duplicate-node error")
	}
}

func TestFlowValidate_RejectsUnknownNodeType(t *testing.T) {
	f := &Flow{
		ID: "flow_bad",
		Nodes: []Node{
			{ID: "n1", Type: "ohno", Config: map[string]any{}},
		},
	}
	if err := f.Validate(); err == nil {
		t.Fatal("expected unknown-node-type error")
	}
}

func TestFlowValidate_RejectsEdgeToUnknownNode(t *testing.T) {
	f := &Flow{
		ID: "flow_edge",
		Nodes: []Node{
			{ID: "n1", Type: NodeTypeLLM, Config: map[string]any{"provider": "mock"}},
		},
		Edges: []Edge{
			{From: "n1", To: "missing"},
		},
	}
	if err := f.Validate(); err == nil {
		t.Fatal("expected unknown-edge-target error")
	}
}

func TestFlowValidate_RejectsCycle(t *testing.T) {
	f := &Flow{
		ID: "flow_cycle",
		Nodes: []Node{
			{ID: "a", Type: NodeTypeLLM, Config: map[string]any{"provider": "mock"}},
			{ID: "b", Type: NodeTypeLLM, Config: map[string]any{"provider": "mock"}},
		},
		Edges: []Edge{
			{From: "a", To: "b"},
			{From: "b", To: "a"},
		},
	}
	if err := f.Validate(); err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestFlowValidate_RejectsEmpty(t *testing.T) {
	f := &Flow{ID: "empty"}
	if err := f.Validate(); err == nil {
		t.Fatal("expected empty-flow error")
	}
}

func TestFlowValidate_RejectsBadIfCondition(t *testing.T) {
	f := &Flow{
		ID: "flow_if_bad",
		Nodes: []Node{
			{ID: "n1", Type: NodeTypeIf, Config: map[string]any{}},
		},
	}
	if err := f.Validate(); err == nil {
		t.Fatal("expected if-missing-condition error")
	}
}

func TestTopoOrder_Linear(t *testing.T) {
	f := &Flow{
		ID: "f",
		Nodes: []Node{
			{ID: "a", Type: NodeTypeLLM, Config: map[string]any{"provider": "mock"}},
			{ID: "b", Type: NodeTypeLLM, Config: map[string]any{"provider": "mock"}},
			{ID: "c", Type: NodeTypeOutput, Config: map[string]any{"keys": []any{}}},
		},
		Edges: []Edge{
			{From: "a", To: "b"},
			{From: "b", To: "c"},
		},
	}
	order, err := f.TopoOrder()
	if err != nil {
		t.Fatalf("TopoOrder() err=%v", err)
	}
	if len(order) != 3 || order[0] != "a" || order[1] != "b" || order[2] != "c" {
		t.Fatalf("unexpected order: %v", order)
	}
}

func TestTopoOrder_Diamond(t *testing.T) {
	f := &Flow{
		ID: "f",
		Nodes: []Node{
			{ID: "a", Type: NodeTypeLLM, Config: map[string]any{"provider": "mock"}},
			{ID: "b", Type: NodeTypeLLM, Config: map[string]any{"provider": "mock"}},
			{ID: "c", Type: NodeTypeLLM, Config: map[string]any{"provider": "mock"}},
			{ID: "d", Type: NodeTypeOutput, Config: map[string]any{"keys": []any{}}},
		},
		Edges: []Edge{
			{From: "a", To: "b"},
			{From: "a", To: "c"},
			{From: "b", To: "d"},
			{From: "c", To: "d"},
		},
	}
	order, err := f.TopoOrder()
	if err != nil {
		t.Fatalf("TopoOrder() err=%v", err)
	}
	pos := map[string]int{}
	for i, id := range order {
		pos[id] = i
	}
	if !(pos["a"] < pos["b"] && pos["a"] < pos["c"] && pos["b"] < pos["d"] && pos["c"] < pos["d"]) {
		t.Fatalf("topo order violates dependencies: %v", order)
	}
}
