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

// --- ActionType byRid endpoint tests ---

func TestGetActionTypeByRid_Found(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
		actionTypes: []oms.ActionType{
			{
				RID: "ri.ontology.main.action-type.at1", OntologyRID: "ri.ontology.main.ontology.1",
				APIName: "createEmployee", DisplayName: "Create Employee",
				Status: "ACTIVE", Parameters: json.RawMessage(`[{"id":"name","type":"string","required":true}]`),
			},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypes/byRid/{actionTypeRid}", handler.GetActionTypeByRidV2)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/actionTypes/byRid/ri.ontology.main.action-type.at1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	body := parseJSON(t, w.Body.Bytes())
	if body["apiName"] != "createEmployee" {
		t.Errorf("expected apiName 'createEmployee', got %v", body["apiName"])
	}
	if body["rid"] != "ri.ontology.main.action-type.at1" {
		t.Errorf("expected rid 'ri.ontology.main.action-type.at1', got %v", body["rid"])
	}
}

func TestGetActionTypeByRid_NotFound(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypes/byRid/{actionTypeRid}", handler.GetActionTypeByRidV2)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/actionTypes/byRid/ri.ontology.main.action-type.nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d: %s", w.Code, w.Body.String())
	}
}

// --- ActionType getByRidBatch endpoint tests ---

