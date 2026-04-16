package oms_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// --- Branch read overlay handler tests (US-115) ---

// branchReadMockRepo builds a repo with main data + branch changes for read tests.
func branchReadMockRepo() *mockRepo {
	return &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test"},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ot-1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "employee", DisplayName: "Employee", PrimaryKey: "employeeId", Status: "ACTIVE", Visibility: "NORMAL"},
		},
		properties: []oms.Property{
			{RID: "p-1", ObjectTypeRID: "ot-1", APIName: "employeeId", BaseType: "integer"},
		},
		linkTypes: []oms.LinkType{
			{RID: "lt-1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "empDept", DisplayName: "Emp Dept", SourceObjectType: "ot-1", TargetObjectType: "ot-2", Cardinality: "MANY_TO_ONE"},
		},
		actionTypes: []oms.ActionType{
			{RID: "at-1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "createEmp", DisplayName: "Create Employee", Status: "ACTIVE", Parameters: json.RawMessage(`[]`)},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/test", Status: "open", BaseVersion: 1},
		},
		branchChanges: []oms.BranchChange{
			// Modified: employee display name changed
			branchChange("br-1", "MODIFIED", "objectType", "ot-1",
				oms.ObjectType{RID: "ot-1", APIName: "employee", DisplayName: "Employee"},
				oms.ObjectType{RID: "ot-1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "employee", DisplayName: "Team Member", PrimaryKey: "employeeId", Status: "ACTIVE", Visibility: "NORMAL"}),
			// Added: new objectType
			branchChange("br-1", "ADDED", "objectType", "ot-new",
				nil,
				oms.ObjectType{RID: "ot-new", OntologyRID: "ri.ontology.main.ontology.1", APIName: "department", DisplayName: "Department", PrimaryKey: "deptId", Status: "ACTIVE", Visibility: "NORMAL"}),
			// Modified: action type display name changed
			branchChange("br-1", "MODIFIED", "actionType", "at-1",
				oms.ActionType{RID: "at-1", APIName: "createEmp", DisplayName: "Create Employee"},
				oms.ActionType{RID: "at-1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "createEmp", DisplayName: "Create Team Member", Status: "ACTIVE", Parameters: json.RawMessage(`[]`)}),
		},
	}
}

// branchChange is a test helper to build a BranchChange with JSON-serialized before/after state.
func branchChange(branchID, changeType, entityType, entityRID string, before, after interface{}) oms.BranchChange {
	var beforeJSON, afterJSON json.RawMessage
	if before != nil {
		b, _ := json.Marshal(before)
		beforeJSON = b
	}
	if after != nil {
		b, _ := json.Marshal(after)
		afterJSON = b
	}
	return oms.BranchChange{
		ID:          "bc-" + entityRID,
		BranchID:    branchID,
		ChangeType:  changeType,
		EntityType:  entityType,
		EntityRID:   entityRID,
		BeforeState: beforeJSON,
		AfterState:  afterJSON,
	}
}

func TestListObjectTypes_WithBranch(t *testing.T) {
	repo := branchReadMockRepo()
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes", handler.ListObjectTypes)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1/objectTypes?branch=br-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	body := parseJSON(t, w.Body.Bytes())
	data, ok := body["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}
	// Should have 2: modified employee + added department
	if len(data) != 2 {
		t.Fatalf("expected 2 objectTypes on branch, got %d", len(data))
	}

	names := map[string]string{}
	for _, item := range data {
		m := item.(map[string]interface{})
		names[m["apiName"].(string)] = m["displayName"].(string)
	}
	if names["employee"] != "Team Member" {
		t.Errorf("expected employee renamed to 'Team Member' on branch, got '%s'", names["employee"])
	}
	if _, ok := names["department"]; !ok {
		t.Error("expected branch-added 'department' in list")
	}
}

func TestListObjectTypes_WithoutBranch_Unchanged(t *testing.T) {
	repo := branchReadMockRepo()
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes", handler.ListObjectTypes)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1/objectTypes", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := parseJSON(t, w.Body.Bytes())
	data := body["data"].([]interface{})
	// Should have only 1 (main employee)
	if len(data) != 1 {
		t.Fatalf("expected 1 objectType on main, got %d", len(data))
	}
}

func TestGetObjectType_WithBranch_Modified(t *testing.T) {
	repo := branchReadMockRepo()
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}", handler.GetObjectType)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1/objectTypes/employee?branch=br-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	body := parseJSON(t, w.Body.Bytes())
	if body["displayName"] != "Team Member" {
		t.Errorf("expected branch-modified displayName='Team Member', got '%s'", body["displayName"])
	}
}

func TestGetObjectType_WithBranch_Added(t *testing.T) {
	repo := branchReadMockRepo()
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}", handler.GetObjectType)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1/objectTypes/department?branch=br-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for branch-added objectType, got %d: %s", w.Code, w.Body.String())
	}

	body := parseJSON(t, w.Body.Bytes())
	if body["displayName"] != "Department" {
		t.Errorf("expected displayName='Department', got '%s'", body["displayName"])
	}
}

func TestGetObjectType_WithBranch_NotOnBranch(t *testing.T) {
	repo := branchReadMockRepo()
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}", handler.GetObjectType)

	// 'department' doesn't exist on main and no branch → 404
	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1/objectTypes/department", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for 'department' on main (not branch), got %d", w.Code)
	}
}

func TestListActionTypes_WithBranch(t *testing.T) {
	repo := branchReadMockRepo()
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypes", handler.ListActionTypes)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1/actionTypes?branch=br-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := parseJSON(t, w.Body.Bytes())
	data := body["data"].([]interface{})
	if len(data) != 1 {
		t.Fatalf("expected 1 actionType, got %d", len(data))
	}
	at := data[0].(map[string]interface{})
	if at["displayName"] != "Create Team Member" {
		t.Errorf("expected branch-modified displayName='Create Team Member', got '%v'", at["displayName"])
	}
}

func TestGetActionType_WithBranch(t *testing.T) {
	repo := branchReadMockRepo()
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypes/{actionTypeRid}", handler.GetActionType)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1/actionTypes/createEmp?branch=br-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	body := parseJSON(t, w.Body.Bytes())
	if body["displayName"] != "Create Team Member" {
		t.Errorf("expected branch-modified displayName='Create Team Member', got '%v'", body["displayName"])
	}
}

func TestBranchRead_BranchNotFound(t *testing.T) {
	repo := branchReadMockRepo()
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes", handler.ListObjectTypes)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1/objectTypes?branch=nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for nonexistent branch, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetFullMetadata_WithBranch(t *testing.T) {
	repo := branchReadMockRepo()
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/fullMetadata", handler.GetFullMetadata)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1/fullMetadata?preview=true&branch=br-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	body := parseJSON(t, w.Body.Bytes())
	objectTypes, ok := body["objectTypes"].([]interface{})
	if !ok {
		t.Fatal("expected objectTypes array")
	}
	// 2 objectTypes: modified employee + added department
	if len(objectTypes) != 2 {
		t.Fatalf("expected 2 objectTypes in fullMetadata, got %d", len(objectTypes))
	}

	// Action types should show modified version
	actionTypes, ok := body["actionTypes"].([]interface{})
	if !ok {
		t.Fatal("expected actionTypes array")
	}
	if len(actionTypes) != 1 {
		t.Fatalf("expected 1 actionType, got %d", len(actionTypes))
	}
}
