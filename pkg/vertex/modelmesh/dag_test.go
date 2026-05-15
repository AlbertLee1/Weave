package modelmesh_test

import (
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/vertex/modelmesh"
)

func TestBuildDependencyGraph_Given_OutputFeedsInput_When_Build_Then_EdgeAdded(t *testing.T) {
	models := []modelmesh.ModelNode{
		{ID: "m1", OutputProperties: []string{"A"}},
		{ID: "m2", InputProperties: []string{"A"}},
	}
	graph, err := modelmesh.BuildDependencyGraph(models)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	deps, ok := graph["m1"]
	if !ok {
		t.Fatalf("expected m1 in graph, got %v", graph)
	}
	if len(deps) != 1 || deps[0] != "m2" {
		t.Fatalf("expected m1 -> [m2], got %v", deps)
	}
	if len(graph["m2"]) != 0 {
		t.Fatalf("expected no outgoing edges from m2, got %v", graph["m2"])
	}
}

func TestBuildDependencyGraph_Given_NoSharedProperties_When_Build_Then_Disconnected(t *testing.T) {
	models := []modelmesh.ModelNode{
		{ID: "m1", OutputProperties: []string{"A"}},
		{ID: "m2", InputProperties: []string{"B"}},
	}
	graph, err := modelmesh.BuildDependencyGraph(models)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(graph["m1"]) != 0 || len(graph["m2"]) != 0 {
		t.Fatalf("expected no edges, got %v", graph)
	}
}

func TestBuildDependencyGraph_Given_DuplicateIDs_When_Build_Then_ErrDuplicate(t *testing.T) {
	models := []modelmesh.ModelNode{
		{ID: "m1", OutputProperties: []string{"A"}},
		{ID: "m1", OutputProperties: []string{"B"}},
	}
	_, err := modelmesh.BuildDependencyGraph(models)
	if !errors.Is(err, modelmesh.ErrDuplicateModelID) {
		t.Fatalf("expected ErrDuplicateModelID, got %v", err)
	}
}

func TestBuildDependencyGraph_Given_EmptyID_When_Build_Then_ErrEmptyID(t *testing.T) {
	models := []modelmesh.ModelNode{
		{ID: "", OutputProperties: []string{"A"}},
	}
	_, err := modelmesh.BuildDependencyGraph(models)
	if !errors.Is(err, modelmesh.ErrEmptyModelID) {
		t.Fatalf("expected ErrEmptyModelID, got %v", err)
	}
}

func TestTopologicalLayers_Given_LinearChain_When_Sort_Then_OneNodePerLayerInOrder(t *testing.T) {
	models := []modelmesh.ModelNode{
		{ID: "m2", InputProperties: []string{"A"}, OutputProperties: []string{"B"}},
		{ID: "m1", OutputProperties: []string{"A"}},
		{ID: "m3", InputProperties: []string{"B"}},
	}
	layers, err := modelmesh.TopologicalLayers(models)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(layers) != 3 {
		t.Fatalf("expected 3 layers, got %d: %v", len(layers), layers)
	}
	if layers[0][0] != "m1" || layers[1][0] != "m2" || layers[2][0] != "m3" {
		t.Fatalf("expected [[m1] [m2] [m3]], got %v", layers)
	}
}

func TestTopologicalLayers_Given_FanOut_When_Sort_Then_SiblingsShareLayerSorted(t *testing.T) {
	models := []modelmesh.ModelNode{
		{ID: "m1", OutputProperties: []string{"A"}},
		{ID: "m3", InputProperties: []string{"A"}, OutputProperties: []string{"C"}},
		{ID: "m2", InputProperties: []string{"A"}, OutputProperties: []string{"B"}},
	}
	layers, err := modelmesh.TopologicalLayers(models)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(layers) != 2 {
		t.Fatalf("expected 2 layers, got %d", len(layers))
	}
	if len(layers[0]) != 1 || layers[0][0] != "m1" {
		t.Fatalf("expected layer 0 = [m1], got %v", layers[0])
	}
	got := append([]string(nil), layers[1]...)
	sort.Strings(got)
	if len(got) != 2 || got[0] != "m2" || got[1] != "m3" {
		t.Fatalf("expected layer 1 = [m2 m3] (sorted), got %v", layers[1])
	}
}

