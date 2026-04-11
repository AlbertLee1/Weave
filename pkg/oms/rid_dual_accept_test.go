package oms_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// ---------------------------------------------------------------------------
// US-009: RID / apiName dual accept for path parameters
//
// All {ontology}, {objectType}, {actionType}, {queryApiName} handlers must
// accept either the apiName string or the full ri.xxx.xxx.xxx.xxx RID.
// ---------------------------------------------------------------------------

func TestGetOntology_ByApiName(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.abc", APIName: "northwind", DisplayName: "Northwind"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}", handler.GetOntology)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := parseJSON(t, w.Body.Bytes())
	if body["apiName"] != "northwind" {
		t.Errorf("expected apiName 'northwind', got %v", body["apiName"])
	}
}

func TestGetOntology_ByRID(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.abc", APIName: "northwind", DisplayName: "Northwind"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}", handler.GetOntology)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.abc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := parseJSON(t, w.Body.Bytes())
	if body["apiName"] != "northwind" {
		t.Errorf("expected apiName 'northwind', got %v", body["apiName"])
	}
}

func TestGetObjectType_ByObjectTypeRID(t *testing.T) {
	repo := &mockRepo{
		objectTypes: []oms.ObjectType{
			{
				RID: "ri.ontology.main.object-type.emp1", OntologyRID: "ri.ontology.main.ontology.abc",
				APIName: "employee", DisplayName: "Employee", PrimaryKey: "employeeId",
				Status: "ACTIVE", Visibility: "NORMAL",
			},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}", handler.GetObjectType)

	// Pass RID as the objectType path param instead of apiName
	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/ri.ontology.main.ontology.abc/objectTypes/ri.ontology.main.object-type.emp1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := parseJSON(t, w.Body.Bytes())
	if body["apiName"] != "employee" {
		t.Errorf("expected apiName 'employee', got %v", body["apiName"])
	}
}

func TestGetObjectType_ByOntologyApiName(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.abc", APIName: "northwind", DisplayName: "Northwind"},
		},
		objectTypes: []oms.ObjectType{
			{
				RID: "ri.ontology.main.object-type.emp1", OntologyRID: "ri.ontology.main.ontology.abc",
				APIName: "employee", DisplayName: "Employee", PrimaryKey: "employeeId",
				Status: "ACTIVE", Visibility: "NORMAL",
			},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}", handler.GetObjectType)

	// Pass ontology apiName (not RID) and objectType apiName
	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/northwind/objectTypes/employee", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := parseJSON(t, w.Body.Bytes())
	if body["apiName"] != "employee" {
		t.Errorf("expected apiName 'employee', got %v", body["apiName"])
	}
}

func TestGetActionType_ByApiName(t *testing.T) {
	repo := &mockRepo{
		actionTypes: []oms.ActionType{
			{
				RID: "ri.ontology.main.action-type.at1", OntologyRID: "ri.ontology.main.ontology.abc",
				APIName: "createEmployee", DisplayName: "Create Employee",
				Status: "ACTIVE", Parameters: json.RawMessage(`[]`),
			},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypes/{actionTypeRid}", handler.GetActionType)

	// Pass apiName as the actionType path param instead of RID
	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/ri.ontology.main.ontology.abc/actionTypes/createEmployee", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := parseJSON(t, w.Body.Bytes())
	if body["apiName"] != "createEmployee" {
		t.Errorf("expected apiName 'createEmployee', got %v", body["apiName"])
	}
}

func TestGetActionType_ByRID_US009(t *testing.T) {
	repo := &mockRepo{
		actionTypes: []oms.ActionType{
			{
				RID: "ri.ontology.main.action-type.at1", OntologyRID: "ri.ontology.main.ontology.abc",
				APIName: "createEmployee", DisplayName: "Create Employee",
				Status: "ACTIVE", Parameters: json.RawMessage(`[]`),
			},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypes/{actionTypeRid}", handler.GetActionType)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/ri.ontology.main.ontology.abc/actionTypes/ri.ontology.main.action-type.at1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := parseJSON(t, w.Body.Bytes())
	if body["apiName"] != "createEmployee" {
		t.Errorf("expected apiName 'createEmployee', got %v", body["apiName"])
	}
}
