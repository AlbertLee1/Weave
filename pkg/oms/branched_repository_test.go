package oms

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// --- BranchedRepository unit tests (US-115) ---

// helper: build a BranchChange for an entity.
func makeBranchChange(branchID, changeType, entityType, entityRID string, before, after interface{}) BranchChange {
	var beforeJSON, afterJSON json.RawMessage
	if before != nil {
		b, _ := json.Marshal(before)
		beforeJSON = b
	}
	if after != nil {
		b, _ := json.Marshal(after)
		afterJSON = b
	}
	return BranchChange{
		ID:          "bc-" + entityRID,
		BranchID:    branchID,
		ChangeType:  changeType,
		EntityType:  entityType,
		EntityRID:   entityRID,
		BeforeState: beforeJSON,
		AfterState:  afterJSON,
	}
}

// --- ListObjectTypes overlay ---

func TestBranchedRepo_ListObjectTypes_Added(t *testing.T) {
	base := &inMemoryRepo{
		objectTypes: []ObjectType{
			{RID: "ot-1", OntologyRID: "ont-1", APIName: "employee", DisplayName: "Employee", PrimaryKey: "id", Status: "ACTIVE", Visibility: "NORMAL"},
		},
		branchChanges: []BranchChange{
			makeBranchChange("br-1", "ADDED", "objectType", "ot-new",
				nil,
				ObjectType{RID: "ot-new", OntologyRID: "ont-1", APIName: "department", DisplayName: "Department", PrimaryKey: "deptId", Status: "ACTIVE", Visibility: "NORMAL"}),
		},
	}
	repo := NewBranchedRepository(base, "br-1")

	list, err := repo.ListObjectTypes(context.Background(), "ont-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 objectTypes (main + added), got %d", len(list))
	}

	// Verify the added one is present
	found := false
	for _, ot := range list {
		if ot.APIName == "department" {
			found = true
			if ot.RID != "ot-new" {
				t.Errorf("expected RID=ot-new, got %s", ot.RID)
			}
		}
	}
	if !found {
		t.Error("expected branch-added 'department' in list")
	}
}

func TestBranchedRepo_ListObjectTypes_Modified(t *testing.T) {
	base := &inMemoryRepo{
		objectTypes: []ObjectType{
			{RID: "ot-1", OntologyRID: "ont-1", APIName: "employee", DisplayName: "Employee", PrimaryKey: "id", Status: "ACTIVE", Visibility: "NORMAL"},
		},
		branchChanges: []BranchChange{
			makeBranchChange("br-1", "MODIFIED", "objectType", "ot-1",
				ObjectType{RID: "ot-1", APIName: "employee", DisplayName: "Employee"},
				ObjectType{RID: "ot-1", OntologyRID: "ont-1", APIName: "employee", DisplayName: "Updated Employee", PrimaryKey: "id", Status: "ACTIVE", Visibility: "NORMAL"}),
		},
	}
	repo := NewBranchedRepository(base, "br-1")

	list, err := repo.ListObjectTypes(context.Background(), "ont-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 objectType (modified), got %d", len(list))
	}
	if list[0].DisplayName != "Updated Employee" {
		t.Errorf("expected modified displayName='Updated Employee', got '%s'", list[0].DisplayName)
	}
}

func TestBranchedRepo_ListObjectTypes_Deleted(t *testing.T) {
	base := &inMemoryRepo{
		objectTypes: []ObjectType{
			{RID: "ot-1", OntologyRID: "ont-1", APIName: "employee", DisplayName: "Employee", PrimaryKey: "id", Status: "ACTIVE", Visibility: "NORMAL"},
			{RID: "ot-2", OntologyRID: "ont-1", APIName: "department", DisplayName: "Department", PrimaryKey: "deptId", Status: "ACTIVE", Visibility: "NORMAL"},
		},
		branchChanges: []BranchChange{
			makeBranchChange("br-1", "DELETED", "objectType", "ot-1",
				ObjectType{RID: "ot-1", APIName: "employee", DisplayName: "Employee"},
				nil),
		},
	}
	repo := NewBranchedRepository(base, "br-1")

	list, err := repo.ListObjectTypes(context.Background(), "ont-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 objectType (deleted ot-1), got %d", len(list))
	}
	if list[0].APIName != "department" {
		t.Errorf("expected remaining objectType='department', got '%s'", list[0].APIName)
	}
}