func TestTopologicalLayers_Given_DiamondDependency_When_Sort_Then_ThreeLayers(t *testing.T) {
	models := []modelmesh.ModelNode{
		{ID: "root", OutputProperties: []string{"A"}},
		{ID: "left", InputProperties: []string{"A"}, OutputProperties: []string{"L"}},
		{ID: "right", InputProperties: []string{"A"}, OutputProperties: []string{"R"}},
		{ID: "join", InputProperties: []string{"L", "R"}},
	}
	layers, err := modelmesh.TopologicalLayers(models)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(layers) != 3 {
		t.Fatalf("expected 3 layers, got %d: %v", len(layers), layers)
	}
	if len(layers[0]) != 1 || layers[0][0] != "root" {
		t.Fatalf("expected layer 0 = [root], got %v", layers[0])
	}
	if len(layers[1]) != 2 {
		t.Fatalf("expected layer 1 to have 2 nodes, got %v", layers[1])
	}
	if len(layers[2]) != 1 || layers[2][0] != "join" {
		t.Fatalf("expected layer 2 = [join], got %v", layers[2])
	}
}

func TestTopologicalLayers_Given_Cycle_When_Sort_Then_ErrCycleDetected(t *testing.T) {
	models := []modelmesh.ModelNode{
		{ID: "m1", InputProperties: []string{"B"}, OutputProperties: []string{"A"}},
		{ID: "m2", InputProperties: []string{"A"}, OutputProperties: []string{"B"}},
	}
	_, err := modelmesh.TopologicalLayers(models)
	if !errors.Is(err, modelmesh.ErrCycleDetected) {
		t.Fatalf("expected ErrCycleDetected, got %v", err)
	}
	var ce *modelmesh.CycleError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *CycleError, got %T (%v)", err, err)
	}
	if len(ce.Cycle) < 2 {
		t.Fatalf("expected cycle path to contain at least 2 nodes, got %v", ce.Cycle)
	}
	first := ce.Cycle[0]
	last := ce.Cycle[len(ce.Cycle)-1]
	if first != last {
		t.Fatalf("expected cycle path to start and end at same node, got %v", ce.Cycle)
	}
	for _, n := range ce.Cycle {
		if n != "m1" && n != "m2" {
			t.Fatalf("cycle path contains unknown node %q (%v)", n, ce.Cycle)
		}
	}
}

func TestTopologicalLayers_Given_SelfLoop_When_Sort_Then_ErrCycleDetected(t *testing.T) {
	models := []modelmesh.ModelNode{
		{ID: "m1", InputProperties: []string{"A"}, OutputProperties: []string{"A"}},
	}
	_, err := modelmesh.TopologicalLayers(models)
	if !errors.Is(err, modelmesh.ErrCycleDetected) {
		t.Fatalf("expected ErrCycleDetected on self-loop, got %v", err)
	}
}

func TestTopologicalLayers_Given_Empty_When_Sort_Then_NoLayers(t *testing.T) {
	layers, err := modelmesh.TopologicalLayers(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(layers) != 0 {
		t.Fatalf("expected 0 layers, got %d", len(layers))
	}
}

func TestCycleError_Error_ContainsCyclePath(t *testing.T) {
	ce := &modelmesh.CycleError{Cycle: []string{"m1", "m2", "m1"}}
	msg := ce.Error()
	if !strings.Contains(msg, "m1") || !strings.Contains(msg, "m2") {
		t.Fatalf("expected cycle path in error message, got %q", msg)
	}
	if !errors.Is(ce, modelmesh.ErrCycleDetected) {
		t.Fatalf("expected CycleError to wrap ErrCycleDetected")
	}
}
