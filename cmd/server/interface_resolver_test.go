package main

import (
	"context"
	"errors"
	"testing"

	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
)

type stubInterfaceRepo struct {
	ont              *oms.Ontology
	ontErr           error
	lookupRID        string
	lookupAPIName    string
	iface            *oms.Interface
	ifaceErr         error
	lookupIfaceRID   string
	implementingList []oms.ObjectType
	implementingErr  error
}

func (s *stubInterfaceRepo) GetOntology(_ context.Context, ridOrApiName string) (*oms.Ontology, error) {
	s.lookupRID = ridOrApiName
	if s.ontErr != nil {
		return nil, s.ontErr
	}
	return s.ont, nil
}

func (s *stubInterfaceRepo) GetInterfaceByAPIName(_ context.Context, ontologyRID, apiName string) (*oms.Interface, error) {
	s.lookupAPIName = apiName
	if s.ifaceErr != nil {
		return nil, s.ifaceErr
	}
	return s.iface, nil
}

func (s *stubInterfaceRepo) ListInterfaceObjectTypes(_ context.Context, interfaceRID string) ([]oms.ObjectType, error) {
	s.lookupIfaceRID = interfaceRID
	if s.implementingErr != nil {
		return nil, s.implementingErr
	}
	return s.implementingList, nil
}

func TestPGInterfaceResolver_ResolveInterfaceObjectTypes(t *testing.T) {
	repo := &stubInterfaceRepo{
		ont:   &oms.Ontology{RID: "ri.ontology.main.ontology.northwind", APIName: "northwind"},
		iface: &oms.Interface{RID: "ri.ontology.main.interface.HasOwner", APIName: "HasOwner"},
		implementingList: []oms.ObjectType{
			{APIName: "customer"},
			{APIName: "order"},
			{APIName: "product"},
		},
	}
	r := newPGInterfaceResolver(repo)

	ctx := index.WithOntologyScope(context.Background(), "northwind")
	names, err := r.ResolveInterfaceObjectTypes(ctx, "HasOwner")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"customer", "order", "product"}
	if len(names) != len(want) {
		t.Fatalf("expected %d names, got %d: %v", len(want), len(names), names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("names[%d]: got %q, want %q", i, names[i], want[i])
		}
	}
	if repo.lookupRID != "northwind" {
		t.Errorf("expected GetOntology called with %q, got %q", "northwind", repo.lookupRID)
	}
	if repo.lookupAPIName != "HasOwner" {
		t.Errorf("expected GetInterfaceByAPIName called with %q, got %q", "HasOwner", repo.lookupAPIName)
	}
	if repo.lookupIfaceRID != "ri.ontology.main.interface.HasOwner" {
		t.Errorf("expected ListInterfaceObjectTypes called with iface RID, got %q", repo.lookupIfaceRID)
	}
}

func TestPGInterfaceResolver_MissingScope(t *testing.T) {
	r := newPGInterfaceResolver(&stubInterfaceRepo{})
	_, err := r.ResolveInterfaceObjectTypes(context.Background(), "HasOwner")
	if err == nil {
		t.Fatal("expected error when ontology scope is missing, got nil")
	}
}

func TestPGInterfaceResolver_InterfaceLookupError(t *testing.T) {
	boom := errors.New("boom")
	repo := &stubInterfaceRepo{
		ont:      &oms.Ontology{RID: "ri.ontology.main.ontology.northwind", APIName: "northwind"},
		ifaceErr: boom,
	}
	r := newPGInterfaceResolver(repo)
	ctx := index.WithOntologyScope(context.Background(), "northwind")
	if _, err := r.ResolveInterfaceObjectTypes(ctx, "HasOwner"); err == nil {
		t.Fatal("expected interface lookup error to propagate, got nil")
	}
}