// --- GetObjectTypeByAPIName overlay ---

func TestBranchedRepo_GetObjectTypeByAPIName_Added(t *testing.T) {
	base := &inMemoryRepo{
		branchChanges: []BranchChange{
			makeBranchChange("br-1", "ADDED", "objectType", "ot-new",
				nil,
				ObjectType{RID: "ot-new", OntologyRID: "ont-1", APIName: "department", DisplayName: "Department", PrimaryKey: "deptId", Status: "ACTIVE", Visibility: "NORMAL"}),
		},
	}
	repo := NewBranchedRepository(base, "br-1")

	ot, err := repo.GetObjectTypeByAPIName(context.Background(), "ont-1", "department")
	if err != nil {
		t.Fatal(err)
	}
	if ot.APIName != "department" {
		t.Errorf("expected apiName=department, got %s", ot.APIName)
	}
	if ot.DisplayName != "Department" {
		t.Errorf("expected displayName=Department, got %s", ot.DisplayName)
	}
}

func TestBranchedRepo_GetObjectTypeByAPIName_Modified(t *testing.T) {
	base := &inMemoryRepo{
		objectTypes: []ObjectType{
			{RID: "ot-1", OntologyRID: "ont-1", APIName: "employee", DisplayName: "Employee", PrimaryKey: "id", Status: "ACTIVE", Visibility: "NORMAL"},
		},
		branchChanges: []BranchChange{
			makeBranchChange("br-1", "MODIFIED", "objectType", "ot-1",
				ObjectType{RID: "ot-1", APIName: "employee", DisplayName: "Employee"},
				ObjectType{RID: "ot-1", OntologyRID: "ont-1", APIName: "employee", DisplayName: "Updated Employee", PrimaryKey: "id", Status: "ACTIVE", Visibility: "NORMAL"}),
		},
	}
	repo := NewBranchedRepository(base, "br-1")

	ot, err := repo.GetObjectTypeByAPIName(context.Background(), "ont-1", "employee")
	if err != nil {
		t.Fatal(err)
	}
	if ot.DisplayName != "Updated Employee" {
		t.Errorf("expected modified displayName='Updated Employee', got '%s'", ot.DisplayName)
	}
}

func TestBranchedRepo_GetObjectTypeByAPIName_Deleted(t *testing.T) {
	base := &inMemoryRepo{
		objectTypes: []ObjectType{
			{RID: "ot-1", OntologyRID: "ont-1", APIName: "employee", DisplayName: "Employee", PrimaryKey: "id", Status: "ACTIVE", Visibility: "NORMAL"},
		},
		branchChanges: []BranchChange{
			makeBranchChange("br-1", "DELETED", "objectType", "ot-1",
				ObjectType{RID: "ot-1", APIName: "employee"},
				nil),
		},
	}
	repo := NewBranchedRepository(base, "br-1")

	_, err := repo.GetObjectTypeByAPIName(context.Background(), "ont-1", "employee")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for deleted objectType, got %v", err)
	}
}

func TestBranchedRepo_GetObjectTypeByAPIName_NoChange(t *testing.T) {
	base := &inMemoryRepo{
		objectTypes: []ObjectType{
			{RID: "ot-1", OntologyRID: "ont-1", APIName: "employee", DisplayName: "Employee", PrimaryKey: "id", Status: "ACTIVE", Visibility: "NORMAL"},
		},
	}
	repo := NewBranchedRepository(base, "br-1")

	ot, err := repo.GetObjectTypeByAPIName(context.Background(), "ont-1", "employee")
	if err != nil {
		t.Fatal(err)
	}
	if ot.DisplayName != "Employee" {
		t.Errorf("expected original displayName='Employee', got '%s'", ot.DisplayName)
	}
}

