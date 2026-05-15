package scenariodiff_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/liyang/weave/pkg/scenarios"
	"github.com/liyang/weave/pkg/vertex/scenariodiff"
)

func raw(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// stubBaseLoader returns canned base ObjectViews keyed by (objectType, objectID).
// LoadBase returns (nil, false, nil) when the key is absent — mirroring how a
// real loader signals "no base for this object" without confusing it with a load
// error.
type stubBaseLoader struct {
	views map[scenarios.ObjectKey]*scenarios.ObjectView
	err   error
}

func (s *stubBaseLoader) LoadBase(_ context.Context, k scenarios.ObjectKey) (*scenarios.ObjectView, bool, error) {
	if s.err != nil {
		return nil, false, s.err
	}
	v, ok := s.views[k]
	if !ok {
		return nil, false, nil
	}
	return v, true, nil
}

func newStubBaseLoader(views ...*scenarios.ObjectView) *stubBaseLoader {
	m := map[scenarios.ObjectKey]*scenarios.ObjectView{}
	for _, v := range views {
		m[scenarios.ObjectKey{ObjectType: v.ObjectType, ObjectID: v.ObjectID}] = v
	}
	return &stubBaseLoader{views: m}
}

// TestComputeDiff_Given_NoEdits_When_Diff_Then_ReturnsEmptyDiff exercises the
// happy "scenario with zero edits is a no-op" path.
func TestComputeDiff_Given_NoEdits_When_Diff_Then_ReturnsEmptyDiff(t *testing.T) {
	base := newStubBaseLoader()
	got, err := scenariodiff.Compute(context.Background(), nil, base)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if len(got.EditedObjects) != 0 || len(got.CreatedObjects) != 0 || len(got.DeletedObjects) != 0 || len(got.Deltas) != 0 {
		t.Fatalf("expected all-empty diff, got %#v", got)
	}
}

// TestComputeDiff_Given_ModifyPropertyEdit_When_Diff_Then_EditedObjectsContainOldAndNewValue
// is the spec BDD #2 verbatim: a single modifyProperty edit produces an
// editedObjects entry that carries property/oldValue/newValue.
func TestComputeDiff_Given_ModifyPropertyEdit_When_Diff_Then_EditedObjectsContainOldAndNewValue(t *testing.T) {
	base := newStubBaseLoader(&scenarios.ObjectView{
		ObjectType: "Airport",
		ObjectID:   "JFK",
		Properties: map[string]json.RawMessage{
			"capacity": raw(100),
			"name":     raw("John F Kennedy"),
		},
	})
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: raw(150)},
	}
	got, err := scenariodiff.Compute(context.Background(), edits, base)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if len(got.EditedObjects) != 1 {
		t.Fatalf("editedObjects len = %d, want 1", len(got.EditedObjects))
	}
	eo := got.EditedObjects[0]
	if eo.ObjectType != "Airport" || eo.ObjectID != "JFK" {
		t.Fatalf("editedObjects[0] = %+v, want Airport/JFK", eo)
	}
	if len(eo.Changes) != 1 {
		t.Fatalf("editedObjects[0].Changes len = %d, want 1", len(eo.Changes))
	}
	ch := eo.Changes[0]
	if ch.Property != "capacity" {
		t.Fatalf("change.Property = %q, want %q", ch.Property, "capacity")
	}
	if string(ch.OldValue) != string(raw(100)) {
		t.Fatalf("change.OldValue = %s, want %s", ch.OldValue, raw(100))
	}
	if string(ch.NewValue) != string(raw(150)) {
		t.Fatalf("change.NewValue = %s, want %s", ch.NewValue, raw(150))
	}
	// Deltas mirrors the flattened-changes view.
	if len(got.Deltas) != 1 {
		t.Fatalf("deltas len = %d, want 1", len(got.Deltas))
	}
	if got.Deltas[0].ObjectType != "Airport" || got.Deltas[0].ObjectID != "JFK" || got.Deltas[0].Property != "capacity" {
		t.Fatalf("deltas[0] = %+v, want Airport/JFK/capacity", got.Deltas[0])
	}
	if len(got.CreatedObjects) != 0 || len(got.DeletedObjects) != 0 {
		t.Fatalf("created/deleted should be empty, got %+v / %+v", got.CreatedObjects, got.DeletedObjects)
	}
}

