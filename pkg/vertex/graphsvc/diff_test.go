package graphsvc

import (
	"reflect"
	"testing"
)

func node(id string, x, y float64, color string, props map[string]any) SnapshotNode {
	return SnapshotNode{
		ID:         id,
		Position:   NodePosition{X: x, Y: y},
		Style:      NodeStyle{Color: color, Size: 1},
		Properties: props,
	}
}

func TestDiff_Given_TwoSnapshots_When_Compared_Then_AddedRemovedModifiedDetected(t *testing.T) {
	from := GraphSnapshot{
		Version: "v3",
		Nodes: []SnapshotNode{
			node("a", 0, 0, "blue", map[string]any{"label": "Alpha"}),
			node("b", 10, 10, "red", map[string]any{"label": "Beta"}),
		},
	}
	to := GraphSnapshot{
		Version: "v5",
		Nodes: []SnapshotNode{
			node("a", 5, 0, "blue", map[string]any{"label": "Alpha"}),    // moved
			node("b", 10, 10, "green", map[string]any{"label": "Beta"}),  // color changed
			node("c", 20, 20, "purple", map[string]any{"label": "Gamma"}),// added
		},
	}

	diff := Diff(from, to)
	if !reflect.DeepEqual(diff.AddedNodes, []string{"c"}) {
		t.Errorf("AddedNodes = %v", diff.AddedNodes)
	}
	if !reflect.DeepEqual(diff.RemovedNodes, []string{}) {
		t.Errorf("RemovedNodes = %v", diff.RemovedNodes)
	}
	if len(diff.StyleChanges) != 1 || diff.StyleChanges[0].NodeID != "b" {
		t.Errorf("StyleChanges = %+v", diff.StyleChanges)
	}
	if len(diff.LayoutChanges) != 1 || diff.LayoutChanges[0].NodeID != "a" {
		t.Errorf("LayoutChanges = %+v", diff.LayoutChanges)
	}
}

func TestDiff_Given_NodeRemoved_When_Diffed_Then_AppearsInRemoved(t *testing.T) {
	from := GraphSnapshot{Nodes: []SnapshotNode{node("a", 0, 0, "blue", nil)}}
	to := GraphSnapshot{}
	diff := Diff(from, to)
	if !reflect.DeepEqual(diff.RemovedNodes, []string{"a"}) {
		t.Errorf("RemovedNodes = %v", diff.RemovedNodes)
	}
}

func TestDiff_Given_PropertyChanged_When_Diffed_Then_NodeAppearsInModified(t *testing.T) {
	from := GraphSnapshot{Nodes: []SnapshotNode{node("a", 0, 0, "blue", map[string]any{"label": "Alpha"})}}
	to := GraphSnapshot{Nodes: []SnapshotNode{node("a", 0, 0, "blue", map[string]any{"label": "Alpha2"})}}
	diff := Diff(from, to)
	if !reflect.DeepEqual(diff.ModifiedNodes, []string{"a"}) {
		t.Errorf("ModifiedNodes = %v", diff.ModifiedNodes)
	}
}

func TestDiff_Given_OnlyStyleOrLayoutChange_When_Diffed_Then_NotInModified(t *testing.T) {
	from := GraphSnapshot{Nodes: []SnapshotNode{node("a", 0, 0, "blue", nil)}}
	to := GraphSnapshot{Nodes: []SnapshotNode{node("a", 0, 0, "red", nil)}}
	diff := Diff(from, to)
	if len(diff.ModifiedNodes) != 0 {
		t.Errorf("style-only change should not appear in ModifiedNodes; got %v", diff.ModifiedNodes)
	}
	if len(diff.StyleChanges) != 1 {
		t.Errorf("StyleChanges = %+v", diff.StyleChanges)
	}
}
