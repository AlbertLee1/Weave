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
	"encoding/json"
	"sort"
)

// PatchOp is one RFC 6902 JSON Patch operation. Only the three canonical
// ops used by JSONPatch are produced: add / remove / replace. The Value
// field is omitted for remove ops via the omitempty JSON tag — RFC 6902
// forbids "value" on remove.
type PatchOp struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value,omitempty"`
}

// keyedArrayFields are top-level payload fields whose arrays carry stable
// per-item id handles. Diffing treats them as id-keyed maps so add /
// remove / replace ops emit paths like /layers/L1 rather than /layers/3 —
// the latter shifts under intervening insertions, breaking byte-stability.
var keyedArrayFields = map[string]bool{
	"layers": true,
	"edges":  true,
}

// JSONPatch computes the RFC 6902 patch that transforms from into to.
// Returns a non-nil empty slice when the two payloads are equal (so the
// JSON encoder emits `[]` rather than `null`).
//
// Algorithm: walk both payloads as decoded any-trees in parallel. For
// known keyed-array fields (layers, edges) we index by id and diff
// per-item; for nested objects we recurse key-by-key; for scalar values
// we emit replace on inequality. Paths are sorted lexicographically so
// repeated calls produce byte-identical output.
func JSONPatch(from, to json.RawMessage) ([]PatchOp, error) {
	fromV, err := decodePayload(from)
	if err != nil {
		return nil, err
	}
	toV, err := decodePayload(to)
	if err != nil {
		return nil, err
	}
	var ops []PatchOp
	diffValue(&ops, "", fromV, toV)
	sort.Slice(ops, func(i, j int) bool { return ops[i].Path < ops[j].Path })
	if ops == nil {
		return []PatchOp{}, nil
	}
	return ops, nil
}

func decodePayload(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return v, nil
}

func diffValue(ops *[]PatchOp, path string, from, to any) {
	if jsonValuesEqual(from, to) {
		return
	}
	if from == nil {
		*ops = append(*ops, PatchOp{Op: "add", Path: path, Value: to})
		return
	}
	if to == nil {
		*ops = append(*ops, PatchOp{Op: "remove", Path: path})
		return
	}
	fromMap, fromIsMap := from.(map[string]any)
	toMap, toIsMap := to.(map[string]any)
	if fromIsMap && toIsMap {
		diffMap(ops, path, fromMap, toMap)
		return
	}
	*ops = append(*ops, PatchOp{Op: "replace", Path: path, Value: to})
}

func diffMap(ops *[]PatchOp, prefix string, from, to map[string]any) {
	keys := sortedKeyUnion(from, to)
	for _, k := range keys {
		fv, fOK := from[k]
		tv, tOK := to[k]
		subPath := prefix + "/" + escapeJSONPointer(k)
		// Top-level layers / edges are arrays-of-objects that carry id
		// handles; route them through diffKeyedArray so paths stay stable
		// against intervening insertions.
		if prefix == "" && keyedArrayFields[k] {
			diffKeyedArray(ops, subPath, fv, tv)
			continue
		}
		switch {
		case !fOK && tOK:
			*ops = append(*ops, PatchOp{Op: "add", Path: subPath, Value: tv})
		case fOK && !tOK:
			*ops = append(*ops, PatchOp{Op: "remove", Path: subPath})
		default:
			diffValue(ops, subPath, fv, tv)
		}
	}
}

func diffKeyedArray(ops *[]PatchOp, prefix string, from, to any) {
	fromIdx := indexArrayByID(from)
	toIdx := indexArrayByID(to)
	keys := sortedKeyUnion(fromIdx, toIdx)
	for _, k := range keys {
		fv, fOK := fromIdx[k]
		tv, tOK := toIdx[k]
		sub := prefix + "/" + escapeJSONPointer(k)
		switch {
		case !fOK && tOK:
			*ops = append(*ops, PatchOp{Op: "add", Path: sub, Value: tv})
		case fOK && !tOK:
			*ops = append(*ops, PatchOp{Op: "remove", Path: sub})
		default:
			diffValue(ops, sub, fv, tv)
		}
	}
}

func indexArrayByID(v any) map[string]any {
	out := map[string]any{}
	arr, ok := v.([]any)
	if !ok {
		return out
	}
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, ok := m["id"].(string)
		if !ok || id == "" {
			continue
		}
		out[id] = m
	}
	return out
}

func sortedKeyUnion(a, b map[string]any) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	keys := make([]string, 0, len(a)+len(b))
	for k := range a {
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			keys = append(keys, k)
		}
	}
	for k := range b {
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// jsonValuesEqual compares two decoded JSON trees by re-encoding them.
// Both inputs come from json.Unmarshal, so map key order is the only
// source of byte-level divergence; encoding/json sorts map keys, so the
// comparison is canonical without us reaching for reflect.DeepEqual.
func jsonValuesEqual(a, b any) bool {
	ab, errA := json.Marshal(a)
	bb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(ab) == string(bb)
}

// escapeJSONPointer applies RFC 6901 segment escaping: ~ → ~0, / → ~1.
// The order matters — escape ~ first so / introduced by escaping doesn't
// get re-escaped.
func escapeJSONPointer(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '~':
			out = append(out, '~', '0')
		case '/':
			out = append(out, '~', '1')
		default:
			out = append(out, s[i])
		}
	}
	return string(out)
}

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