// TestComputeDiff_Given_ModifyPropertyOnAbsentBase_When_Diff_Then_OldValueIsJSONNull
// validates that when no base property exists, oldValue is emitted as JSON null
// (not missing) so wire clients can distinguish "no prior value" from
// "property absent in diff".
func TestComputeDiff_Given_ModifyPropertyOnAbsentBase_When_Diff_Then_OldValueIsJSONNull(t *testing.T) {
	base := newStubBaseLoader() // no base for Airport/JFK
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: raw(150)},
	}
	got, err := scenariodiff.Compute(context.Background(), edits, base)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if len(got.EditedObjects) != 1 || len(got.EditedObjects[0].Changes) != 1 {
		t.Fatalf("diff = %+v, want one edited object with one change", got)
	}
	ch := got.EditedObjects[0].Changes[0]
	if string(ch.OldValue) != "null" {
		t.Fatalf("missing-base oldValue = %s, want null", ch.OldValue)
	}
}

// TestComputeDiff_Given_CreateObjectEdit_When_Diff_Then_AppearsInCreatedNotEdited
// validates that creation paths bucket into createdObjects (not editedObjects)
// and carry the property bag the createObject edit staged.
func TestComputeDiff_Given_CreateObjectEdit_When_Diff_Then_AppearsInCreatedNotEdited(t *testing.T) {
	base := newStubBaseLoader()
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "createObject", ObjectType: "Order", ObjectID: "O-1", NewValue: raw(map[string]any{"total": 99, "status": "pending"})},
	}
	got, err := scenariodiff.Compute(context.Background(), edits, base)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if len(got.CreatedObjects) != 1 {
		t.Fatalf("createdObjects len = %d, want 1", len(got.CreatedObjects))
	}
	co := got.CreatedObjects[0]
	if co.ObjectType != "Order" || co.ObjectID != "O-1" {
		t.Fatalf("createdObjects[0] = %+v, want Order/O-1", co)
	}
	// Properties propagated as a map.
	if len(co.Properties) != 2 {
		t.Fatalf("createdObjects[0].Properties len = %d, want 2", len(co.Properties))
	}
	if string(co.Properties["total"]) != string(raw(99)) {
		t.Fatalf("created.total = %s, want %s", co.Properties["total"], raw(99))
	}
	if len(got.EditedObjects) != 0 {
		t.Fatalf("editedObjects should be empty when only createObject, got %+v", got.EditedObjects)
	}
}

// TestComputeDiff_Given_CreateThenModify_When_Diff_Then_StaysInCreatedWithLatestValue
// validates that subsequent modifyProperty edits on a newly-created object
// just patch the staged property bag — the object stays in createdObjects.
func TestComputeDiff_Given_CreateThenModify_When_Diff_Then_StaysInCreatedWithLatestValue(t *testing.T) {
	base := newStubBaseLoader()
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "createObject", ObjectType: "Order", ObjectID: "O-1", NewValue: raw(map[string]any{"total": 99})},
		{Seq: 2, Op: "modifyProperty", ObjectType: "Order", ObjectID: "O-1", Property: "total", NewValue: raw(120)},
	}
	got, err := scenariodiff.Compute(context.Background(), edits, base)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if len(got.CreatedObjects) != 1 || got.CreatedObjects[0].ObjectID != "O-1" {
		t.Fatalf("expected single created Order/O-1, got %+v", got.CreatedObjects)
	}
	if string(got.CreatedObjects[0].Properties["total"]) != string(raw(120)) {
		t.Fatalf("created.total = %s, want %s", got.CreatedObjects[0].Properties["total"], raw(120))
	}
	if len(got.EditedObjects) != 0 {
		t.Fatalf("editedObjects should be empty, got %+v", got.EditedObjects)
	}
	// Deltas should not list a created-object-property as a delta — there is no
	// "old" value to compare against.
	if len(got.Deltas) != 0 {
		t.Fatalf("deltas should be empty for created objects, got %+v", got.Deltas)
	}
}

// TestComputeDiff_Given_DeleteObjectEdit_When_Diff_Then_AppearsInDeleted captures
// the simple delete-only flow.
func TestComputeDiff_Given_DeleteObjectEdit_When_Diff_Then_AppearsInDeleted(t *testing.T) {
	base := newStubBaseLoader(&scenarios.ObjectView{
		ObjectType: "Airport", ObjectID: "LGA",
		Properties: map[string]json.RawMessage{"capacity": raw(50)},
	})
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "deleteObject", ObjectType: "Airport", ObjectID: "LGA"},
	}
	got, err := scenariodiff.Compute(context.Background(), edits, base)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if len(got.DeletedObjects) != 1 {
		t.Fatalf("deletedObjects len = %d, want 1", len(got.DeletedObjects))
	}
	if got.DeletedObjects[0] != (scenariodiff.ObjectRef{ObjectType: "Airport", ObjectID: "LGA"}) {
		t.Fatalf("deletedObjects[0] = %+v, want Airport/LGA", got.DeletedObjects[0])
	}
	if len(got.EditedObjects) != 0 || len(got.CreatedObjects) != 0 || len(got.Deltas) != 0 {
		t.Fatalf("other buckets should be empty, got %+v", got)
	}
}