func TestBranchedRepo_GetObjectTypeByAPIName_NotFound(t *testing.T) {
	base := &inMemoryRepo{}
	repo := NewBranchedRepository(base, "br-1")

	_, err := repo.GetObjectTypeByAPIName(context.Background(), "ont-1", "nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// --- ListProperties overlay ---

func TestBranchedRepo_ListProperties_Overlay(t *testing.T) {
	base := &inMemoryRepo{
		properties: []Property{
			{RID: "p-1", ObjectTypeRID: "ot-1", APIName: "firstName", DisplayName: "First Name", BaseType: "String"},
			{RID: "p-2", ObjectTypeRID: "ot-1", APIName: "lastName", DisplayName: "Last Name", BaseType: "String"},
		},
		branchChanges: []BranchChange{
			// Modify firstName
			makeBranchChange("br-1", "MODIFIED", "property", "p-1",
				Property{RID: "p-1", APIName: "firstName", DisplayName: "First Name"},
				Property{RID: "p-1", ObjectTypeRID: "ot-1", APIName: "firstName", DisplayName: "Given Name", BaseType: "String"}),
			// Add a new property
			makeBranchChange("br-1", "ADDED", "property", "p-new",
				nil,
				Property{RID: "p-new", ObjectTypeRID: "ot-1", APIName: "middleName", DisplayName: "Middle Name", BaseType: "String"}),
		},
	}
	repo := NewBranchedRepository(base, "br-1")

	list, err := repo.ListProperties(context.Background(), "ot-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 properties (2 main, -0 deleted, +1 added), got %d", len(list))
	}

	// Verify modification
	for _, p := range list {
		if p.APIName == "firstName" && p.DisplayName != "Given Name" {
			t.Errorf("expected firstName displayName='Given Name', got '%s'", p.DisplayName)
		}
	}

	// Verify addition
	found := false
	for _, p := range list {
		if p.APIName == "middleName" {
			found = true
		}
	}
	if !found {
		t.Error("expected branch-added 'middleName' in properties")
	}
}

// --- ListLinkTypes overlay ---

func TestBranchedRepo_ListLinkTypes_Overlay(t *testing.T) {
	base := &inMemoryRepo{
		linkTypes: []LinkType{
			{RID: "lt-1", OntologyRID: "ont-1", APIName: "empDept", DisplayName: "Emp Dept", Cardinality: "MANY_TO_ONE"},
		},
		branchChanges: []BranchChange{
			makeBranchChange("br-1", "ADDED", "linkType", "lt-new",
				nil,
				LinkType{RID: "lt-new", OntologyRID: "ont-1", APIName: "empManager", DisplayName: "Emp Manager", Cardinality: "MANY_TO_ONE"}),
			makeBranchChange("br-1", "MODIFIED", "linkType", "lt-1",
				LinkType{RID: "lt-1", APIName: "empDept", DisplayName: "Emp Dept"},
				LinkType{RID: "lt-1", OntologyRID: "ont-1", APIName: "empDept", DisplayName: "Employee Department", Cardinality: "MANY_TO_ONE"}),
		},
	}
	repo := NewBranchedRepository(base, "br-1")

	list, err := repo.ListLinkTypes(context.Background(), "ont-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 linkTypes, got %d", len(list))
	}

	for _, lt := range list {
		if lt.APIName == "empDept" && lt.DisplayName != "Employee Department" {
			t.Errorf("expected modified displayName='Employee Department', got '%s'", lt.DisplayName)
		}
	}
}

// --- ListActionTypes overlay ---

func TestBranchedRepo_ListActionTypes_Overlay(t *testing.T) {
	base := &inMemoryRepo{
		actionTypes: []ActionType{
			{RID: "at-1", OntologyRID: "ont-1", APIName: "createEmp", DisplayName: "Create Employee", Status: "ACTIVE"},
		},
		branchChanges: []BranchChange{
			makeBranchChange("br-1", "DELETED", "actionType", "at-1",
				ActionType{RID: "at-1", APIName: "createEmp"},
				nil),
		},
	}
	repo := NewBranchedRepository(base, "br-1")

	list, err := repo.ListActionTypes(context.Background(), "ont-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 actionTypes (deleted), got %d", len(list))
	}
}

// --- GetActionTypeByAPIName overlay ---

