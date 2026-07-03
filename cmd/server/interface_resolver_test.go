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
	// otInterfaces maps an objectTypeRID to the ObjectTypeInterface rows the
	// repo would return for it (each carrying a PropertyMapping).
	otInterfaces    map[string][]oms.ObjectTypeInterface
	otInterfacesErr error
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

func (s *stubInterfaceRepo) ListObjectTypeInterfaces(_ context.Context, objectTypeRID string) ([]oms.ObjectTypeInterface, error) {
	if s.otInterfacesErr != nil {
		return nil, s.otInterfacesErr
	}
	return s.otInterfaces[objectTypeRID], nil
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

func TestPGInterfaceResolver_ResolveInterfacePropertyMappings(t *testing.T) {
	const ifaceRID = "ri.ontology.main.interface.HasOwner"
	repo := &stubInterfaceRepo{
		ont:   &oms.Ontology{RID: "ri.ontology.main.ontology.northwind", APIName: "northwind"},
		iface: &oms.Interface{RID: ifaceRID, APIName: "HasOwner"},
		implementingList: []oms.ObjectType{
			{RID: "ri.ot.employee", APIName: "employee"},
			{RID: "ri.ot.vehicle", APIName: "vehicle"},
		},
		otInterfaces: map[string][]oms.ObjectTypeInterface{
			"ri.ot.employee": {
				// An unrelated interface implemented by the same object type must be
				// filtered out by interface RID.
				{ObjectTypeRID: "ri.ot.employee", InterfaceRID: "ri.other", PropertyMapping: []byte(`{"x":"y"}`)},
				{ObjectTypeRID: "ri.ot.employee", InterfaceRID: ifaceRID, PropertyMapping: []byte(`{"ownerName":"manager","ownerId":"empId"}`)},
			},
			"ri.ot.vehicle": {
				{ObjectTypeRID: "ri.ot.vehicle", InterfaceRID: ifaceRID, PropertyMapping: []byte(`{"ownerName":"driver","ownerId":"vin"}`)},
			},
		},
	}
	r := newPGInterfaceResolver(repo)
	ctx := index.WithOntologyScope(context.Background(), "northwind")

	got, err := r.ResolveInterfacePropertyMappings(ctx, "HasOwner")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected mappings for 2 object types, got %d: %v", len(got), got)
	}
	if got["employee"]["ownerName"] != "manager" || got["employee"]["ownerId"] != "empId" {
		t.Errorf("employee mapping wrong: %v", got["employee"])
	}
	if got["vehicle"]["ownerName"] != "driver" || got["vehicle"]["ownerId"] != "vin" {
		t.Errorf("vehicle mapping wrong: %v", got["vehicle"])
	}
}

func TestPGInterfaceResolver_ResolveInterfacePropertyMappings_EmptyMapping(t *testing.T) {
	const ifaceRID = "ri.ontology.main.interface.HasOwner"
	repo := &stubInterfaceRepo{
		ont:              &oms.Ontology{RID: "ri.ontology.main.ontology.nw", APIName: "nw"},
		iface:            &oms.Interface{RID: ifaceRID, APIName: "HasOwner"},
		implementingList: []oms.ObjectType{{RID: "ri.ot.employee", APIName: "employee"}},
		otInterfaces: map[string][]oms.ObjectTypeInterface{
			"ri.ot.employee": {
				{ObjectTypeRID: "ri.ot.employee", InterfaceRID: ifaceRID, PropertyMapping: []byte(`{}`)},
			},
		},
	}
	r := newPGInterfaceResolver(repo)
	ctx := index.WithOntologyScope(context.Background(), "nw")

	got, err := r.ResolveInterfacePropertyMappings(ctx, "HasOwner")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	empMap, ok := got["employee"]
	if !ok {
		t.Fatalf("expected employee key present with empty mapping, got %v", got)
	}
	if len(empMap) != 0 {
		t.Errorf("expected empty mapping for employee, got %v", empMap)
	}
}
