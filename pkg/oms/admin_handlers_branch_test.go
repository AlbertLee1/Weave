package oms_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// --- Branch overlay write tests (US-114) ---

func TestCreateObjectType_BranchOverlay(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/add-types", Status: "open", BaseVersion: 1},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/admin/ontologies/{ontologyApiName}/objectTypes", handler.CreateObjectType)

	body := `{"apiName":"employee","displayName":"Employee","primaryKey":"employeeId","status":"ACTIVE","visibility":"NORMAL"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/test/objectTypes?branch=br-1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Verify branch change was recorded
	if len(repo.branchChanges) != 1 {
		t.Fatalf("expected 1 branch change, got %d", len(repo.branchChanges))
	}

	bc := repo.branchChanges[0]
	if bc.BranchID != "br-1" {
		t.Errorf("expected branchID=br-1, got %s", bc.BranchID)
	}
	if bc.ChangeType != "ADDED" {
		t.Errorf("expected changeType=ADDED, got %s", bc.ChangeType)
	}
	if bc.EntityType != "objectType" {
		t.Errorf("expected entityType=objectType, got %s", bc.EntityType)
	}
	if bc.BeforeState != nil {
		t.Errorf("expected beforeState=nil for ADDED, got %s", string(bc.BeforeState))
	}
	if bc.AfterState == nil {
		t.Fatal("expected afterState to be set for ADDED")
	}

	// afterState should be a valid ObjectType JSON
	var afterOt oms.ObjectType
	if err := json.Unmarshal(bc.AfterState, &afterOt); err != nil {
		t.Fatalf("afterState is not valid ObjectType JSON: %v", err)
	}
	if afterOt.APIName != "employee" {
		t.Errorf("expected apiName=employee in afterState, got %s", afterOt.APIName)
	}

	// Object should NOT be in main objectTypes
	if len(repo.objectTypes) != 0 {
		t.Errorf("expected 0 main objectTypes, got %d", len(repo.objectTypes))
	}
}

func TestUpdateObjectType_BranchOverlay(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test"},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ot-1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "employee", DisplayName: "Employee", PrimaryKey: "employeeId", Status: "ACTIVE", Visibility: "NORMAL"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/update", Status: "open", BaseVersion: 1},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Put("/api/admin/objectTypes/{objectTypeRid}", handler.UpdateObjectType)

	body := `{"displayName":"Updated Employee","status":"ACTIVE","visibility":"NORMAL"}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/objectTypes/ot-1?branch=br-1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify branch change recorded
	if len(repo.branchChanges) != 1 {
		t.Fatalf("expected 1 branch change, got %d", len(repo.branchChanges))
	}

	bc := repo.branchChanges[0]
	if bc.ChangeType != "MODIFIED" {
		t.Errorf("expected changeType=MODIFIED, got %s", bc.ChangeType)
	}
	if bc.EntityType != "objectType" {
		t.Errorf("expected entityType=objectType, got %s", bc.EntityType)
	}
	if bc.EntityRID != "ot-1" {
		t.Errorf("expected entityRID=ot-1, got %s", bc.EntityRID)
	}

	// beforeState should contain original
	if bc.BeforeState == nil {
		t.Fatal("expected beforeState for MODIFIED")
	}
	var beforeOt oms.ObjectType
	if err := json.Unmarshal(bc.BeforeState, &beforeOt); err != nil {
		t.Fatalf("beforeState invalid: %v", err)
	}
	if beforeOt.DisplayName != "Employee" {
		t.Errorf("expected original displayName=Employee, got %s", beforeOt.DisplayName)
	}

	// afterState should contain updated
	if bc.AfterState == nil {
		t.Fatal("expected afterState for MODIFIED")
	}
	var afterOt oms.ObjectType
	if err := json.Unmarshal(bc.AfterState, &afterOt); err != nil {
		t.Fatalf("afterState invalid: %v", err)
	}
	if afterOt.DisplayName != "Updated Employee" {
		t.Errorf("expected updated displayName=Updated Employee, got %s", afterOt.DisplayName)
	}

	// Main objectTypes should NOT be modified
	if repo.objectTypes[0].DisplayName != "Employee" {
		t.Errorf("expected main objectType unchanged, got displayName=%s", repo.objectTypes[0].DisplayName)
	}
}