// TestComputeDiff_Given_DeleteThenCreateSameKey_When_Diff_Then_NetIsCreated
// captures the FoldObject semantics for re-creation: net effect = created, the
// pre-delete identity does not surface in deletedObjects.
func TestComputeDiff_Given_DeleteThenCreateSameKey_When_Diff_Then_NetIsCreated(t *testing.T) {
	base := newStubBaseLoader(&scenarios.ObjectView{
		ObjectType: "Airport", ObjectID: "JFK",
		Properties: map[string]json.RawMessage{"capacity": raw(100)},
	})
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "deleteObject", ObjectType: "Airport", ObjectID: "JFK"},
		{Seq: 2, Op: "createObject", ObjectType: "Airport", ObjectID: "JFK", NewValue: raw(map[string]any{"capacity": 999})},
	}
	got, err := scenariodiff.Compute(context.Background(), edits, base)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if len(got.CreatedObjects) != 1 || got.CreatedObjects[0].ObjectID != "JFK" {
		t.Fatalf("expected re-created Airport/JFK in createdObjects, got %+v", got.CreatedObjects)
	}
	if len(got.DeletedObjects) != 0 {
		t.Fatalf("deletedObjects should be empty (net = created), got %+v", got.DeletedObjects)
	}
	if string(got.CreatedObjects[0].Properties["capacity"]) != string(raw(999)) {
		t.Fatalf("created.capacity = %s, want 999", got.CreatedObjects[0].Properties["capacity"])
	}
}

// TestComputeDiff_Given_TwoModifyProperty_When_Diff_Then_LatestNewValueWins covers
// the same fold-replay semantics used by scenarios.FoldObject: a later
// modifyProperty on the same property wins, and only one delta entry is
// emitted with that final newValue.
func TestComputeDiff_Given_TwoModifyProperty_When_Diff_Then_LatestNewValueWins(t *testing.T) {
	base := newStubBaseLoader(&scenarios.ObjectView{
		ObjectType: "Airport", ObjectID: "JFK",
		Properties: map[string]json.RawMessage{"capacity": raw(100)},
	})
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: raw(120)},
		{Seq: 2, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: raw(150)},
	}
	got, err := scenariodiff.Compute(context.Background(), edits, base)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if len(got.EditedObjects) != 1 || len(got.EditedObjects[0].Changes) != 1 {
		t.Fatalf("expected single edited object with one change, got %+v", got.EditedObjects)
	}
	ch := got.EditedObjects[0].Changes[0]
	if string(ch.OldValue) != string(raw(100)) {
		t.Fatalf("oldValue = %s, want 100", ch.OldValue)
	}
	if string(ch.NewValue) != string(raw(150)) {
		t.Fatalf("newValue = %s, want 150 (latest)", ch.NewValue)
	}
}

// TestComputeDiff_Given_ModifySameAsBase_When_Diff_Then_OmittedAsNonChange
// validates that a modifyProperty edit whose new value is byte-identical to
// the base value is filtered out — diff only surfaces real changes.
func TestComputeDiff_Given_ModifySameAsBase_When_Diff_Then_OmittedAsNonChange(t *testing.T) {
	base := newStubBaseLoader(&scenarios.ObjectView{
		ObjectType: "Airport", ObjectID: "JFK",
		Properties: map[string]json.RawMessage{"capacity": raw(100)},
	})
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: raw(100)},
	}
	got, err := scenariodiff.Compute(context.Background(), edits, base)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if len(got.EditedObjects) != 0 || len(got.Deltas) != 0 {
		t.Fatalf("non-change should not surface, got %+v", got)
	}
}

