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

// --- ObjectType fullMetadata endpoint tests ---

func TestGetObjectTypeFullMetadata_Found(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
		objectTypes: []oms.ObjectType{
			{
				RID: "ri.ontology.main.object-type.ot1", OntologyRID: "ri.ontology.main.ontology.1",
				APIName: "Employee", DisplayName: "Employee", PluralDisplayName: "Employees",
				Description: "An employee record", PrimaryKey: "employeeId",
				TitleProperty: "fullName", Status: "ACTIVE", Visibility: "NORMAL",
			},
		},
		properties: []oms.Property{
			{RID: "ri.ontology.main.property.p1", ObjectTypeRID: "ri.ontology.main.object-type.ot1", APIName: "employeeId", BaseType: "integer"},
			{RID: "ri.ontology.main.property.p2", ObjectTypeRID: "ri.ontology.main.object-type.ot1", APIName: "fullName", BaseType: "string"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/fullMetadata", handler.GetObjectTypeFullMetadataV2)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/objectTypes/Employee/fullMetadata?preview=true", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	body := parseJSON(t, w.Body.Bytes())
	if body["apiName"] != "Employee" {
		t.Errorf("expected apiName 'Employee', got %v", body["apiName"])
	}
	if body["description"] != "An employee record" {
		t.Errorf("expected description 'An employee record', got %v", body["description"])
	}
	if body["titleProperty"] != "fullName" {
		t.Errorf("expected titleProperty 'fullName', got %v", body["titleProperty"])
	}
	// fullMetadata should include properties
	props, ok := body["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties to be an object (V2 record format)")
	}
	if _, hasEmpId := props["employeeId"]; !hasEmpId {
		t.Error("expected properties to have key 'employeeId'")
	}
	if _, hasName := props["fullName"]; !hasName {
		t.Error("expected properties to have key 'fullName'")
	}
}

func TestGetObjectTypeFullMetadata_NotFound(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/fullMetadata", handler.GetObjectTypeFullMetadataV2)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/objectTypes/Nonexistent/fullMetadata?preview=true", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetObjectTypeFullMetadata_ByRID(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
		objectTypes: []oms.ObjectType{
			{
				RID: "ri.ontology.main.object-type.ot1", OntologyRID: "ri.ontology.main.ontology.1",
				APIName: "Employee", DisplayName: "Employee",
				PrimaryKey: "employeeId", Status: "ACTIVE", Visibility: "NORMAL",
			},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/fullMetadata", handler.GetObjectTypeFullMetadataV2)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/objectTypes/ri.ontology.main.object-type.ot1/fullMetadata?preview=true", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	body := parseJSON(t, w.Body.Bytes())
	if body["apiName"] != "Employee" {
		t.Errorf("expected apiName 'Employee', got %v", body["apiName"])
	}
}

// --- ObjectType getByRidBatch endpoint tests ---

func TestGetObjectTypesByRidBatch_MultipleFound(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
		objectTypes: []oms.ObjectType{
			{
				RID: "ri.ontology.main.object-type.ot1", OntologyRID: "ri.ontology.main.ontology.1",
				APIName: "Employee", DisplayName: "Employee",
				PrimaryKey: "employeeId", Status: "ACTIVE", Visibility: "NORMAL",
			},
			{
				RID: "ri.ontology.main.object-type.ot2", OntologyRID: "ri.ontology.main.ontology.1",
				APIName: "Order", DisplayName: "Order",
				PrimaryKey: "orderId", Status: "ACTIVE", Visibility: "NORMAL",
			},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectTypes/getByRidBatch", handler.GetObjectTypesByRidBatchV2)

	body := `{"rids":["ri.ontology.main.object-type.ot1","ri.ontology.main.object-type.ot2"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/northwind/objectTypes/getByRidBatch", strings.NewReader(body))
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
		t.Fatalf("expected 2 object types, got %d", len(data))
	}
}

func TestGetObjectTypesByRidBatch_EmptyRids(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectTypes/getByRidBatch", handler.GetObjectTypesByRidBatchV2)

	body := `{"rids":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/northwind/objectTypes/getByRidBatch", strings.NewReader(body))
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

func TestGetObjectTypesByRidBatch_SomeMissing(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
		objectTypes: []oms.ObjectType{
			{
				RID: "ri.ontology.main.object-type.ot1", OntologyRID: "ri.ontology.main.ontology.1",
				APIName: "Employee", DisplayName: "Employee",
				PrimaryKey: "employeeId", Status: "ACTIVE", Visibility: "NORMAL",
			},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectTypes/getByRidBatch", handler.GetObjectTypesByRidBatchV2)

	body := `{"rids":["ri.ontology.main.object-type.ot1","ri.ontology.main.object-type.nonexistent"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/northwind/objectTypes/getByRidBatch", strings.NewReader(body))
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
	if len(data) != 1 {
		t.Fatalf("expected 1 object type (missing ones skipped), got %d", len(data))
	}
	ot := data[0].(map[string]interface{})
	if ot["apiName"] != "Employee" {
		t.Errorf("expected apiName 'Employee', got %v", ot["apiName"])
	}
}

// --- ObjectType editsHistory endpoint tests ---

func TestPostObjectTypeEditsHistory_Found(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
		objectTypes: []oms.ObjectType{
			{
				RID: "ri.ontology.main.object-type.ot1", OntologyRID: "ri.ontology.main.ontology.1",
				APIName: "Employee", DisplayName: "Employee",
				PrimaryKey: "employeeId", Status: "ACTIVE", Visibility: "NORMAL",
			},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/editsHistory", handler.PostObjectTypeEditsHistoryV2)

	reqBody := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/northwind/objectTypes/Employee/editsHistory", strings.NewReader(reqBody))
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
	// Empty because the mock returns no action logs
	_ = data
}

func TestPostObjectTypeEditsHistory_ObjectTypeNotFound(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/editsHistory", handler.PostObjectTypeEditsHistoryV2)

	reqBody := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/northwind/objectTypes/Nonexistent/editsHistory", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPostObjectTypeEditsHistory_WithData(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
		objectTypes: []oms.ObjectType{
			{
				RID: "ri.ontology.main.object-type.ot1", OntologyRID: "ri.ontology.main.ontology.1",
				APIName: "Employee", DisplayName: "Employee",
				PrimaryKey: "employeeId", Status: "ACTIVE", Visibility: "NORMAL",
			},
		},
	}
	// Pre-populate action logs for this object type
	repo.actionLogs = []oms.ActionLog{
		{ID: 1, ActionTypeRID: "ri.ontology.main.action-type.at1", Status: "SUCCESS", Parameters: json.RawMessage(`{"name":"Alice"}`), Edits: json.RawMessage(`[]`)},
		{ID: 2, ActionTypeRID: "ri.ontology.main.action-type.at2", Status: "SUCCESS", Parameters: json.RawMessage(`{"name":"Bob"}`), Edits: json.RawMessage(`[]`)},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/editsHistory", handler.PostObjectTypeEditsHistoryV2)

	reqBody := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/northwind/objectTypes/Employee/editsHistory", strings.NewReader(reqBody))
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
		t.Fatalf("expected 2 edits history entries, got %d", len(data))
	}
}