func TestBranchedRepo_GetActionTypeByAPIName_Modified(t *testing.T) {
	base := &inMemoryRepo{
		ontologies: []Ontology{
			{RID: "ont-1", APIName: "test"},
		},
		actionTypes: []ActionType{
			{RID: "at-1", OntologyRID: "ont-1", APIName: "createEmp", DisplayName: "Create Employee", Status: "ACTIVE"},
		},
		branchChanges: []BranchChange{
			makeBranchChange("br-1", "MODIFIED", "actionType", "at-1",
				ActionType{RID: "at-1", APIName: "createEmp", DisplayName: "Create Employee"},
				ActionType{RID: "at-1", OntologyRID: "ont-1", APIName: "createEmp", DisplayName: "Create Employee V2", Status: "ACTIVE"}),
		},
	}
	repo := NewBranchedRepository(base, "br-1")

	at, err := repo.GetActionTypeByAPIName(context.Background(), "test", "createEmp")
	if err != nil {
		t.Fatal(err)
	}
	if at.DisplayName != "Create Employee V2" {
		t.Errorf("expected modified displayName='Create Employee V2', got '%s'", at.DisplayName)
	}
}

// --- Mixed changes on same branch ---

func TestBranchedRepo_MixedChanges(t *testing.T) {
	base := &inMemoryRepo{
		objectTypes: []ObjectType{
			{RID: "ot-1", OntologyRID: "ont-1", APIName: "employee", DisplayName: "Employee", PrimaryKey: "id", Status: "ACTIVE", Visibility: "NORMAL"},
			{RID: "ot-2", OntologyRID: "ont-1", APIName: "order", DisplayName: "Order", PrimaryKey: "orderId", Status: "ACTIVE", Visibility: "NORMAL"},
		},
		branchChanges: []BranchChange{
			// Add a new type
			makeBranchChange("br-1", "ADDED", "objectType", "ot-new",
				nil,
				ObjectType{RID: "ot-new", OntologyRID: "ont-1", APIName: "product", DisplayName: "Product", PrimaryKey: "productId", Status: "ACTIVE", Visibility: "NORMAL"}),
			// Modify employee
			makeBranchChange("br-1", "MODIFIED", "objectType", "ot-1",
				ObjectType{RID: "ot-1", APIName: "employee", DisplayName: "Employee"},
				ObjectType{RID: "ot-1", OntologyRID: "ont-1", APIName: "employee", DisplayName: "Team Member", PrimaryKey: "id", Status: "ACTIVE", Visibility: "NORMAL"}),
			// Delete order
			makeBranchChange("br-1", "DELETED", "objectType", "ot-2",
				ObjectType{RID: "ot-2", APIName: "order"},
				nil),
		},
	}
	repo := NewBranchedRepository(base, "br-1")

	list, err := repo.ListObjectTypes(context.Background(), "ont-1")
	if err != nil {
		t.Fatal(err)
	}
	// employee (modified) + product (added) = 2 (order deleted)
	if len(list) != 2 {
		t.Fatalf("expected 2 objectTypes, got %d", len(list))
	}

	names := map[string]string{}
	for _, ot := range list {
		names[ot.APIName] = ot.DisplayName
	}
	if names["employee"] != "Team Member" {
		t.Errorf("expected employee displayName='Team Member', got '%s'", names["employee"])
	}
	if _, ok := names["product"]; !ok {
		t.Error("expected branch-added 'product' in list")
	}
	if _, ok := names["order"]; ok {
		t.Error("expected 'order' to be removed (deleted on branch)")
	}
}

// --- Main reads unaffected by branch ---

func TestBranchedRepo_MainReadsUnaffected(t *testing.T) {
	base := &inMemoryRepo{
		objectTypes: []ObjectType{
			{RID: "ot-1", OntologyRID: "ont-1", APIName: "employee", DisplayName: "Employee", PrimaryKey: "id", Status: "ACTIVE", Visibility: "NORMAL"},
		},
		branchChanges: []BranchChange{
			makeBranchChange("br-1", "MODIFIED", "objectType", "ot-1",
				ObjectType{RID: "ot-1", APIName: "employee", DisplayName: "Employee"},
				ObjectType{RID: "ot-1", OntologyRID: "ont-1", APIName: "employee", DisplayName: "Updated Employee", PrimaryKey: "id", Status: "ACTIVE", Visibility: "NORMAL"}),
		},
	}

	// Read from base (main) — should be unmodified
	list, err := base.ListObjectTypes(context.Background(), "ont-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].DisplayName != "Employee" {
		t.Errorf("expected main to be unmodified, got displayName='%s'", list[0].DisplayName)
	}

	// Read from branched repo — should be modified
	branched := NewBranchedRepository(base, "br-1")
	bList, err := branched.ListObjectTypes(context.Background(), "ont-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(bList) != 1 || bList[0].DisplayName != "Updated Employee" {
		t.Errorf("expected branch view to show 'Updated Employee', got '%s'", bList[0].DisplayName)
	}
}