// TestComputeDiff_Given_MultipleEdits_When_Diff_Then_SortedByObjectKey ensures
// the wire output is deterministic — clients can diff-of-diffs over runs.
func TestComputeDiff_Given_MultipleEdits_When_Diff_Then_SortedByObjectKey(t *testing.T) {
	base := newStubBaseLoader(
		&scenarios.ObjectView{
			ObjectType: "Airport", ObjectID: "JFK",
			Properties: map[string]json.RawMessage{"capacity": raw(100)},
		},
		&scenarios.ObjectView{
			ObjectType: "Airport", ObjectID: "LGA",
			Properties: map[string]json.RawMessage{"capacity": raw(50)},
		},
	)
	// Insertion order is intentionally jumbled.
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "LGA", Property: "capacity", NewValue: raw(60)},
		{Seq: 2, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: raw(150)},
	}
	got, err := scenariodiff.Compute(context.Background(), edits, base)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if len(got.EditedObjects) != 2 {
		t.Fatalf("editedObjects len = %d, want 2", len(got.EditedObjects))
	}
	if got.EditedObjects[0].ObjectID != "JFK" || got.EditedObjects[1].ObjectID != "LGA" {
		t.Fatalf("editedObjects order = [%s, %s], want [JFK, LGA]", got.EditedObjects[0].ObjectID, got.EditedObjects[1].ObjectID)
	}
}

// TestComputeDiff_Given_BaseLoaderError_When_Diff_Then_ErrorBubbles ensures
// the diff fails fast when the base loader can't be queried — the alternative
// would be a half-computed diff with phantom missing oldValues.
func TestComputeDiff_Given_BaseLoaderError_When_Diff_Then_ErrorBubbles(t *testing.T) {
	want := errors.New("base loader down")
	base := &stubBaseLoader{err: want}
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: raw(150)},
	}
	_, err := scenariodiff.Compute(context.Background(), edits, base)
	if !errors.Is(err, want) {
		t.Fatalf("Compute err = %v, want errors.Is %v", err, want)
	}
}

// TestComputeDiff_Given_LinkEdits_When_Diff_Then_IgnoredForObjectBuckets
// validates that addLink/deleteLink edits do not pollute the object buckets.
// (Link-level diff is a separate concern; this spec focuses on objects.)
func TestComputeDiff_Given_LinkEdits_When_Diff_Then_IgnoredForObjectBuckets(t *testing.T) {
	base := newStubBaseLoader()
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "addLink", LinkType: "departsFrom", SrcID: "F1", DstID: "JFK"},
		{Seq: 2, Op: "deleteLink", LinkType: "departsFrom", SrcID: "F2", DstID: "LGA"},
	}
	got, err := scenariodiff.Compute(context.Background(), edits, base)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if len(got.EditedObjects) != 0 || len(got.CreatedObjects) != 0 || len(got.DeletedObjects) != 0 || len(got.Deltas) != 0 {
		t.Fatalf("link edits should not affect object buckets, got %+v", got)
	}
}

// TestComputeDiff_Given_DeltasFlattened_When_Diff_Then_OrderedByObjectThenProperty
// pins down the deltas[] ordering — same (object, property) sort as
// editedObjects so callers can join the two slices by index if convenient.
func TestComputeDiff_Given_DeltasFlattened_When_Diff_Then_OrderedByObjectThenProperty(t *testing.T) {
	base := newStubBaseLoader(&scenarios.ObjectView{
		ObjectType: "Airport", ObjectID: "JFK",
		Properties: map[string]json.RawMessage{"capacity": raw(100), "name": raw("John F Kennedy")},
	})
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "name", NewValue: raw("JFK")},
		{Seq: 2, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: raw(150)},
	}
	got, err := scenariodiff.Compute(context.Background(), edits, base)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	gotProps := make([]string, 0, len(got.Deltas))
	for _, d := range got.Deltas {
		gotProps = append(gotProps, d.Property)
	}
	want := []string{"capacity", "name"}
	if !reflect.DeepEqual(gotProps, want) {
		t.Fatalf("deltas property order = %v, want %v", gotProps, want)
	}
	// Sanity: same property order within editedObjects.Changes.
	eo := got.EditedObjects[0]
	gotEoProps := make([]string, 0, len(eo.Changes))
	for _, c := range eo.Changes {
		gotEoProps = append(gotEoProps, c.Property)
	}
	if !reflect.DeepEqual(gotEoProps, want) {
		t.Fatalf("editedObjects[0].Changes property order = %v, want %v", gotEoProps, want)
	}
	// And: sort.StringsAreSorted asserts our sort key is correct.
	if !sort.StringsAreSorted(gotProps) {
		t.Fatalf("deltas property order not lexicographic: %v", gotProps)
	}
}