func TestDeleteObjectType_BranchOverlay(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test"},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ot-1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "employee", DisplayName: "Employee", PrimaryKey: "employeeId", Status: "DEPRECATED", Visibility: "NORMAL"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/delete", Status: "open", BaseVersion: 1},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Delete("/api/admin/objectTypes/{objectTypeRid}", handler.DeleteObjectType)

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/objectTypes/ot-1?branch=br-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Branch change recorded
	if len(repo.branchChanges) != 1 {
		t.Fatalf("expected 1 branch change, got %d", len(repo.branchChanges))
	}

	bc := repo.branchChanges[0]
	if bc.ChangeType != "DELETED" {
		t.Errorf("expected changeType=DELETED, got %s", bc.ChangeType)
	}
	if bc.EntityType != "objectType" {
		t.Errorf("expected entityType=objectType, got %s", bc.EntityType)
	}
	if bc.BeforeState == nil {
		t.Fatal("expected beforeState for DELETED")
	}
	if bc.AfterState != nil {
		t.Error("expected afterState=nil for DELETED")
	}

	// Main objectTypes should NOT be modified (object still exists)
	if len(repo.objectTypes) != 1 {
		t.Errorf("expected main objectType still exists, got %d", len(repo.objectTypes))
	}
}

func TestCreateProperty_BranchOverlay(t *testing.T) {
	repo := &mockRepo{
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/props", Status: "open", BaseVersion: 1},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/admin/objectTypes/{objectTypeRid}/properties", handler.CreateProperty)

	body := `{"apiName":"firstName","baseType":"String","displayName":"First Name"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/objectTypes/ot-1/properties?branch=br-1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	if len(repo.branchChanges) != 1 {
		t.Fatalf("expected 1 branch change, got %d", len(repo.branchChanges))
	}
	bc := repo.branchChanges[0]
	if bc.ChangeType != "ADDED" {
		t.Errorf("expected ADDED, got %s", bc.ChangeType)
	}
	if bc.EntityType != "property" {
		t.Errorf("expected entityType=property, got %s", bc.EntityType)
	}
	if len(repo.properties) != 0 {
		t.Error("expected 0 main properties")
	}
}

func TestCreateLinkType_BranchOverlay(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/links", Status: "open", BaseVersion: 1},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/admin/ontologies/{ontologyApiName}/linkTypes", handler.CreateLinkType)

	body := `{"apiName":"employeeDept","displayName":"Employee Dept","objectTypeApiName":"Employee","linkedObjectTypeApiName":"Department","cardinality":"MANY_TO_ONE"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/test/linkTypes?branch=br-1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	if len(repo.branchChanges) != 1 {
		t.Fatalf("expected 1 branch change, got %d", len(repo.branchChanges))
	}
	bc := repo.branchChanges[0]
	if bc.ChangeType != "ADDED" {
		t.Errorf("expected ADDED, got %s", bc.ChangeType)
	}
	if bc.EntityType != "linkType" {
		t.Errorf("expected entityType=linkType, got %s", bc.EntityType)
	}
	if len(repo.linkTypes) != 0 {
		t.Error("expected 0 main linkTypes")
	}
}

func TestCreateActionType_BranchOverlay(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/actions", Status: "open", BaseVersion: 1},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/admin/ontologies/{ontologyApiName}/actionTypes", handler.CreateActionType)

	body := `{"apiName":"createEmp","displayName":"Create Employee","status":"ACTIVE","parameters":[],"rules":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/test/actionTypes?branch=br-1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	if len(repo.branchChanges) != 1 {
		t.Fatalf("expected 1 branch change, got %d", len(repo.branchChanges))
	}
	bc := repo.branchChanges[0]
	if bc.ChangeType != "ADDED" {
		t.Errorf("expected ADDED, got %s", bc.ChangeType)
	}
	if bc.EntityType != "actionType" {
		t.Errorf("expected entityType=actionType, got %s", bc.EntityType)
	}
	if len(repo.actionTypes) != 0 {
		t.Error("expected 0 main actionTypes")
	}
}

func TestBranchOverlay_BranchNotFound(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/admin/ontologies/{ontologyApiName}/objectTypes", handler.CreateObjectType)

	body := `{"apiName":"employee","displayName":"Employee","primaryKey":"employeeId"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/test/objectTypes?branch=nonexistent", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for nonexistent branch, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBranchOverlay_ClosedBranch(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-closed", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/closed", Status: "closed", BaseVersion: 1},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/admin/ontologies/{ontologyApiName}/objectTypes", handler.CreateObjectType)

	body := `{"apiName":"employee","displayName":"Employee","primaryKey":"employeeId"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/test/objectTypes?branch=br-closed", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 for closed branch, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateProperty_BranchOverlay(t *testing.T) {
	repo := &mockRepo{
		properties: []oms.Property{
			{RID: "prop-1", ObjectTypeRID: "ot-1", APIName: "firstName", DisplayName: "First Name", BaseType: "String"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/update-prop", Status: "open", BaseVersion: 1},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Put("/api/admin/properties/{propertyRid}", handler.UpdateProperty)

	body := `{"displayName":"Updated First Name"}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/properties/prop-1?branch=br-1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if len(repo.branchChanges) != 1 {
		t.Fatalf("expected 1 branch change, got %d", len(repo.branchChanges))
	}
	bc := repo.branchChanges[0]
	if bc.ChangeType != "MODIFIED" {
		t.Errorf("expected MODIFIED, got %s", bc.ChangeType)
	}
	if bc.EntityType != "property" {
		t.Errorf("expected entityType=property, got %s", bc.EntityType)
	}

	// Main property should be unchanged
	if repo.properties[0].DisplayName != "First Name" {
		t.Errorf("expected main property unchanged, got displayName=%s", repo.properties[0].DisplayName)
	}
}

