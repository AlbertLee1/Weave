package actions

import (
	"errors"
	"testing"

	"github.com/liyang/weave/pkg/funnel"
)

// US-473: property timeline LWW + schema validation tests.

// TestCollapseEdits_PropertyTimeline_FiveStepLWW pins the PRD literal
// "5 步 edit → 合并后属性 set 等价" acceptance: five back-to-back edits on
// the same object collapse into a single edit whose Properties reflect
// the last-write-wins resolution of every property touched.
func TestCollapseEdits_PropertyTimeline_FiveStepLWW(t *testing.T) {
	edits := []funnel.Edit{
		{Type: funnel.EditTypeCreate, ObjectType: "A", PrimaryKey: "1",
			Properties: map[string]interface{}{"x": 1, "y": 1}},
		{Type: funnel.EditTypeModify, ObjectType: "A", PrimaryKey: "1",
			Properties: map[string]interface{}{"x": 2}},
		{Type: funnel.EditTypeModify, ObjectType: "A", PrimaryKey: "1",
			Properties: map[string]interface{}{"y": 2, "z": 1}},
		{Type: funnel.EditTypeModify, ObjectType: "A", PrimaryKey: "1",
			Properties: map[string]interface{}{"x": 3}},
		{Type: funnel.EditTypeModify, ObjectType: "A", PrimaryKey: "1",
			Properties: map[string]interface{}{"z": 9, "w": 7}},
	}
	result := CollapseEdits(edits)

	if len(result) != 1 {
		t.Fatalf("expected 1 collapsed edit, got %d", len(result))
	}
	got := result[0]
	if got.Type != funnel.EditTypeCreate {
		t.Errorf("type = %q, want CREATE (CREATE+MODIFY chains keep CREATE)", got.Type)
	}
	want := map[string]interface{}{"x": 3, "y": 2, "z": 9, "w": 7}
	if len(got.Properties) != len(want) {
		t.Fatalf("properties len = %d (%v), want %d (%v)",
			len(got.Properties), got.Properties, len(want), want)
	}
	for k, v := range want {
		if got.Properties[k] != v {
			t.Errorf("Properties[%q] = %v, want %v", k, got.Properties[k], v)
		}
	}
}

// TestCollapseEdits_PropertyTimeline_DoesNotMutateInputProperties locks the
// US-473 promise that the per-property LWW merge builds a fresh map on the
// returned edit and does NOT mutate any input Edit's Properties map. The
// pre-US-473 implementation mutated the first edit's map in place, which
// leaked merged state into per-action result.Edits payloads.
func TestCollapseEdits_PropertyTimeline_DoesNotMutateInputProperties(t *testing.T) {
	createProps := map[string]interface{}{"x": 1}
	modifyProps := map[string]interface{}{"y": 2}
	edits := []funnel.Edit{
		{Type: funnel.EditTypeCreate, ObjectType: "A", PrimaryKey: "1",
			Properties: createProps},
		{Type: funnel.EditTypeModify, ObjectType: "A", PrimaryKey: "1",
			Properties: modifyProps},
	}
	_ = CollapseEdits(edits)
	if _, ok := createProps["y"]; ok {
		t.Errorf("CollapseEdits mutated the CREATE edit's input Properties map: %v", createProps)
	}
	if _, ok := modifyProps["x"]; ok {
		t.Errorf("CollapseEdits mutated the MODIFY edit's input Properties map: %v", modifyProps)
	}
}

