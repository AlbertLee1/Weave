package pipeline

import (
	"strings"
	"testing"
)

func TestBuildDAG_NodeOrderAndDeps(t *testing.T) {
	p := &Pipeline{
		ID:   "etl",
		Name: "ETL",
		Inputs: []Input{
			{Name: "src_a", Type: "objectset"},
			{Name: "src_b", Type: "objectset"},
		},
		Transforms: []Transform{
			{Name: "join", Type: "join", Inputs: []string{"src_a", "src_b"}},
			{Name: "filter", Type: "filter", Inputs: []string{"join"}},
		},
		Outputs: []Output{
			{Name: "sink", Type: "jdbc", Input: "filter"},
			{Name: "audit", Type: "log"},
		},
	}
	nodes, err := BuildDAG(p)
	if err != nil {
		t.Fatalf("BuildDAG: %v", err)
	}
	if got, want := len(nodes), 6; got != want {
		t.Fatalf("BuildDAG returned %d nodes, want %d", got, want)
	}
	wantOrder := []string{"src_a", "src_b", "join", "filter", "sink", "audit"}
	for i, n := range nodes {
		if n.Name != wantOrder[i] {
			t.Errorf("nodes[%d].Name = %q, want %q", i, n.Name, wantOrder[i])
		}
	}
	expectKind := map[string]NodeKind{
		"src_a":  NodeKindInput,
		"src_b":  NodeKindInput,
		"join":   NodeKindTransform,
		"filter": NodeKindTransform,
		"sink":   NodeKindOutput,
		"audit":  NodeKindOutput,
	}
	for _, n := range nodes {
		if n.Kind != expectKind[n.Name] {
			t.Errorf("nodes[%q].Kind = %q, want %q", n.Name, n.Kind, expectKind[n.Name])
		}
	}
	expectDeps := map[string][]string{
		"src_a":  nil,
		"src_b":  nil,
		"join":   {"src_a", "src_b"},
		"filter": {"join"},
		"sink":   {"filter"},
		"audit":  nil,
	}
	for _, n := range nodes {
		want := expectDeps[n.Name]
		if len(n.Deps) != len(want) {
			t.Errorf("%q.Deps = %v, want %v", n.Name, n.Deps, want)
			continue
		}
		for i := range n.Deps {
			if n.Deps[i] != want[i] {
				t.Errorf("%q.Deps[%d] = %q, want %q", n.Name, i, n.Deps[i], want[i])
			}
		}
	}
}

func TestBuildDAG_NilOrInvalid(t *testing.T) {
	if _, err := BuildDAG(nil); err == nil {
		t.Fatal("BuildDAG(nil) returned nil err")
	}
	bad := &Pipeline{ID: ""}
	if _, err := BuildDAG(bad); err == nil {
		t.Fatal("BuildDAG with invalid pipeline returned nil err")
	}
}

func TestTopoOrder_SortsByDependency(t *testing.T) {
	nodes := []DAGNode{
		{Name: "a", Kind: NodeKindInput},
		{Name: "b", Kind: NodeKindInput},
		{Name: "c", Kind: NodeKindTransform, Deps: []string{"a", "b"}},
		{Name: "d", Kind: NodeKindOutput, Deps: []string{"c"}},
	}
	order, err := TopoOrder(nodes)
	if err != nil {
		t.Fatalf("TopoOrder: %v", err)
	}
	if got, want := len(order), 4; got != want {
		t.Fatalf("TopoOrder returned %d entries, want %d", got, want)
	}
	pos := map[string]int{}
	for i, n := range order {
		pos[n] = i
	}
	if pos["c"] < pos["a"] || pos["c"] < pos["b"] {
		t.Fatalf("c must come after a,b: got %v", order)
	}
	if pos["d"] < pos["c"] {
		t.Fatalf("d must come after c: got %v", order)
	}
}

func TestTopoOrder_DeterministicTieBreak(t *testing.T) {
	nodes := []DAGNode{
		{Name: "a", Kind: NodeKindInput},
		{Name: "b", Kind: NodeKindInput},
		{Name: "c", Kind: NodeKindOutput, Deps: []string{"a"}},
		{Name: "d", Kind: NodeKindOutput, Deps: []string{"b"}},
	}
	first, _ := TopoOrder(nodes)
	for i := 0; i < 5; i++ {
		got, _ := TopoOrder(nodes)
		for j := range got {
			if got[j] != first[j] {
				t.Fatalf("TopoOrder is not deterministic: run %d returned %v, prior %v", i, got, first)
			}
		}
	}
}

func TestTopoOrder_UnknownDep(t *testing.T) {
	nodes := []DAGNode{
		{Name: "a", Kind: NodeKindTransform, Deps: []string{"missing"}},
	}
	_, err := TopoOrder(nodes)
	if err == nil {
		t.Fatal("TopoOrder with unknown dep returned nil err")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected error mentioning missing, got %v", err)
	}
}

func TestTopoOrder_Cycle(t *testing.T) {
	// Manually constructed cycle (Validate would have rejected this; we
	// still want TopoOrder to detect it as a defensive measure).
	nodes := []DAGNode{
		{Name: "a", Kind: NodeKindTransform, Deps: []string{"b"}},
		{Name: "b", Kind: NodeKindTransform, Deps: []string{"a"}},
	}
	_, err := TopoOrder(nodes)
	if err == nil {
		t.Fatal("TopoOrder cycle returned nil err")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}
