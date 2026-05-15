// VTX-119 — Snapshot Diff between two versions of a System Graph.
//
// GraphDiff is the canonical wire shape the /api/vertex/v1/graphs/{rid}/diff
// handler returns; Diff(from, to) computes it from two GraphSnapshot
// inputs. Separating the pure function from the HTTP layer keeps the
// diff algorithm trivially unit-testable and lets future callers
// (e.g. Workshop's history pane in VTX-105) reuse the same logic
// without round-tripping through HTTP.

package graphsvc

import (
	"sort"
)

// NodePosition is the (x, y) coordinate of a node in graph layout space.
type NodePosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// NodeStyle holds the visual attributes that contribute to a styleChange
// — color, size, label visibility. Anything finer-grained (icon swap,
// badge text) extends this struct rather than its own diff row.
type NodeStyle struct {
	Color string  `json:"color,omitempty"`
	Size  float64 `json:"size,omitempty"`
}

// SnapshotNode is one node in a GraphSnapshot. ID is the per-graph
// identifier (the underlying object's RID belongs in Properties).
type SnapshotNode struct {
	ID         string                 `json:"id"`
	Position   NodePosition           `json:"position"`
	Style      NodeStyle              `json:"style"`
	Properties map[string]any         `json:"properties,omitempty"`
}

// GraphSnapshot is the input to Diff — a frozen view of the graph at
// one version.
type GraphSnapshot struct {
	Version string         `json:"version"`
	Nodes   []SnapshotNode `json:"nodes"`
}

// GraphDiff is the wire shape returned by the /diff endpoint.
type GraphDiff struct {
	AddedNodes     []string         `json:"addedNodes"`
	RemovedNodes   []string         `json:"removedNodes"`
	ModifiedNodes  []string         `json:"modifiedNodes"`
	StyleChanges   []NodeStyleChange  `json:"styleChanges"`
	LayoutChanges  []NodeLayoutChange `json:"layoutChanges"`
}

// NodeStyleChange records a per-node style transition.
type NodeStyleChange struct {
	NodeID string    `json:"nodeId"`
	From   NodeStyle `json:"from"`
	To     NodeStyle `json:"to"`
}

// NodeLayoutChange records a per-node position transition.
type NodeLayoutChange struct {
	NodeID string       `json:"nodeId"`
	From   NodePosition `json:"from"`
	To     NodePosition `json:"to"`
}

// Diff computes the set of added / removed / modified node ids between
// from and to plus the per-node style / layout deltas. Results are
// sorted deterministically so consumers can byte-compare two diffs.
func Diff(from, to GraphSnapshot) GraphDiff {
	fromIdx := indexByID(from.Nodes)
	toIdx := indexByID(to.Nodes)

	var added, removed, modified []string
	var styleChanges []NodeStyleChange
	var layoutChanges []NodeLayoutChange

	for id, n := range toIdx {
		old, ok := fromIdx[id]
		if !ok {
			added = append(added, id)
			continue
		}
		// Record style / layout changes regardless of "modified" status
		// — the diff is consumed by both an audit view (which wants all
		// rows) and a render view (which only cares about node membership).
		if old.Style != n.Style {
			styleChanges = append(styleChanges, NodeStyleChange{NodeID: id, From: old.Style, To: n.Style})
		}
		if old.Position != n.Position {
			layoutChanges = append(layoutChanges, NodeLayoutChange{NodeID: id, From: old.Position, To: n.Position})
		}
		if nodeContentChanged(old, n) {
			modified = append(modified, id)
		}
	}
	for id := range fromIdx {
		if _, ok := toIdx[id]; !ok {
			removed = append(removed, id)
		}
	}

	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(modified)
	sort.Slice(styleChanges, func(i, j int) bool { return styleChanges[i].NodeID < styleChanges[j].NodeID })
	sort.Slice(layoutChanges, func(i, j int) bool { return layoutChanges[i].NodeID < layoutChanges[j].NodeID })

	return GraphDiff{
		AddedNodes:    nonNil(added),
		RemovedNodes:  nonNil(removed),
		ModifiedNodes: nonNil(modified),
		StyleChanges:  styleChanges,
		LayoutChanges: layoutChanges,
	}
}

func indexByID(nodes []SnapshotNode) map[string]SnapshotNode {
	m := make(map[string]SnapshotNode, len(nodes))
	for _, n := range nodes {
		m[n.ID] = n
	}
	return m
}

// nodeContentChanged returns true when anything other than style /
// layout differs between two snapshots of the same node. Style/layout
// changes do NOT count as "modified" because they have their own
// dedicated rows in the diff — bundling them would double-count in
// audit views.
func nodeContentChanged(a, b SnapshotNode) bool {
	if len(a.Properties) != len(b.Properties) {
		return true
	}
	for k, va := range a.Properties {
		vb, ok := b.Properties[k]
		if !ok {
			return true
		}
		if !sameValue(va, vb) {
			return true
		}
	}
	return false
}

func sameValue(a, b any) bool {
	// Trivial JSON-friendly equality. The properties map only contains
	// types encoding/json round-trips: string, float64, bool, nil, and
	// nested maps/slices. For maps/slices we delegate to reflect-style
	// deep equality via fmt rather than pulling in reflect.DeepEqual —
	// the diff is best-effort; consumers re-fetch authoritative state
	// from the read API anyway.
	return jsonEq(a, b)
}

func jsonEq(a, b any) bool {
	switch av := a.(type) {
	case nil:
		return b == nil
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case float64:
		bv, ok := b.(float64)
		return ok && av == bv
	case int:
		bv, ok := b.(int)
		return ok && av == bv
	}
	return false
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
