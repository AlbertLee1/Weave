package scenarios_test

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/liyang/weave/pkg/scenarios"
)

// US-481 — Fold returns (view, deleted, []ScenarioConflict). The PRD names two
// conflict shapes explicitly:
//   - "deleted+modify"  → a modifyProperty edit on an object that was already
//     deleted by an earlier edit in the same scenario
//   - "duplicate add"   → two createObject edits for the same (objectType,
//     objectID) with no intervening deleteObject
//
// We also cover delete-after-delete (idempotent delete is fine, but two
// independent delete intents on the same key are operator-visible) and the
// link-side analogues (addLink dup / deleteLink of missing edge) because
// FoldLinks is the other half of the fold chain and shares the audit sink.

func raw481(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func TestUS481_FoldObjectWithConflicts_ModifyAfterDelete(t *testing.T) {
	target := scenarios.ObjectKey{ObjectType: "Airport", ObjectID: "JFK"}
	base := &scenarios.ObjectView{
		ObjectType: "Airport",
		ObjectID:   "JFK",
		Properties: map[string]json.RawMessage{"capacity": raw481(100)},
	}
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "deleteObject", ObjectType: "Airport", ObjectID: "JFK"},
		{Seq: 2, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: raw481(120)},
	}

	view, deleted, conflicts := scenarios.FoldObjectWithConflicts(target, base, edits)
	if view != nil || !deleted {
		t.Fatalf("expected (nil, true) view/deleted; got view=%v deleted=%v", view, deleted)
	}
	if len(conflicts) != 1 {
		t.Fatalf("expected exactly 1 conflict, got %d (%+v)", len(conflicts), conflicts)
	}
	c := conflicts[0]
	if c.ConflictType != scenarios.ConflictModifyAfterDelete {
		t.Errorf("conflict type: got %q want %q", c.ConflictType, scenarios.ConflictModifyAfterDelete)
	}
	if c.ObjectType != "Airport" || c.ObjectID != "JFK" {
		t.Errorf("conflict target: got (%s,%s)", c.ObjectType, c.ObjectID)
	}
	if c.Op != "modifyProperty" || c.Property != "capacity" {
		t.Errorf("conflict op/prop: got (%s,%s)", c.Op, c.Property)
	}
	// EditSeqs must include both the delete seq (the suppressor) and the modify seq.
	if len(c.EditSeqs) != 2 || c.EditSeqs[0] != 1 || c.EditSeqs[1] != 2 {
		t.Errorf("conflict editSeqs: got %v want [1 2]", c.EditSeqs)
	}
}

func TestUS481_FoldObjectWithConflicts_DuplicateCreate(t *testing.T) {
	target := scenarios.ObjectKey{ObjectType: "Order", ObjectID: "O-1"}
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "createObject", ObjectType: "Order", ObjectID: "O-1", NewValue: raw481(map[string]any{"total": 10})},
		{Seq: 2, Op: "createObject", ObjectType: "Order", ObjectID: "O-1", NewValue: raw481(map[string]any{"total": 20})},
	}

	view, deleted, conflicts := scenarios.FoldObjectWithConflicts(target, nil, edits)
	if deleted {
		t.Fatalf("duplicate create should not result in deleted=true")
	}
	if view == nil {
		t.Fatalf("expected fold to yield the last create's view")
	}
	if string(view.Properties["total"]) != "20" {
		t.Errorf("last-write-wins: got total=%s want 20", string(view.Properties["total"]))
	}
	if len(conflicts) != 1 {
		t.Fatalf("expected exactly 1 conflict, got %d (%+v)", len(conflicts), conflicts)
	}
	c := conflicts[0]
	if c.ConflictType != scenarios.ConflictDuplicateCreate {
		t.Errorf("conflict type: got %q want %q", c.ConflictType, scenarios.ConflictDuplicateCreate)
	}
	if c.Op != "createObject" {
		t.Errorf("conflict op: got %q want createObject", c.Op)
	}
	if len(c.EditSeqs) != 2 || c.EditSeqs[0] != 1 || c.EditSeqs[1] != 2 {
		t.Errorf("conflict editSeqs: got %v want [1 2]", c.EditSeqs)
	}
}

func TestUS481_FoldObjectWithConflicts_DeleteAfterDelete(t *testing.T) {
	target := scenarios.ObjectKey{ObjectType: "Airport", ObjectID: "JFK"}
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "deleteObject", ObjectType: "Airport", ObjectID: "JFK"},
		{Seq: 2, Op: "deleteObject", ObjectType: "Airport", ObjectID: "JFK"},
	}

	view, deleted, conflicts := scenarios.FoldObjectWithConflicts(target, nil, edits)
	if view != nil || !deleted {
		t.Fatalf("expected (nil, true); got view=%v deleted=%v", view, deleted)
	}
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d (%+v)", len(conflicts), conflicts)
	}
	if conflicts[0].ConflictType != scenarios.ConflictDeleteAfterDelete {
		t.Errorf("conflict type: got %q want %q", conflicts[0].ConflictType, scenarios.ConflictDeleteAfterDelete)
	}
}

