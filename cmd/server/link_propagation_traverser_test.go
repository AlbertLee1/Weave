package main

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
)

// stubTraverserRepo is a tiny in-memory linkPropagationTraverserRepo. The
// adapter only calls ListOntologies / ListLinkTypes / GetObjectType, so the
// fake mirrors those three with a per-RID map.
type stubTraverserRepo struct {
	onts        []oms.Ontology
	linkTypes   map[string][]oms.LinkType // ontologyRID -> LinkTypes
	objectTypes map[string]*oms.ObjectType
	listOntErr  error
	listLTErr   error
	getOTErr    error
}

func (s *stubTraverserRepo) ListOntologies(_ context.Context) ([]oms.Ontology, error) {
	if s.listOntErr != nil {
		return nil, s.listOntErr
	}
	return s.onts, nil
}

func (s *stubTraverserRepo) ListLinkTypes(_ context.Context, ontologyRID string) ([]oms.LinkType, error) {
	if s.listLTErr != nil {
		return nil, s.listLTErr
	}
	return s.linkTypes[ontologyRID], nil
}

func (s *stubTraverserRepo) GetObjectType(_ context.Context, rid string) (*oms.ObjectType, error) {
	if s.getOTErr != nil {
		return nil, s.getOTErr
	}
	if ot, ok := s.objectTypes[rid]; ok {
		return ot, nil
	}
	return nil, oms.ErrNotFound
}

// stubEdgeTargetLister captures the (linkTypeRID, sourcePKs) inputs the
// traverser calls into and replays a configured per-LinkType target map.
type stubEdgeTargetLister struct {
	byLinkType map[string][]string // linkTypeRID -> target PKs
	calls      []struct {
		LinkTypeRID string
		SourcePKs   []string
	}
	err error
}

func (s *stubEdgeTargetLister) ListEdgeTargets(_ context.Context, linkTypeRID string, sourcePKs []string) ([]string, error) {
	cp := append([]string(nil), sourcePKs...)
	s.calls = append(s.calls, struct {
		LinkTypeRID string
		SourcePKs   []string
	}{linkTypeRID, cp})
	if s.err != nil {
		return nil, s.err
	}
	return append([]string(nil), s.byLinkType[linkTypeRID]...), nil
}

func TestLinkPropagationTraverser_Refresh_BuildsPropagatingBySourceAPIName(t *testing.T) {
	repo := &stubTraverserRepo{
		onts: []oms.Ontology{{RID: "ri.ont.x"}},
		linkTypes: map[string][]oms.LinkType{
			"ri.ont.x": {
				{RID: "ri.lt.AB", SourceObjectType: "ri.ot.a", TargetObjectType: "ri.ot.b", PropagateMarkings: true},
				{RID: "ri.lt.BC", SourceObjectType: "ri.ot.b", TargetObjectType: "ri.ot.c", PropagateMarkings: true},
				{RID: "ri.lt.BD", SourceObjectType: "ri.ot.b", TargetObjectType: "ri.ot.d", PropagateMarkings: false}, // excluded
			},
		},
		objectTypes: map[string]*oms.ObjectType{
			"ri.ot.a": {RID: "ri.ot.a", APIName: "a"},
			"ri.ot.b": {RID: "ri.ot.b", APIName: "b"},
			"ri.ot.c": {RID: "ri.ot.c", APIName: "c"},
			"ri.ot.d": {RID: "ri.ot.d", APIName: "d"},
		},
	}
	edges := &stubEdgeTargetLister{}
	tr := newLinkPropagationTraverser(repo, edges)
	if err := tr.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// Source "a" -> just the AB propagating link.
	got, err := tr.ListPropagatingOutgoingEdges(context.Background(), "a", []string{"a1"})
	if err != nil {
		t.Fatalf("ListPropagatingOutgoingEdges a: %v", err)
	}
	// edges stub returns no targets — but the LinkType lookup happened.
	if len(got) != 0 {
		t.Fatalf("expected 0 targets when stubEdgeTargetLister has no entries, got %v", got)
	}
	if len(edges.calls) != 1 || edges.calls[0].LinkTypeRID != "ri.lt.AB" {
		t.Fatalf("expected one ListEdgeTargets call against ri.lt.AB, got %+v", edges.calls)
	}

	// Wire edges and re-query for source "b" — only the propagating BC link
	// should be visited (BD is propagate=false and must be excluded).
	edges.byLinkType = map[string][]string{
		"ri.lt.BC": {"c1", "c2"},
		"ri.lt.BD": {"d1"}, // would leak if BD weren't filtered out
	}
	edges.calls = nil
	got, err = tr.ListPropagatingOutgoingEdges(context.Background(), "b", []string{"b1"})
	if err != nil {
		t.Fatalf("ListPropagatingOutgoingEdges b: %v", err)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].TargetPK < got[j].TargetPK })
	want := []funnel.PropagatingOutgoingEdge{
		{LinkTypeRID: "ri.lt.BC", TargetObjectTypeAPIName: "c", TargetPK: "c1"},
		{LinkTypeRID: "ri.lt.BC", TargetObjectTypeAPIName: "c", TargetPK: "c2"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("source=b\n  got:  %v\n  want: %v", got, want)
	}
	// Ensure the traverser didn't query BD at all.
	for _, c := range edges.calls {
		if c.LinkTypeRID == "ri.lt.BD" {
			t.Fatalf("non-propagating LinkType ri.lt.BD must not be queried, calls=%+v", edges.calls)
		}
	}
}

func TestLinkPropagationTraverser_Refresh_BubblesListOntologiesError(t *testing.T) {
	repo := &stubTraverserRepo{listOntErr: errors.New("pg dead")}
	tr := newLinkPropagationTraverser(repo, &stubEdgeTargetLister{})
	if err := tr.Refresh(context.Background()); err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestLinkPropagationTraverser_ListEdges_NilRepoOrEmptyInputs_NoOp(t *testing.T) {
	tr := &linkPropagationTraverser{} // zero value
	got, err := tr.ListPropagatingOutgoingEdges(context.Background(), "a", []string{"a1"})
	if err != nil {
		t.Fatalf("nil-repo traverser must not error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil result for nil repo, got %v", got)
	}

	tr2 := newLinkPropagationTraverser(&stubTraverserRepo{}, &stubEdgeTargetLister{})
	if _, err := tr2.ListPropagatingOutgoingEdges(context.Background(), "", nil); err != nil {
		t.Fatalf("empty inputs must not error: %v", err)
	}
}

func TestLinkPropagationTraverser_Refresh_SkipsLinkTypesWithMissingObjectTypes(t *testing.T) {
	repo := &stubTraverserRepo{
		onts: []oms.Ontology{{RID: "ri.ont.x"}},
		linkTypes: map[string][]oms.LinkType{
			"ri.ont.x": {
				// Source OT missing from the map → resolveAPI returns ""+nil
				// and the LinkType is skipped (treated as soft skip).
				{RID: "ri.lt.bad", SourceObjectType: "ri.ot.missing", TargetObjectType: "ri.ot.b", PropagateMarkings: true},
			},
		},
		objectTypes: map[string]*oms.ObjectType{
			"ri.ot.b": {RID: "ri.ot.b", APIName: "b"},
		},
	}
	tr := newLinkPropagationTraverser(repo, &stubEdgeTargetLister{})
	if err := tr.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh must tolerate missing ObjectType: %v", err)
	}
	got, err := tr.ListPropagatingOutgoingEdges(context.Background(), "b", []string{"b1"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got != nil {
		t.Fatalf("expected no edges (missing source OT excluded), got %v", got)
	}
}