func TestDeleteProperty_BranchOverlay(t *testing.T) {
	repo := &mockRepo{
		properties: []oms.Property{
			{RID: "prop-1", ObjectTypeRID: "ot-1", APIName: "firstName", DisplayName: "First Name", BaseType: "String"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/del-prop", Status: "open", BaseVersion: 1},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Delete("/api/admin/properties/{propertyRid}", handler.DeleteProperty)

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/properties/prop-1?branch=br-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	if len(repo.branchChanges) != 1 {
		t.Fatalf("expected 1 branch change, got %d", len(repo.branchChanges))
	}
	bc := repo.branchChanges[0]
	if bc.ChangeType != "DELETED" {
		t.Errorf("expected DELETED, got %s", bc.ChangeType)
	}
	if bc.BeforeState == nil {
		t.Error("expected beforeState for DELETED property")
	}
	// Main property should still exist
	if len(repo.properties) != 1 {
		t.Errorf("expected main property still exists, got %d", len(repo.properties))
	}
}

func TestUpdateLinkType_BranchOverlay(t *testing.T) {
	repo := &mockRepo{
		linkTypes: []oms.LinkType{
			{RID: "lt-1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "empDept", DisplayName: "Emp Dept", Cardinality: "MANY_TO_ONE"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/update-link", Status: "open", BaseVersion: 1},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Put("/api/admin/linkTypes/{linkTypeRid}", handler.UpdateLinkType)

	body := `{"displayName":"Updated Emp Dept"}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/linkTypes/lt-1?branch=br-1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if len(repo.branchChanges) != 1 {
		t.Fatalf("expected 1 branch change, got %d", len(repo.branchChanges))
	}
	bc := repo.branchChanges[0]
	if bc.ChangeType != "MODIFIED" {
		t.Errorf("expected MODIFIED, got %s", bc.ChangeType)
	}
	if bc.EntityType != "linkType" {
		t.Errorf("expected entityType=linkType, got %s", bc.EntityType)
	}
	// Main should be unchanged
	if repo.linkTypes[0].DisplayName != "Emp Dept" {
		t.Error("expected main linkType unchanged")
	}
}

func TestDeleteLinkType_BranchOverlay(t *testing.T) {
	repo := &mockRepo{
		linkTypes: []oms.LinkType{
			{RID: "lt-1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "empDept", DisplayName: "Emp Dept"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/del-link", Status: "open", BaseVersion: 1},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Delete("/api/admin/linkTypes/{linkTypeRid}", handler.DeleteLinkType)

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/linkTypes/lt-1?branch=br-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	if len(repo.branchChanges) != 1 {
		t.Fatalf("expected 1 branch change, got %d", len(repo.branchChanges))
	}
	bc := repo.branchChanges[0]
	if bc.ChangeType != "DELETED" {
		t.Errorf("expected DELETED, got %s", bc.ChangeType)
	}
	if bc.EntityType != "linkType" {
		t.Errorf("expected entityType=linkType, got %s", bc.EntityType)
	}
	// Main still exists
	if len(repo.linkTypes) != 1 {
		t.Error("expected main linkType still exists")
	}
}

func TestUpdateActionType_BranchOverlay(t *testing.T) {
	repo := &mockRepo{
		actionTypes: []oms.ActionType{
			{RID: "at-1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "createEmp", DisplayName: "Create Emp", Status: "ACTIVE"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/update-action", Status: "open", BaseVersion: 1},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Put("/api/admin/actionTypes/{actionTypeRid}", handler.UpdateActionType)

	body := `{"displayName":"Updated Create Emp","status":"ACTIVE","parameters":[],"rules":[]}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/actionTypes/at-1?branch=br-1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if len(repo.branchChanges) != 1 {
		t.Fatalf("expected 1 branch change, got %d", len(repo.branchChanges))
	}
	bc := repo.branchChanges[0]
	if bc.ChangeType != "MODIFIED" {
		t.Errorf("expected MODIFIED, got %s", bc.ChangeType)
	}
	if bc.EntityType != "actionType" {
		t.Errorf("expected entityType=actionType, got %s", bc.EntityType)
	}
	// Main unchanged
	if repo.actionTypes[0].DisplayName != "Create Emp" {
		t.Error("expected main actionType unchanged")
	}
}

func TestDeleteActionType_BranchOverlay(t *testing.T) {
	repo := &mockRepo{
		actionTypes: []oms.ActionType{
			{RID: "at-1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "createEmp", DisplayName: "Create Emp", Status: "ACTIVE"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/del-action", Status: "open", BaseVersion: 1},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Delete("/api/admin/actionTypes/{actionTypeRid}", handler.DeleteActionType)

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/actionTypes/at-1?branch=br-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	if len(repo.branchChanges) != 1 {
		t.Fatalf("expected 1 branch change, got %d", len(repo.branchChanges))
	}
	bc := repo.branchChanges[0]
	if bc.ChangeType != "DELETED" {
		t.Errorf("expected DELETED, got %s", bc.ChangeType)
	}
	if bc.EntityType != "actionType" {
		t.Errorf("expected entityType=actionType, got %s", bc.EntityType)
	}
	// Main still exists
	if len(repo.actionTypes) != 1 {
		t.Error("expected main actionType still exists")
	}
}

func TestBranchOverlay_WithoutBranch_NormalFlow(t *testing.T) {
	// When no ?branch= param, should work as normal (existing behavior preserved)
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/admin/ontologies/{ontologyApiName}/objectTypes", handler.CreateObjectType)

	body := `{"apiName":"employee","displayName":"Employee","primaryKey":"employeeId","status":"ACTIVE","visibility":"NORMAL"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/test/objectTypes", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Should go to main
	if len(repo.objectTypes) != 1 {
		t.Errorf("expected 1 main objectType, got %d", len(repo.objectTypes))
	}
	// No branch changes
	if len(repo.branchChanges) != 0 {
		t.Errorf("expected 0 branch changes, got %d", len(repo.branchChanges))
	}
}