// TestCollapseEdits_PropertyTimeline_MultiObjectInterleaved makes sure each
// object owns its own timeline — interleaved edits on three objects collapse
// independently without cross-bleed.
func TestCollapseEdits_PropertyTimeline_MultiObjectInterleaved(t *testing.T) {
	edits := []funnel.Edit{
		{Type: funnel.EditTypeCreate, ObjectType: "A", PrimaryKey: "1",
			Properties: map[string]interface{}{"x": 1}},
		{Type: funnel.EditTypeCreate, ObjectType: "B", PrimaryKey: "1",
			Properties: map[string]interface{}{"x": 100}},
		{Type: funnel.EditTypeModify, ObjectType: "A", PrimaryKey: "1",
			Properties: map[string]interface{}{"x": 2}},
		{Type: funnel.EditTypeModify, ObjectType: "B", PrimaryKey: "1",
			Properties: map[string]interface{}{"y": 200}},
		{Type: funnel.EditTypeModify, ObjectType: "A", PrimaryKey: "1",
			Properties: map[string]interface{}{"y": 3}},
	}
	result := CollapseEdits(edits)
	if len(result) != 2 {
		t.Fatalf("expected 2 collapsed edits, got %d", len(result))
	}
	byKey := map[string]funnel.Edit{}
	for _, e := range result {
		byKey[e.ObjectType+"|"+e.PrimaryKey] = e
	}
	a := byKey["A|1"]
	if a.Properties["x"] != 2 || a.Properties["y"] != 3 || len(a.Properties) != 2 {
		t.Errorf("A|1 properties = %v, want {x:2, y:3}", a.Properties)
	}
	b := byKey["B|1"]
	if b.Properties["x"] != 100 || b.Properties["y"] != 200 || len(b.Properties) != 2 {
		t.Errorf("B|1 properties = %v, want {x:100, y:200}", b.Properties)
	}
}

// ---------------------------------------------------------------------------
// Schema validation tests
// ---------------------------------------------------------------------------

// fakeSchemaLookup is a deterministic in-memory SchemaLookup for tests.
type fakeSchemaLookup map[string]map[string]struct{}

func (f fakeSchemaLookup) PropertyNames(objectType string) (map[string]struct{}, bool) {
	props, ok := f[objectType]
	return props, ok
}

func newFakeSchema(specs map[string][]string) fakeSchemaLookup {
	out := fakeSchemaLookup{}
	for ot, names := range specs {
		s := make(map[string]struct{}, len(names))
		for _, n := range names {
			s[n] = struct{}{}
		}
		out[ot] = s
	}
	return out
}

func TestCollapseEditsWithSchema_AllPropertiesDeclared_NoError(t *testing.T) {
	schema := newFakeSchema(map[string][]string{
		"A": {"x", "y", "z"},
	})
	edits := []funnel.Edit{
		{Type: funnel.EditTypeCreate, ObjectType: "A", PrimaryKey: "1",
			Properties: map[string]interface{}{"x": 1}},
		{Type: funnel.EditTypeModify, ObjectType: "A", PrimaryKey: "1",
			Properties: map[string]interface{}{"y": 2}},
	}
	collapsed, violations, err := CollapseEditsWithSchema(edits, schema)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("expected 0 violations, got %d (%v)", len(violations), violations)
	}
	if len(collapsed) != 1 || collapsed[0].Properties["x"] != 1 || collapsed[0].Properties["y"] != 2 {
		t.Errorf("collapsed = %v, want one edit with {x:1, y:2}", collapsed)
	}
}

func TestCollapseEditsWithSchema_CreatePlusModifyMergesUndeclaredProperty_ReturnsViolation(t *testing.T) {
	// CREATE writes only declared properties; MODIFY introduces an undeclared
	// property. After collapse, the merged Properties map carries the
	// undeclared name → violation.
	schema := newFakeSchema(map[string][]string{
		"A": {"x", "y"},
	})
	edits := []funnel.Edit{
		{Type: funnel.EditTypeCreate, ObjectType: "A", PrimaryKey: "1",
			Properties: map[string]interface{}{"x": 1}},
		{Type: funnel.EditTypeModify, ObjectType: "A", PrimaryKey: "1",
			Properties: map[string]interface{}{"unknown_prop": 9}},
	}
	collapsed, violations, err := CollapseEditsWithSchema(edits, schema)
	if !errors.Is(err, ErrCollapseSchemaViolation) {
		t.Fatalf("expected ErrCollapseSchemaViolation, got %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d (%v)", len(violations), violations)
	}
	v := violations[0]
	if v.ObjectType != "A" || v.PrimaryKey != "1" || v.Property != "unknown_prop" {
		t.Errorf("violation = %+v, want {A,1,unknown_prop}", v)
	}
	// Collapsed output is still returned (caller decides whether to publish)
	// so they can use it for diagnostics.
	if len(collapsed) != 1 {
		t.Errorf("expected 1 collapsed edit, got %d", len(collapsed))
	}
}

