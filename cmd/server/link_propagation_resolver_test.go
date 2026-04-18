package main

import (
	"context"
	"errors"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// stubLinkPropagationRepo is a tiny in-memory repo satisfying
// linkPropagationResolverRepo. Each lookup returns a configured row or a
// configured error so tests can drive every code path of the resolver
// without spinning up Postgres.
type stubLinkPropagationRepo struct {
	linkTypes   map[string]*oms.LinkType
	objectTypes map[string]*oms.ObjectType
	linkErr     error
	otErr       error
}

func (s *stubLinkPropagationRepo) GetLinkType(_ context.Context, rid string) (*oms.LinkType, error) {
	if s.linkErr != nil {
		return nil, s.linkErr
	}
	if lt, ok := s.linkTypes[rid]; ok {
		return lt, nil
	}
	return nil, oms.ErrNotFound
}

func (s *stubLinkPropagationRepo) GetObjectType(_ context.Context, rid string) (*oms.ObjectType, error) {
	if s.otErr != nil {
		return nil, s.otErr
	}
	if ot, ok := s.objectTypes[rid]; ok {
		return ot, nil
	}
	return nil, oms.ErrNotFound
}

func TestLinkPropagationResolver_Lookup_PropagateAndAPINames(t *testing.T) {
	repo := &stubLinkPropagationRepo{
		linkTypes: map[string]*oms.LinkType{
			"ri.lt.parent-child": {
				RID:               "ri.lt.parent-child",
				SourceObjectType:  "ri.ot.parent",
				TargetObjectType:  "ri.ot.child",
				PropagateMarkings: true,
			},
		},
		objectTypes: map[string]*oms.ObjectType{
			"ri.ot.parent": {RID: "ri.ot.parent", APIName: "parent"},
			"ri.ot.child":  {RID: "ri.ot.child", APIName: "child"},
		},
	}
	r := newLinkPropagationResolver(repo)
	info, ok, err := r.LookupLinkPropagation(context.Background(), "ri.lt.parent-child")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("expected found=true, got false")
	}
	if !info.PropagateMarkings {
		t.Errorf("expected PropagateMarkings=true")
	}
	if info.SourceObjectTypeAPIName != "parent" {
		t.Errorf("source api name = %q, want %q", info.SourceObjectTypeAPIName, "parent")
	}
	if info.TargetObjectTypeAPIName != "child" {
		t.Errorf("target api name = %q, want %q", info.TargetObjectTypeAPIName, "child")
	}
}

func TestLinkPropagationResolver_Lookup_LinkTypeNotFound_SoftSkip(t *testing.T) {
	repo := &stubLinkPropagationRepo{linkTypes: map[string]*oms.LinkType{}}
	r := newLinkPropagationResolver(repo)
	info, ok, err := r.LookupLinkPropagation(context.Background(), "ri.lt.unknown")
	if err != nil {
		t.Fatalf("expected nil error for missing LinkType, got %v", err)
	}
	if ok {
		t.Fatalf("expected found=false, got true: %+v", info)
	}
}

func TestLinkPropagationResolver_Lookup_PropagateFalse_StillReturnsAPINames(t *testing.T) {
	// PropagateMarkings=false must still return found=true so the consumer
	// knows the LinkType exists; the consumer's own propagateMarkings()
	// short-circuits on the flag.
	repo := &stubLinkPropagationRepo{
		linkTypes: map[string]*oms.LinkType{
			"ri.lt.x": {
				RID:               "ri.lt.x",
				SourceObjectType:  "ri.ot.parent",
				TargetObjectType:  "ri.ot.child",
				PropagateMarkings: false,
			},
		},
		objectTypes: map[string]*oms.ObjectType{
			"ri.ot.parent": {RID: "ri.ot.parent", APIName: "parent"},
			"ri.ot.child":  {RID: "ri.ot.child", APIName: "child"},
		},
	}
	r := newLinkPropagationResolver(repo)
	info, ok, err := r.LookupLinkPropagation(context.Background(), "ri.lt.x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("expected found=true even when PropagateMarkings=false")
	}
	if info.PropagateMarkings {
		t.Errorf("expected PropagateMarkings=false")
	}
}

func TestLinkPropagationResolver_Lookup_GetLinkTypeError_Propagates(t *testing.T) {
	repo := &stubLinkPropagationRepo{linkErr: errors.New("pg down")}
	r := newLinkPropagationResolver(repo)
	_, _, err := r.LookupLinkPropagation(context.Background(), "ri.lt.x")
	if err == nil {
		t.Fatalf("expected error to propagate, got nil")
	}
}

func TestLinkPropagationResolver_Lookup_GetObjectTypeNotFound_SoftSkip(t *testing.T) {
	// LinkType exists but its ObjectType pointers are stale — the resolver
	// returns empty API names + found=true; the consumer then no-ops via
	// the empty-string guard inside propagateMarkings.
	repo := &stubLinkPropagationRepo{
		linkTypes: map[string]*oms.LinkType{
			"ri.lt.x": {
				RID:               "ri.lt.x",
				SourceObjectType:  "ri.ot.gone",
				TargetObjectType:  "ri.ot.also-gone",
				PropagateMarkings: true,
			},
		},
		objectTypes: map[string]*oms.ObjectType{},
	}
	r := newLinkPropagationResolver(repo)
	info, ok, err := r.LookupLinkPropagation(context.Background(), "ri.lt.x")
	if err != nil {
		t.Fatalf("expected nil error for missing ObjectType, got %v", err)
	}
	if !ok {
		t.Fatalf("expected found=true")
	}
	if info.SourceObjectTypeAPIName != "" || info.TargetObjectTypeAPIName != "" {
		t.Errorf("expected empty API names for missing ObjectTypes, got %+v", info)
	}
}

func TestLinkPropagationResolver_Lookup_EmptyLinkRID_NoLookup(t *testing.T) {
	repo := &stubLinkPropagationRepo{}
	r := newLinkPropagationResolver(repo)
	_, ok, err := r.LookupLinkPropagation(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("expected found=false for empty RID")
	}
}
