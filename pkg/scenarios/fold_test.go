package scenarios_test

import (
	"encoding/json"
	"testing"

	"github.com/liyang/weave/pkg/scenarios"
)

func raw(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// TestFoldObject_TableDriven covers the BDD acceptance criteria for Fold over a
// single object: base+modify, base+delete, no-base+create, ignore-others, and
// the boundary cases listed in PRD VTX-003 (re-create, modify-after-delete).
func TestFoldObject_TableDriven(t *testing.T) {
	jfk := &scenarios.ObjectView{
		ObjectType: "Airport",
		ObjectID:   "JFK",
		Properties: map[string]json.RawMessage{
			"capacity": raw(100),
			"name":     raw("John F Kennedy"),
		},
	}
	target := scenarios.ObjectKey{ObjectType: "Airport", ObjectID: "JFK"}

	cases := []struct {
		name        string
		target      scenarios.ObjectKey
		base        *scenarios.ObjectView
		edits       []scenarios.ScenarioEdit
		wantDeleted bool
		wantNil     bool
		wantProps   map[string]json.RawMessage
	}{
		{
			name:    "no base + no edits => nil view, not deleted",
			target:  target,
			base:    nil,
			wantNil: true,
		},
		{
			name:      "base + no edits => unchanged",
			target:    target,
			base:      jfk,
			wantProps: map[string]json.RawMessage{"capacity": raw(100), "name": raw("John F Kennedy")},
		},
		{
			name:   "two modifyProperty => later wins (BDD #1)",
			target: target,
			base:   jfk,
			edits: []scenarios.ScenarioEdit{
				{Seq: 1, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: raw(120)},
				{Seq: 2, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: raw(150)},
			},
			wantProps: map[string]json.RawMessage{"capacity": raw(150), "name": raw("John F Kennedy")},
		},
		{
			name:   "deleteObject => (nil, true) (BDD #2)",
			target: target,
			base:   jfk,
			edits: []scenarios.ScenarioEdit{
				{Seq: 1, Op: "deleteObject", ObjectType: "Airport", ObjectID: "JFK"},
			},
			wantDeleted: true,
			wantNil:     true,
		},
		{
			name:   "no base + createObject => new view (BDD #3)",
			target: scenarios.ObjectKey{ObjectType: "Order", ObjectID: "O-1"},
			edits: []scenarios.ScenarioEdit{
				{Seq: 1, Op: "createObject", ObjectType: "Order", ObjectID: "O-1", NewValue: raw(map[string]any{"total": 99, "status": "pending"})},
			},
			wantProps: map[string]json.RawMessage{"total": raw(99), "status": raw("pending")},
		},
		{
			name:   "re-create after delete => no carryover of old props",
			target: target,
			base:   jfk,
			edits: []scenarios.ScenarioEdit{
				{Seq: 1, Op: "deleteObject", ObjectType: "Airport", ObjectID: "JFK"},
				{Seq: 2, Op: "createObject", ObjectType: "Airport", ObjectID: "JFK", NewValue: raw(map[string]any{"capacity": 999})},
			},
			wantDeleted: false,
			wantProps:   map[string]json.RawMessage{"capacity": raw(999)},
		},
		{
			name:   "modify-after-delete is ignored",
			target: target,
			base:   jfk,
			edits: []scenarios.ScenarioEdit{
				{Seq: 1, Op: "deleteObject", ObjectType: "Airport", ObjectID: "JFK"},
				{Seq: 2, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: raw(500)},
			},
			wantDeleted: true,
			wantNil:     true,
		},
		{
			name:   "createObject then modifyProperty",
			target: scenarios.ObjectKey{ObjectType: "Order", ObjectID: "O-2"},
			edits: []scenarios.ScenarioEdit{
				{Seq: 1, Op: "createObject", ObjectType: "Order", ObjectID: "O-2", NewValue: raw(map[string]any{"total": 10})},
				{Seq: 2, Op: "modifyProperty", ObjectType: "Order", ObjectID: "O-2", Property: "total", NewValue: raw(42)},
			},
			wantProps: map[string]json.RawMessage{"total": raw(42)},
		},
		{
			name:   "edits targeting other objects are ignored",
			target: target,
			base:   jfk,
			edits: []scenarios.ScenarioEdit{
				{Seq: 1, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "LAX", Property: "capacity", NewValue: raw(7)},
				{Seq: 2, Op: "deleteObject", ObjectType: "Airport", ObjectID: "ORD"},
				{Seq: 3, Op: "modifyProperty", ObjectType: "Customer", ObjectID: "JFK", Property: "capacity", NewValue: raw(8)},
			},
			wantProps: map[string]json.RawMessage{"capacity": raw(100), "name": raw("John F Kennedy")},
		},
		{
			name:   "edits supplied out of seq order get sorted internally",
			target: target,
			base:   jfk,
			edits: []scenarios.ScenarioEdit{
				{Seq: 7, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: raw(7)},
				{Seq: 3, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: raw(3)},
				{Seq: 5, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: raw(5)},
			},
			wantProps: map[string]json.RawMessage{"capacity": raw(7), "name": raw("John F Kennedy")},
		},
		{
			name:   "createObject over existing base replaces props",
			target: target,
			base:   jfk,
			edits: []scenarios.ScenarioEdit{
				{Seq: 1, Op: "createObject", ObjectType: "Airport", ObjectID: "JFK", NewValue: raw(map[string]any{"capacity": 1})},
			},
			wantProps: map[string]json.RawMessage{"capacity": raw(1)},
		},
		{
			name:   "addLink / deleteLink edits do not affect object fold",
			target: target,
			base:   jfk,
			edits: []scenarios.ScenarioEdit{
				{Seq: 1, Op: "addLink", LinkType: "Owns", SrcID: "JFK", DstID: "T1"},
				{Seq: 2, Op: "deleteLink", LinkType: "Owns", SrcID: "JFK", DstID: "T1"},
			},
			wantProps: map[string]json.RawMessage{"capacity": raw(100), "name": raw("John F Kennedy")},
		},
		{
			name:   "modify => delete => recreate => modify (state cycle)",
			target: target,
			base:   jfk,
			edits: []scenarios.ScenarioEdit{
				{Seq: 1, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: raw(120)},
				{Seq: 2, Op: "deleteObject", ObjectType: "Airport", ObjectID: "JFK"},
				{Seq: 3, Op: "createObject", ObjectType: "Airport", ObjectID: "JFK", NewValue: raw(map[string]any{"capacity": 9})},
				{Seq: 4, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: raw(11)},
			},
			wantProps: map[string]json.RawMessage{"capacity": raw(11)},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, deleted := scenarios.FoldObject(tc.target, tc.base, tc.edits)
			if deleted != tc.wantDeleted {
				t.Fatalf("deleted: got %v want %v", deleted, tc.wantDeleted)
			}
			if tc.wantNil {
				if got != nil {
					t.Fatalf("expected nil view, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil view, got nil")
			}
			if got.ObjectType != tc.target.ObjectType || got.ObjectID != tc.target.ObjectID {
				t.Errorf("identity: got (%s,%s) want (%s,%s)",
					got.ObjectType, got.ObjectID, tc.target.ObjectType, tc.target.ObjectID)
			}
			if len(got.Properties) != len(tc.wantProps) {
				t.Errorf("props len: got %d want %d (got=%v)", len(got.Properties), len(tc.wantProps), got.Properties)
			}
			for k, v := range tc.wantProps {
				if string(got.Properties[k]) != string(v) {
					t.Errorf("prop %q: got %s want %s", k, string(got.Properties[k]), string(v))
				}
			}
		})
	}
}

// TestFoldLinks_TableDriven covers BDD #4 (link_add / link_delete adjacency
// merge) plus determinism / dedup / cross-type isolation / cycle preservation.
func TestFoldLinks_TableDriven(t *testing.T) {
	cases := []struct {
		name  string
		base  []scenarios.LinkView
		edits []scenarios.ScenarioEdit
		want  []scenarios.LinkView
	}{
		{
			name: "no base + no edits => empty",
			want: []scenarios.LinkView{},
		},
		{
			name: "addLink on empty base",
			edits: []scenarios.ScenarioEdit{
				{Seq: 1, Op: "addLink", LinkType: "FlightTo", SrcID: "JFK", DstID: "LAX"},
			},
			want: []scenarios.LinkView{{LinkType: "FlightTo", SrcID: "JFK", DstID: "LAX"}},
		},
		{
			name: "addLink duplicate of base edge is deduped",
			base: []scenarios.LinkView{{LinkType: "FlightTo", SrcID: "JFK", DstID: "LAX"}},
			edits: []scenarios.ScenarioEdit{
				{Seq: 1, Op: "addLink", LinkType: "FlightTo", SrcID: "JFK", DstID: "LAX"},
			},
			want: []scenarios.LinkView{{LinkType: "FlightTo", SrcID: "JFK", DstID: "LAX"}},
		},
		{
			name: "deleteLink removes one existing edge",
			base: []scenarios.LinkView{
				{LinkType: "FlightTo", SrcID: "JFK", DstID: "LAX"},
				{LinkType: "FlightTo", SrcID: "JFK", DstID: "ORD"},
			},
			edits: []scenarios.ScenarioEdit{
				{Seq: 1, Op: "deleteLink", LinkType: "FlightTo", SrcID: "JFK", DstID: "LAX"},
			},
			want: []scenarios.LinkView{{LinkType: "FlightTo", SrcID: "JFK", DstID: "ORD"}},
		},
		{
			name: "deleteLink of missing edge is no-op",
			base: []scenarios.LinkView{{LinkType: "FlightTo", SrcID: "JFK", DstID: "LAX"}},
			edits: []scenarios.ScenarioEdit{
				{Seq: 1, Op: "deleteLink", LinkType: "FlightTo", SrcID: "JFK", DstID: "SFO"},
			},
			want: []scenarios.LinkView{{LinkType: "FlightTo", SrcID: "JFK", DstID: "LAX"}},
		},
		{
			name: "add then delete same edge => empty",
			edits: []scenarios.ScenarioEdit{
				{Seq: 1, Op: "addLink", LinkType: "FlightTo", SrcID: "JFK", DstID: "LAX"},
				{Seq: 2, Op: "deleteLink", LinkType: "FlightTo", SrcID: "JFK", DstID: "LAX"},
			},
			want: []scenarios.LinkView{},
		},
		{
			name: "delete then re-add => present",
			base: []scenarios.LinkView{{LinkType: "FlightTo", SrcID: "JFK", DstID: "LAX"}},
			edits: []scenarios.ScenarioEdit{
				{Seq: 1, Op: "deleteLink", LinkType: "FlightTo", SrcID: "JFK", DstID: "LAX"},
				{Seq: 2, Op: "addLink", LinkType: "FlightTo", SrcID: "JFK", DstID: "LAX"},
			},
			want: []scenarios.LinkView{{LinkType: "FlightTo", SrcID: "JFK", DstID: "LAX"}},
		},
		{
			name: "cycle A->B and B->A are both preserved",
			edits: []scenarios.ScenarioEdit{
				{Seq: 1, Op: "addLink", LinkType: "Knows", SrcID: "A", DstID: "B"},
				{Seq: 2, Op: "addLink", LinkType: "Knows", SrcID: "B", DstID: "A"},
			},
			want: []scenarios.LinkView{
				{LinkType: "Knows", SrcID: "A", DstID: "B"},
				{LinkType: "Knows", SrcID: "B", DstID: "A"},
			},
		},
		{
			name: "multiple link types preserved separately and sorted",
			edits: []scenarios.ScenarioEdit{
				{Seq: 1, Op: "addLink", LinkType: "Owns", SrcID: "JFK", DstID: "T1"},
				{Seq: 2, Op: "addLink", LinkType: "FlightTo", SrcID: "JFK", DstID: "LAX"},
			},
			want: []scenarios.LinkView{
				{LinkType: "FlightTo", SrcID: "JFK", DstID: "LAX"},
				{LinkType: "Owns", SrcID: "JFK", DstID: "T1"},
			},
		},
		{
			name: "object-shaped edits ignored by FoldLinks",
			edits: []scenarios.ScenarioEdit{
				{Seq: 1, Op: "createObject", ObjectType: "Airport", ObjectID: "JFK"},
				{Seq: 2, Op: "deleteObject", ObjectType: "Airport", ObjectID: "JFK"},
				{Seq: 3, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "p", NewValue: raw(1)},
			},
			want: []scenarios.LinkView{},
		},
		{
			name: "edits out of seq order get sorted internally",
			edits: []scenarios.ScenarioEdit{
				{Seq: 2, Op: "deleteLink", LinkType: "FlightTo", SrcID: "JFK", DstID: "LAX"},
				{Seq: 1, Op: "addLink", LinkType: "FlightTo", SrcID: "JFK", DstID: "LAX"},
			},
			want: []scenarios.LinkView{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scenarios.FoldLinks(tc.base, tc.edits)
			if len(got) != len(tc.want) {
				t.Fatalf("len: got %d want %d (got=%v)", len(got), len(tc.want), got)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("idx %d: got %+v want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// BenchmarkFoldObject_1000Edits validates the BDD perf bound: 1000 edits folded
// in ≤ 5 ms. Run: `go test -bench BenchmarkFoldObject_1000Edits -benchmem ./pkg/scenarios/...`
func BenchmarkFoldObject_1000Edits(b *testing.B) {
	target := scenarios.ObjectKey{ObjectType: "Airport", ObjectID: "JFK"}
	base := &scenarios.ObjectView{
		ObjectType: "Airport",
		ObjectID:   "JFK",
		Properties: map[string]json.RawMessage{"capacity": raw(100)},
	}
	edits := make([]scenarios.ScenarioEdit, 1000)
	for i := range edits {
		edits[i] = scenarios.ScenarioEdit{
			Seq: int64(i + 1), Op: "modifyProperty",
			ObjectType: "Airport", ObjectID: "JFK",
			Property: "capacity", NewValue: raw(i),
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = scenarios.FoldObject(target, base, edits)
	}
}