func TestUS481_FoldObjectWithConflicts_RecreateAfterDeleteIsNotAConflict(t *testing.T) {
	// modify-after-delete is a conflict; recreate-after-delete is not (it's
	// the documented "object cycle" path in FoldObject's BDD #2/#3).
	target := scenarios.ObjectKey{ObjectType: "Airport", ObjectID: "JFK"}
	base := &scenarios.ObjectView{
		ObjectType: "Airport",
		ObjectID:   "JFK",
		Properties: map[string]json.RawMessage{"capacity": raw481(100)},
	}
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "deleteObject", ObjectType: "Airport", ObjectID: "JFK"},
		{Seq: 2, Op: "createObject", ObjectType: "Airport", ObjectID: "JFK", NewValue: raw481(map[string]any{"capacity": 7})},
		{Seq: 3, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: raw481(9)},
	}
	view, deleted, conflicts := scenarios.FoldObjectWithConflicts(target, base, edits)
	if deleted || view == nil {
		t.Fatalf("recreate-after-delete should yield live view; got view=%v deleted=%v", view, deleted)
	}
	if len(conflicts) != 0 {
		t.Errorf("expected zero conflicts, got %d (%+v)", len(conflicts), conflicts)
	}
}

func TestUS481_FoldObjectWithConflicts_HappyPathNoConflicts(t *testing.T) {
	target := scenarios.ObjectKey{ObjectType: "Airport", ObjectID: "JFK"}
	base := &scenarios.ObjectView{
		ObjectType: "Airport",
		ObjectID:   "JFK",
		Properties: map[string]json.RawMessage{"capacity": raw481(100)},
	}
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: raw481(150)},
	}
	view, deleted, conflicts := scenarios.FoldObjectWithConflicts(target, base, edits)
	if deleted || view == nil {
		t.Fatalf("happy path: got view=%v deleted=%v", view, deleted)
	}
	if len(conflicts) != 0 {
		t.Errorf("happy path: expected 0 conflicts, got %d (%+v)", len(conflicts), conflicts)
	}
}

func TestUS481_FoldObjectWithConflicts_LegacyFoldObjectUnchanged(t *testing.T) {
	// Behavioural guarantee: the legacy two-return FoldObject keeps emitting
	// the same view/deleted bits as before — callers in pkg/oss/handlers.go
	// only see the new signal if they upgrade to FoldObjectWithConflicts.
	target := scenarios.ObjectKey{ObjectType: "Order", ObjectID: "O-1"}
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "createObject", ObjectType: "Order", ObjectID: "O-1", NewValue: raw481(map[string]any{"total": 10})},
		{Seq: 2, Op: "createObject", ObjectType: "Order", ObjectID: "O-1", NewValue: raw481(map[string]any{"total": 20})},
	}
	view, deleted := scenarios.FoldObject(target, nil, edits)
	if deleted || view == nil {
		t.Fatalf("legacy: got view=%v deleted=%v", view, deleted)
	}
	if string(view.Properties["total"]) != "20" {
		t.Errorf("legacy: last-write-wins broken; got total=%s want 20", string(view.Properties["total"]))
	}
}

func TestUS481_FoldLinksWithConflicts_DuplicateAddAndDeleteMissing(t *testing.T) {
	base := []scenarios.LinkView{
		{LinkType: "FlightTo", SrcID: "JFK", DstID: "LAX"},
	}
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "addLink", LinkType: "FlightTo", SrcID: "JFK", DstID: "LAX"},
		{Seq: 2, Op: "deleteLink", LinkType: "FlightTo", SrcID: "JFK", DstID: "SFO"},
	}

	links, conflicts := scenarios.FoldLinksWithConflicts(base, edits)
	if len(links) != 1 || links[0] != base[0] {
		t.Fatalf("expected unchanged base; got %+v", links)
	}
	if len(conflicts) != 2 {
		t.Fatalf("expected 2 conflicts, got %d (%+v)", len(conflicts), conflicts)
	}
	// sort for deterministic assertions (slice order is fold-step order; the
	// two conflicts here happen to come in seq order but we don't lock that).
	sort.Slice(conflicts, func(i, j int) bool { return conflicts[i].EditSeqs[0] < conflicts[j].EditSeqs[0] })

	if conflicts[0].ConflictType != scenarios.ConflictDuplicateAddLink {
		t.Errorf("c[0] type: got %q", conflicts[0].ConflictType)
	}
	if conflicts[0].LinkType != "FlightTo" || conflicts[0].SrcID != "JFK" || conflicts[0].DstID != "LAX" {
		t.Errorf("c[0] target: %+v", conflicts[0])
	}
	if conflicts[1].ConflictType != scenarios.ConflictDeleteMissingLink {
		t.Errorf("c[1] type: got %q", conflicts[1].ConflictType)
	}
	if conflicts[1].DstID != "SFO" {
		t.Errorf("c[1] dst: %+v", conflicts[1])
	}
}

func TestUS481_FoldLinksWithConflicts_HappyPathNoConflicts(t *testing.T) {
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "addLink", LinkType: "Owns", SrcID: "A", DstID: "B"},
	}
	links, conflicts := scenarios.FoldLinksWithConflicts(nil, edits)
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if len(conflicts) != 0 {
		t.Errorf("expected 0 conflicts, got %d (%+v)", len(conflicts), conflicts)
	}
}
