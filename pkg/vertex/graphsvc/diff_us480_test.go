// US-480 — RFC 6902 JSON Patch diff between two snapshots of a Vertex
// SystemGraph payload. The PRD literal acceptance gates four shapes:
//   - add node (layer)
//   - remove node (layer)
//   - modify edge subfield
//   - modify layer property
// Each test asserts the exact (op, path, value) tuple JSONPatch emits so
// regressions on path-shape (id-keyed vs index-based) or op-classification
// (replace vs add/remove) fail loudly.

package graphsvc

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestJSONPatch_Given_AddedLayer_When_Diffed_Then_EmitAddOpAtIDKeyedPath_US480(t *testing.T) {
	from := json.RawMessage(`{"layers":[],"edges":[]}`)
	to := json.RawMessage(`{"layers":[{"id":"L1","objectType":"Customer"}],"edges":[]}`)
	ops, err := JSONPatch(from, to)
	if err != nil {
		t.Fatalf("JSONPatch error: %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("len(ops)=%d, want 1; ops=%+v", len(ops), ops)
	}
	got := ops[0]
	if got.Op != "add" {
		t.Errorf("ops[0].Op=%q, want add", got.Op)
	}
	if got.Path != "/layers/L1" {
		t.Errorf("ops[0].Path=%q, want /layers/L1 (id-keyed)", got.Path)
	}
	// Value must round-trip back to the added layer object.
	gotMap, ok := got.Value.(map[string]any)
	if !ok {
		t.Fatalf("ops[0].Value type=%T, want map[string]any", got.Value)
	}
	if gotMap["id"] != "L1" || gotMap["objectType"] != "Customer" {
		t.Errorf("ops[0].Value=%+v, want {id:L1, objectType:Customer}", gotMap)
	}
}

func TestJSONPatch_Given_RemovedLayer_When_Diffed_Then_EmitRemoveOp_US480(t *testing.T) {
	from := json.RawMessage(`{"layers":[{"id":"L1","objectType":"Customer"}],"edges":[]}`)
	to := json.RawMessage(`{"layers":[],"edges":[]}`)
	ops, err := JSONPatch(from, to)
	if err != nil {
		t.Fatalf("JSONPatch error: %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("len(ops)=%d, want 1; ops=%+v", len(ops), ops)
	}
	got := ops[0]
	if got.Op != "remove" {
		t.Errorf("ops[0].Op=%q, want remove", got.Op)
	}
	if got.Path != "/layers/L1" {
		t.Errorf("ops[0].Path=%q, want /layers/L1", got.Path)
	}
	if got.Value != nil {
		t.Errorf("ops[0].Value=%v, remove ops must not carry a value (RFC 6902)", got.Value)
	}
}

func TestJSONPatch_Given_EdgeSourceChanged_When_Diffed_Then_EmitReplaceOpAtNestedPath_US480(t *testing.T) {
	from := json.RawMessage(`{"layers":[],"edges":[{"id":"E1","source":"L1","target":"L2","linkTypeRid":"ri.link.a"}]}`)
	to := json.RawMessage(`{"layers":[],"edges":[{"id":"E1","source":"L3","target":"L2","linkTypeRid":"ri.link.a"}]}`)
	ops, err := JSONPatch(from, to)
	if err != nil {
		t.Fatalf("JSONPatch error: %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("len(ops)=%d, want 1; ops=%+v", len(ops), ops)
	}
	got := ops[0]
	if got.Op != "replace" {
		t.Errorf("ops[0].Op=%q, want replace", got.Op)
	}
	if got.Path != "/edges/E1/source" {
		t.Errorf("ops[0].Path=%q, want /edges/E1/source", got.Path)
	}
	if got.Value != "L3" {
		t.Errorf("ops[0].Value=%v, want L3", got.Value)
	}
}

func TestJSONPatch_Given_LayerPropertyChanged_When_Diffed_Then_EmitReplaceOp_US480(t *testing.T) {
	from := json.RawMessage(`{"layers":[{"id":"L1","objectType":"Customer","filter":{"status":"active"}}]}`)
	to := json.RawMessage(`{"layers":[{"id":"L1","objectType":"Customer","filter":{"status":"inactive"}}]}`)
	ops, err := JSONPatch(from, to)
	if err != nil {
		t.Fatalf("JSONPatch error: %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("len(ops)=%d, want 1; ops=%+v", len(ops), ops)
	}
	got := ops[0]
	if got.Op != "replace" {
		t.Errorf("ops[0].Op=%q, want replace", got.Op)
	}
	if got.Path != "/layers/L1/filter/status" {
		t.Errorf("ops[0].Path=%q, want /layers/L1/filter/status", got.Path)
	}
	if got.Value != "inactive" {
		t.Errorf("ops[0].Value=%v, want inactive", got.Value)
	}
}

func TestJSONPatch_Given_IdenticalPayloads_When_Diffed_Then_EmptyOps_US480(t *testing.T) {
	p := json.RawMessage(`{"layers":[{"id":"L1","objectType":"Customer"}],"edges":[{"id":"E1","source":"L1","target":"L1"}]}`)
	ops, err := JSONPatch(p, p)
	if err != nil {
		t.Fatalf("JSONPatch error: %v", err)
	}
	if len(ops) != 0 {
		t.Errorf("identical payloads should diff to []; got %+v", ops)
	}
	// Caller must never see nil: it would JSON-marshal to `null` instead of `[]`.
	if ops == nil {
		t.Errorf("JSONPatch returned nil; must return non-nil empty slice for JSON [] shape")
	}
}

func TestJSONPatch_Given_AddedAndRemovedEdges_When_Diffed_Then_PathsAreSortedDeterministically_US480(t *testing.T) {
	// Determinism is what makes patch responses byte-stable across calls —
	// SDKs that hash diffs for cache keys depend on this. Two changes, two
	// ops, sorted by Path (RFC 6901 lexical order).
	from := json.RawMessage(`{"edges":[{"id":"E1","source":"a","target":"b"},{"id":"E2","source":"c","target":"d"}]}`)
	to := json.RawMessage(`{"edges":[{"id":"E2","source":"c","target":"d"},{"id":"E3","source":"e","target":"f"}]}`)
	ops, err := JSONPatch(from, to)
	if err != nil {
		t.Fatalf("JSONPatch error: %v", err)
	}
	if len(ops) != 2 {
		t.Fatalf("len(ops)=%d, want 2; ops=%+v", len(ops), ops)
	}
	gotPaths := []string{ops[0].Path, ops[1].Path}
	wantPaths := []string{"/edges/E1", "/edges/E3"}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Errorf("ops paths=%v, want %v (sorted ASC)", gotPaths, wantPaths)
	}
	if ops[0].Op != "remove" || ops[1].Op != "add" {
		t.Errorf("ops ops=[%s,%s], want [remove, add]", ops[0].Op, ops[1].Op)
	}
}

func TestJSONPatch_Given_PositionsMapKeyChanged_When_Diffed_Then_EmitReplaceOnNestedKey_US480(t *testing.T) {
	// positions is a JSON object (not an array), so it diffs key-by-key
	// rather than via id-keyed array handling.
	from := json.RawMessage(`{"positions":{"L1":{"x":0,"y":0},"L2":{"x":5,"y":5}}}`)
	to := json.RawMessage(`{"positions":{"L1":{"x":10,"y":0},"L2":{"x":5,"y":5}}}`)
	ops, err := JSONPatch(from, to)
	if err != nil {
		t.Fatalf("JSONPatch error: %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("len(ops)=%d, want 1; ops=%+v", len(ops), ops)
	}
	got := ops[0]
	if got.Op != "replace" {
		t.Errorf("ops[0].Op=%q, want replace", got.Op)
	}
	if got.Path != "/positions/L1/x" {
		t.Errorf("ops[0].Path=%q, want /positions/L1/x", got.Path)
	}
	if v, ok := got.Value.(float64); !ok || v != 10 {
		t.Errorf("ops[0].Value=%v (type %T), want 10 (float64)", got.Value, got.Value)
	}
}

func TestJSONPatch_Given_JSONPointerEscapedKey_When_Diffed_Then_EscapesPerRFC6901_US480(t *testing.T) {
	// RFC 6901: ~ → ~0, / → ~1. A layer id containing a slash must be
	// escaped or the path is ambiguous.
	from := json.RawMessage(`{"layers":[{"id":"a/b","objectType":"X"}]}`)
	to := json.RawMessage(`{"layers":[]}`)
	ops, err := JSONPatch(from, to)
	if err != nil {
		t.Fatalf("JSONPatch error: %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("len(ops)=%d, want 1; ops=%+v", len(ops), ops)
	}
	if ops[0].Path != "/layers/a~1b" {
		t.Errorf("ops[0].Path=%q, want /layers/a~1b (RFC 6901 escape)", ops[0].Path)
	}
}