func TestGetActionTypesByRidBatch_MultipleFound(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
		actionTypes: []oms.ActionType{
			{
				RID: "ri.ontology.main.action-type.at1", OntologyRID: "ri.ontology.main.ontology.1",
				APIName: "createEmployee", DisplayName: "Create Employee",
				Status: "ACTIVE", Parameters: json.RawMessage(`[]`),
			},
			{
				RID: "ri.ontology.main.action-type.at2", OntologyRID: "ri.ontology.main.ontology.1",
				APIName: "deleteEmployee", DisplayName: "Delete Employee",
				Status: "ACTIVE", Parameters: json.RawMessage(`[]`),
			},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/actionTypes/getByRidBatch", handler.GetActionTypesByRidBatchV2)

	body := `{"rids":["ri.ontology.main.action-type.at1","ri.ontology.main.action-type.at2"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/northwind/actionTypes/getByRidBatch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	result := parseJSON(t, w.Body.Bytes())
	data, ok := result["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}
	if len(data) != 2 {
		t.Fatalf("expected 2 action types, got %d", len(data))
	}
}

func TestGetActionTypesByRidBatch_EmptyRids(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/actionTypes/getByRidBatch", handler.GetActionTypesByRidBatchV2)

	body := `{"rids":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/northwind/actionTypes/getByRidBatch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	result := parseJSON(t, w.Body.Bytes())
	data, ok := result["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}
	if len(data) != 0 {
		t.Errorf("expected empty array, got %d items", len(data))
	}
}

func TestGetActionTypesByRidBatch_SomeMissing(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
		actionTypes: []oms.ActionType{
			{
				RID: "ri.ontology.main.action-type.at1", OntologyRID: "ri.ontology.main.ontology.1",
				APIName: "createEmployee", DisplayName: "Create Employee",
				Status: "ACTIVE", Parameters: json.RawMessage(`[]`),
			},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/actionTypes/getByRidBatch", handler.GetActionTypesByRidBatchV2)

	body := `{"rids":["ri.ontology.main.action-type.at1","ri.ontology.main.action-type.nonexistent"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/northwind/actionTypes/getByRidBatch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	result := parseJSON(t, w.Body.Bytes())
	data, ok := result["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}
	// Only the found one should be returned, missing ones are silently skipped
	if len(data) != 1 {
		t.Fatalf("expected 1 action type (missing ones skipped), got %d", len(data))
	}
	at := data[0].(map[string]interface{})
	if at["apiName"] != "createEmployee" {
		t.Errorf("expected apiName 'createEmployee', got %v", at["apiName"])
	}
}

// --- ActionType fullMetadata endpoint tests ---

func TestGetActionTypeFullMetadata_Found(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
		actionTypes: []oms.ActionType{
			{
				RID: "ri.ontology.main.action-type.at1", OntologyRID: "ri.ontology.main.ontology.1",
				APIName: "createEmployee", DisplayName: "Create Employee",
				Description: "Creates a new employee",
				Status:      "ACTIVE",
				Parameters:  json.RawMessage(`[{"id":"name","type":"string","required":true}]`),
				Rules:       json.RawMessage(`{"type":"notification"}`),
			},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypes/{actionTypeRid}/fullMetadata", handler.GetActionTypeFullMetadataV2)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/actionTypes/createEmployee/fullMetadata", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	body := parseJSON(t, w.Body.Bytes())
	if body["apiName"] != "createEmployee" {
		t.Errorf("expected apiName 'createEmployee', got %v", body["apiName"])
	}
	if body["description"] != "Creates a new employee" {
		t.Errorf("expected description 'Creates a new employee', got %v", body["description"])
	}
	// fullMetadata should include rules
	if body["rules"] == nil {
		t.Error("expected rules to be present in fullMetadata response")
	}
	// parameters should be in V2 format
	params, ok := body["parameters"].(map[string]interface{})
	if !ok {
		t.Fatal("expected parameters to be an object (V2 format)")
	}
	if _, hasName := params["name"]; !hasName {
		t.Error("expected parameters to have key 'name'")
	}
}

func TestGetActionTypeFullMetadata_NotFound(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypes/{actionTypeRid}/fullMetadata", handler.GetActionTypeFullMetadataV2)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/actionTypes/nonexistent/fullMetadata", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetActionTypeFullMetadata_ByRID(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
		actionTypes: []oms.ActionType{
			{
				RID: "ri.ontology.main.action-type.at1", OntologyRID: "ri.ontology.main.ontology.1",
				APIName: "createEmployee", DisplayName: "Create Employee",
				Status: "ACTIVE", Parameters: json.RawMessage(`[]`),
			},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypes/{actionTypeRid}/fullMetadata", handler.GetActionTypeFullMetadataV2)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/actionTypes/ri.ontology.main.action-type.at1/fullMetadata", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	body := parseJSON(t, w.Body.Bytes())
	if body["apiName"] != "createEmployee" {
		t.Errorf("expected apiName 'createEmployee', got %v", body["apiName"])
	}
}

// --- actionTypesFullMetadata (list all with full metadata) tests ---

func TestListActionTypesFullMetadata_WithData(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
		actionTypes: []oms.ActionType{
			{
				RID: "ri.ontology.main.action-type.at1", OntologyRID: "ri.ontology.main.ontology.1",
				APIName: "createEmployee", DisplayName: "Create Employee",
				Status: "ACTIVE", Parameters: json.RawMessage(`[{"id":"name","type":"string","required":true}]`),
				Rules: json.RawMessage(`{"type":"notification"}`),
			},
			{
				RID: "ri.ontology.main.action-type.at2", OntologyRID: "ri.ontology.main.ontology.1",
				APIName: "deleteEmployee", DisplayName: "Delete Employee",
				Status: "ACTIVE", Parameters: json.RawMessage(`[]`),
			},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypesFullMetadata", handler.ListActionTypesFullMetadataV2)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1/actionTypesFullMetadata", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	result := parseJSON(t, w.Body.Bytes())
	data, ok := result["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}
	if len(data) != 2 {
		t.Fatalf("expected 2 action types, got %d", len(data))
	}

	// First should have rules (fullMetadata includes everything)
	first := data[0].(map[string]interface{})
	if first["apiName"] != "createEmployee" {
		t.Errorf("expected first apiName 'createEmployee', got %v", first["apiName"])
	}
	if first["rules"] == nil {
		t.Error("expected rules to be present in fullMetadata response")
	}
}

func TestListActionTypesFullMetadata_Empty(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypesFullMetadata", handler.ListActionTypesFullMetadataV2)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/actionTypesFullMetadata", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	result := parseJSON(t, w.Body.Bytes())
	data, ok := result["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}
	if len(data) != 0 {
		t.Errorf("expected empty array, got %d items", len(data))
	}
}