func TestCollapseEditsWithSchema_UnknownObjectType_LenientSkip(t *testing.T) {
	// OT not registered in the schema → lenient skip (matches the existing
	// pkg/actions executor pattern in validateEditPropertyValues, which
	// silently continues when the OT cannot be looked up).
	schema := newFakeSchema(map[string][]string{
		"B": {"x"},
	})
	edits := []funnel.Edit{
		{Type: funnel.EditTypeCreate, ObjectType: "A", PrimaryKey: "1",
			Properties: map[string]interface{}{"any_prop": 1}},
	}
	_, violations, err := CollapseEditsWithSchema(edits, schema)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for unknown OT, got %d", len(violations))
	}
}

func TestCollapseEditsWithSchema_LinkEdits_NotValidated(t *testing.T) {
	// LINK_CREATE / LINK_DELETE edits never carry object Properties, so they
	// must bypass schema validation entirely.
	schema := newFakeSchema(map[string][]string{
		"A": {"x"},
	})
	edits := []funnel.Edit{
		{Type: funnel.EditTypeLinkCreate, PrimaryKey: "emp-1",
			LinkTypeRID: "lt-1", TargetPrimaryKey: "dept-1"},
		{Type: funnel.EditTypeLinkDelete, PrimaryKey: "emp-2",
			LinkTypeRID: "lt-1", TargetPrimaryKey: "dept-2"},
	}
	collapsed, violations, err := CollapseEditsWithSchema(edits, schema)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("expected 0 violations, got %d", len(violations))
	}
	if len(collapsed) != 2 {
		t.Errorf("expected 2 link edits, got %d", len(collapsed))
	}
}

func TestCollapseEditsWithSchema_DeleteEditsSkipPropertyCheck(t *testing.T) {
	// DELETE edits have no Properties payload (they signal removal). Schema
	// validation must not blow up on them.
	schema := newFakeSchema(map[string][]string{
		"A": {"x"},
	})
	edits := []funnel.Edit{
		{Type: funnel.EditTypeDelete, ObjectType: "A", PrimaryKey: "1"},
	}
	_, violations, err := CollapseEditsWithSchema(edits, schema)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("expected 0 violations, got %d", len(violations))
	}
}

// TestCollapseEditsWithSchema_NilLookup_DegradesGracefully — when no schema
// lookup is wired (nil), the helper behaves identically to CollapseEdits and
// returns no violations. Mirrors the executor degradation pattern where an
// uninitialized omsRepo skips validation.
func TestCollapseEditsWithSchema_NilLookup_DegradesGracefully(t *testing.T) {
	edits := []funnel.Edit{
		{Type: funnel.EditTypeCreate, ObjectType: "A", PrimaryKey: "1",
			Properties: map[string]interface{}{"anything": 1}},
	}
	collapsed, violations, err := CollapseEditsWithSchema(edits, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("expected 0 violations with nil lookup, got %d", len(violations))
	}
	if len(collapsed) != 1 {
		t.Errorf("expected 1 collapsed edit, got %d", len(collapsed))
	}
}

func TestCollapseEditsWithSchema_MultipleViolations_AllReported(t *testing.T) {
	// Two separate violations across two objects: both should land in the
	// returned slice so the caller can log all of them at once.
	schema := newFakeSchema(map[string][]string{
		"A": {"x"},
		"B": {"y"},
	})
	edits := []funnel.Edit{
		{Type: funnel.EditTypeCreate, ObjectType: "A", PrimaryKey: "1",
			Properties: map[string]interface{}{"bad_a": 1}},
		{Type: funnel.EditTypeCreate, ObjectType: "B", PrimaryKey: "2",
			Properties: map[string]interface{}{"bad_b": 2}},
	}
	_, violations, err := CollapseEditsWithSchema(edits, schema)
	if !errors.Is(err, ErrCollapseSchemaViolation) {
		t.Fatalf("expected ErrCollapseSchemaViolation, got %v", err)
	}
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations, got %d (%v)", len(violations), violations)
	}
}
