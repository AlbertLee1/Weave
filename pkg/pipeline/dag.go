package pipeline

import (
	"errors"
	"fmt"
	"sort"
)

// NodeKind classifies a DAGNode by its origin in the Pipeline shape.
type NodeKind string

const (
	NodeKindInput     NodeKind = "input"
	NodeKindTransform NodeKind = "transform"
	NodeKindOutput    NodeKind = "output"
)

// DAGNode is the executor-side projection of one Pipeline node. The
// per-kind structs (Input / Transform / Output) collapse into this
// single shape so the runner can iterate uniformly.
//
// Config aliases the underlying Pipeline map; treat as read-only.
type DAGNode struct {
	Name   string
	Kind   NodeKind
	Type   string
	Config map[string]any
	Deps   []string
}

// BuildDAG validates p and returns the executor projection in
// declaration order: inputs first, then transforms, then outputs. The
// caller may pass the result to TopoOrder to obtain a runnable order.
func BuildDAG(p *Pipeline) ([]DAGNode, error) {
	if p == nil {
		return nil, errors.New("pipeline is nil")
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	out := make([]DAGNode, 0, len(p.Inputs)+len(p.Transforms)+len(p.Outputs))
	for _, in := range p.Inputs {
		out = append(out, DAGNode{
			Name:   in.Name,
			Kind:   NodeKindInput,
			Type:   in.Type,
			Config: in.Config,
		})
	}
	for _, t := range p.Transforms {
		var deps []string
		if len(t.Inputs) > 0 {
			deps = append([]string(nil), t.Inputs...)
		}
		out = append(out, DAGNode{
			Name:   t.Name,
			Kind:   NodeKindTransform,
			Type:   t.Type,
			Config: t.Config,
			Deps:   deps,
		})
	}
	for _, o := range p.Outputs {
		var deps []string
		if o.Input != "" {
			deps = []string{o.Input}
		}
		out = append(out, DAGNode{
			Name:   o.Name,
			Kind:   NodeKindOutput,
			Type:   o.Type,
			Config: o.Config,
			Deps:   deps,
		})
	}
	return out, nil
}

// TopoOrder returns node names in a topological order such that every
// edge goes from an earlier name to a later name. Tie-breaking uses the
// declaration order of nodes so the result is deterministic across
// runs. Returns an error when an unknown dep is referenced or the
// graph contains a cycle.
func TopoOrder(nodes []DAGNode) ([]string, error) {
	indeg := make(map[string]int, len(nodes))
	adj := make(map[string][]string, len(nodes))
	declIdx := make(map[string]int, len(nodes))
	for i, n := range nodes {
		indeg[n.Name] = 0
		declIdx[n.Name] = i
	}
	for _, n := range nodes {
		for _, dep := range n.Deps {
			if _, ok := indeg[dep]; !ok {
				return nil, fmt.Errorf("node %q references unknown dependency %q", n.Name, dep)
			}
			adj[dep] = append(adj[dep], n.Name)
			indeg[n.Name]++
		}
	}
	ready := make([]string, 0)
	for _, n := range nodes {
		if indeg[n.Name] == 0 {
			ready = append(ready, n.Name)
		}
	}
	out := make([]string, 0, len(nodes))
	for len(ready) > 0 {
		sort.SliceStable(ready, func(i, j int) bool { return declIdx[ready[i]] < declIdx[ready[j]] })
		head := ready[0]
		ready = ready[1:]
		out = append(out, head)
		for _, dst := range adj[head] {
			indeg[dst]--
			if indeg[dst] == 0 {
				ready = append(ready, dst)
			}
		}
	}
	if len(out) != len(nodes) {
		return nil, errors.New("pipeline DAG contains a cycle")
	}
	return out, nil
}