// --- inMemoryRepo: minimal in-package Repository for BranchedRepository tests ---

type inMemoryRepo struct {
	Repository // embed interface for unimplemented methods
	ontologies    []Ontology
	objectTypes   []ObjectType
	properties    []Property
	linkTypes     []LinkType
	actionTypes   []ActionType
	branchChanges []BranchChange
}

func (r *inMemoryRepo) GetOntology(_ context.Context, ridOrApiName string) (*Ontology, error) {
	for i := range r.ontologies {
		if r.ontologies[i].RID == ridOrApiName || r.ontologies[i].APIName == ridOrApiName {
			return &r.ontologies[i], nil
		}
	}
	return nil, ErrNotFound
}

func (r *inMemoryRepo) ListObjectTypes(_ context.Context, ontologyRID string) ([]ObjectType, error) {
	var result []ObjectType
	for _, ot := range r.objectTypes {
		if ot.OntologyRID == ontologyRID {
			result = append(result, ot)
		}
	}
	return result, nil
}

func (r *inMemoryRepo) GetObjectTypeByAPIName(_ context.Context, ontologyRID, apiName string) (*ObjectType, error) {
	for i := range r.objectTypes {
		ontologyMatch := r.objectTypes[i].OntologyRID == ontologyRID
		if !ontologyMatch {
			for _, o := range r.ontologies {
				if o.APIName == ontologyRID && o.RID == r.objectTypes[i].OntologyRID {
					ontologyMatch = true
					break
				}
			}
		}
		if ontologyMatch && r.objectTypes[i].APIName == apiName {
			return &r.objectTypes[i], nil
		}
	}
	return nil, ErrNotFound
}

func (r *inMemoryRepo) ListProperties(_ context.Context, objectTypeRID string) ([]Property, error) {
	var result []Property
	for _, p := range r.properties {
		if p.ObjectTypeRID == objectTypeRID {
			result = append(result, p)
		}
	}
	return result, nil
}

func (r *inMemoryRepo) ListLinkTypes(_ context.Context, ontologyRID string) ([]LinkType, error) {
	var result []LinkType
	for _, lt := range r.linkTypes {
		if lt.OntologyRID == ontologyRID {
			result = append(result, lt)
		}
	}
	return result, nil
}

func (r *inMemoryRepo) ListActionTypes(_ context.Context, ontologyRID string) ([]ActionType, error) {
	var result []ActionType
	for _, at := range r.actionTypes {
		if at.OntologyRID == ontologyRID {
			result = append(result, at)
		}
	}
	return result, nil
}

func (r *inMemoryRepo) GetActionTypeByAPIName(_ context.Context, ontologyRID, apiNameOrRID string) (*ActionType, error) {
	for i := range r.actionTypes {
		ontologyMatch := r.actionTypes[i].OntologyRID == ontologyRID
		if !ontologyMatch {
			for _, o := range r.ontologies {
				if o.APIName == ontologyRID && o.RID == r.actionTypes[i].OntologyRID {
					ontologyMatch = true
					break
				}
			}
		}
		if ontologyMatch && (r.actionTypes[i].APIName == apiNameOrRID || r.actionTypes[i].RID == apiNameOrRID) {
			return &r.actionTypes[i], nil
		}
	}
	return nil, ErrNotFound
}

func (r *inMemoryRepo) ListBranchChanges(_ context.Context, branchID string) ([]BranchChange, error) {
	var result []BranchChange
	for _, c := range r.branchChanges {
		if c.BranchID == branchID {
			result = append(result, c)
		}
	}
	return result, nil
}

func (r *inMemoryRepo) GetBranch(_ context.Context, id string) (*OntologyBranch, error) {
	return nil, ErrNotFound
}
