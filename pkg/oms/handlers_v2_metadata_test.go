package oms_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// --- InterfaceType V2 Endpoint Tests ---

func TestListInterfaceTypesV2_WithPreview(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
		interfaces: []oms.Interface{
			{RID: "ri.ontology.main.interface.1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Addressable", DisplayName: "Addressable"},
			{RID: "ri.ontology.main.interface.2", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Auditable", DisplayName: "Auditable"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/interfaceTypes", handler.ListInterfaceTypesV2)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1/interfaceTypes?preview=true", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	body := parseJSON(t, w.Body.Bytes())
	data, ok := body["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}
	if len(data) != 2 {
		t.Errorf("expected 2 interface types, got %d", len(data))
	}
}

func TestListInterfaceTypesV2_WithoutPreview_Returns400(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/interfaceTypes", handler.ListInterfaceTypesV2)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/interfaceTypes", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", w.Code, w.Body.String())
	}

	body := parseJSON(t, w.Body.Bytes())
	if body["errorName"] != "PreviewRequired" {
		t.Errorf("expected errorName 'PreviewRequired', got %v", body["errorName"])
	}
}

func TestListInterfaceTypesV2_Empty(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/interfaceTypes", handler.ListInterfaceTypesV2)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/interfaceTypes?preview=true", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	body := parseJSON(t, w.Body.Bytes())
	data, ok := body["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}
	if len(data) != 0 {
		t.Errorf("expected empty array, got %d items", len(data))
	}
}

func TestGetInterfaceTypeV2_Found(t *testing.T) {
	repo := &mockRepo{
		interfaces: []oms.Interface{
			{RID: "ri.ontology.main.interface.1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Addressable", DisplayName: "Addressable"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/interfaceTypes/{interfaceType}", handler.GetInterfaceTypeV2)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1/interfaceTypes/Addressable", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	body := parseJSON(t, w.Body.Bytes())
	if body["apiName"] != "Addressable" {
		t.Errorf("expected apiName 'Addressable', got %v", body["apiName"])
	}
}

func TestGetInterfaceTypeV2_NotFound(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/interfaceTypes/{interfaceType}", handler.GetInterfaceTypeV2)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/interfaceTypes/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetInterfaceTypeV2_ByRID(t *testing.T) {
	repo := &mockRepo{
		interfaces: []oms.Interface{
			{RID: "ri.ontology.main.interface.1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Addressable", DisplayName: "Addressable"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/interfaceTypes/{interfaceType}", handler.GetInterfaceTypeV2)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1/interfaceTypes/ri.ontology.main.interface.1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	body := parseJSON(t, w.Body.Bytes())
	if body["apiName"] != "Addressable" {
		t.Errorf("expected apiName 'Addressable', got %v", body["apiName"])
	}
}

// --- ValueType V2 Endpoint Tests ---

func TestListValueTypesV2_WithPreview(t *testing.T) {
	repo := &mockRepo{
		valueTypes: []oms.ValueType{
			{RID: "ri.ontology.main.valuetype.1", APIName: "Currency", DisplayName: "Currency", BaseType: "double"},
			{RID: "ri.ontology.main.valuetype.2", APIName: "Email", DisplayName: "Email", BaseType: "string"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/valueTypes", handler.ListValueTypesV2)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/valueTypes?preview=true", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	body := parseJSON(t, w.Body.Bytes())
	data, ok := body["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}
	if len(data) != 2 {
		t.Errorf("expected 2 value types, got %d", len(data))
	}

	first := data[0].(map[string]interface{})
	if first["apiName"] != "Currency" {
		t.Errorf("expected first value type apiName 'Currency', got %v", first["apiName"])
	}
}

func TestListValueTypesV2_WithoutPreview_Returns400(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/valueTypes", handler.ListValueTypesV2)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/valueTypes", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", w.Code, w.Body.String())
	}

	body := parseJSON(t, w.Body.Bytes())
	if body["errorName"] != "PreviewRequired" {
		t.Errorf("expected errorName 'PreviewRequired', got %v", body["errorName"])
	}
}

func TestListValueTypesV2_Empty(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/valueTypes", handler.ListValueTypesV2)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/valueTypes?preview=true", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	body := parseJSON(t, w.Body.Bytes())
	data, ok := body["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}
	if len(data) != 0 {
		t.Errorf("expected empty array, got %d items", len(data))
	}
}

func TestGetValueTypeV2_Found(t *testing.T) {
	repo := &mockRepo{
		valueTypes: []oms.ValueType{
			{RID: "ri.ontology.main.valuetype.1", APIName: "Currency", DisplayName: "Currency", BaseType: "double", Version: 1, Constraints: json.RawMessage(`{}`)},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/valueTypes/{valueType}", handler.GetValueTypeV2)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/valueTypes/Currency", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	body := parseJSON(t, w.Body.Bytes())
	if body["apiName"] != "Currency" {
		t.Errorf("expected apiName 'Currency', got %v", body["apiName"])
	}
	if body["baseType"] != "double" {
		t.Errorf("expected baseType 'double', got %v", body["baseType"])
	}
}

func TestGetValueTypeV2_ByRID(t *testing.T) {
	repo := &mockRepo{
		valueTypes: []oms.ValueType{
			{RID: "ri.ontology.main.valuetype.1", APIName: "Currency", DisplayName: "Currency", BaseType: "double", Version: 1},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/valueTypes/{valueType}", handler.GetValueTypeV2)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/valueTypes/ri.ontology.main.valuetype.1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	body := parseJSON(t, w.Body.Bytes())
	if body["apiName"] != "Currency" {
		t.Errorf("expected apiName 'Currency', got %v", body["apiName"])
	}
}

func TestGetValueTypeV2_NotFound(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/valueTypes/{valueType}", handler.GetValueTypeV2)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/valueTypes/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d: %s", w.Code, w.Body.String())
	}
}

// --- QueryType V2 Endpoint Tests ---

func TestListQueryTypesV2_WithData(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
		queryTypes: []oms.QueryType{
			{RID: "ri.ontology.main.querytype.1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "topCustomers", DisplayName: "Top Customers", Status: "ACTIVE"},
			{RID: "ri.ontology.main.querytype.2", OntologyRID: "ri.ontology.main.ontology.1", APIName: "recentOrders", DisplayName: "Recent Orders", Status: "ACTIVE"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/queryTypes", handler.ListQueryTypesV2)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/queryTypes", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	body := parseJSON(t, w.Body.Bytes())
	data, ok := body["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}
	if len(data) != 2 {
		t.Errorf("expected 2 query types, got %d", len(data))
	}
}

func TestListQueryTypesV2_Empty(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/queryTypes", handler.ListQueryTypesV2)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/queryTypes", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	body := parseJSON(t, w.Body.Bytes())
	data, ok := body["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}
	if len(data) != 0 {
		t.Errorf("expected empty array, got %d items", len(data))
	}
}

func TestListQueryTypesV2_ByOntologyApiName(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
		queryTypes: []oms.QueryType{
			{RID: "ri.ontology.main.querytype.1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "topCustomers", DisplayName: "Top Customers", Status: "ACTIVE"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/queryTypes", handler.ListQueryTypesV2)

	// Use ontology apiName (not RID) in path
	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/queryTypes", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	body := parseJSON(t, w.Body.Bytes())
	data, ok := body["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}
	if len(data) != 1 {
		t.Errorf("expected 1 query type, got %d", len(data))
	}
}

func TestGetQueryTypeV2_Found(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
		queryTypes: []oms.QueryType{
			{RID: "ri.ontology.main.querytype.1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "topCustomers", DisplayName: "Top Customers", Status: "ACTIVE"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/queryTypes/{queryApiName}", handler.GetQueryTypeV2)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/queryTypes/topCustomers", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	body := parseJSON(t, w.Body.Bytes())
	if body["apiName"] != "topCustomers" {
		t.Errorf("expected apiName 'topCustomers', got %v", body["apiName"])
	}
}

func TestGetQueryTypeV2_ByRID(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
		queryTypes: []oms.QueryType{
			{RID: "ri.ontology.main.querytype.1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "topCustomers", DisplayName: "Top Customers", Status: "ACTIVE"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/queryTypes/{queryApiName}", handler.GetQueryTypeV2)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1/queryTypes/ri.ontology.main.querytype.1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	body := parseJSON(t, w.Body.Bytes())
	if body["apiName"] != "topCustomers" {
		t.Errorf("expected apiName 'topCustomers', got %v", body["apiName"])
	}
}

func TestGetQueryTypeV2_NotFound(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/queryTypes/{queryApiName}", handler.GetQueryTypeV2)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/queryTypes/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestExecuteQueryType_UsesOntologyApiNameParam(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
		queryTypes: []oms.QueryType{
			{RID: "ri.ontology.main.querytype.1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "topCustomers", DisplayName: "Top Customers", Status: "ACTIVE", Parameters: json.RawMessage(`[]`), Output: json.RawMessage(`{}`), Query: json.RawMessage(`{"objectType":"Customer"}`)},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	// Route uses {ontologyApiName} for consistency with other V2 routes
	r.Post("/api/v2/ontologies/{ontologyApiName}/queries/{queryApiName}/execute", handler.ExecuteQueryType)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/northwind/queries/topCustomers/execute", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	body := parseJSON(t, w.Body.Bytes())
	if body["apiName"] != "topCustomers" {
		t.Errorf("expected apiName 'topCustomers', got %v", body["apiName"])
	}
}
