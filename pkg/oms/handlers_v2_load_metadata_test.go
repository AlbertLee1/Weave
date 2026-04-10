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

// --- Ontology load_metadata endpoint tests (US-014) ---

func TestLoadMetadata_AllSubsets(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ri.ontology.main.object-type.ot1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Employee"},
		},
		linkTypes: []oms.LinkType{
			{RID: "ri.ontology.main.link-type.lt1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "supervises"},
		},
		actionTypes: []oms.ActionType{
			{RID: "ri.ontology.main.action-type.at1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "promote"},
		},
		interfaces: []oms.Interface{
			{RID: "ri.ontology.main.interface.i1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Addressable"},
		},
		queryTypes: []oms.QueryType{
			{RID: "ri.ontology.main.query-type.qt1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "topEmployees"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/metadata", handler.LoadMetadataV2)

	body := `{"objectTypes":{},"linkTypes":{},"actionTypes":{},"interfaceTypes":{},"queryTypes":{}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/ri.ontology.main.ontology.1/metadata", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())

	// Ontology should always be present
	ontology, ok := resp["ontology"].(map[string]interface{})
	if !ok {
		t.Fatal("expected ontology to be an object")
	}
	if ontology["apiName"] != "northwind" {
		t.Errorf("expected ontology apiName 'northwind', got %v", ontology["apiName"])
	}

	// All subsets should be present
	checkDataArray(t, resp, "objectTypes", 1)
	checkDataArray(t, resp, "linkTypes", 1)
	checkDataArray(t, resp, "actionTypes", 1)
	checkDataArray(t, resp, "interfaceTypes", 1)
	checkDataArray(t, resp, "queryTypes", 1)
}

func TestLoadMetadata_SelectiveSubsets(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ri.ontology.main.object-type.ot1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Employee"},
		},
		actionTypes: []oms.ActionType{
			{RID: "ri.ontology.main.action-type.at1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "promote"},
		},
		linkTypes: []oms.LinkType{
			{RID: "ri.ontology.main.link-type.lt1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "supervises"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/metadata", handler.LoadMetadataV2)

	// Only request objectTypes — other subsets should be absent
	body := `{"objectTypes":{}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/ri.ontology.main.ontology.1/metadata", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())

	// objectTypes should be present
	checkDataArray(t, resp, "objectTypes", 1)

	// Other subsets should NOT be present (not requested)
	if _, ok := resp["linkTypes"]; ok {
		t.Error("linkTypes should not be present when not requested")
	}
	if _, ok := resp["actionTypes"]; ok {
		t.Error("actionTypes should not be present when not requested")
	}
	if _, ok := resp["interfaceTypes"]; ok {
		t.Error("interfaceTypes should not be present when not requested")
	}
	if _, ok := resp["queryTypes"]; ok {
		t.Error("queryTypes should not be present when not requested")
	}
}

func TestLoadMetadata_EmptyBody_ReturnsOnlyOntology(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ri.ontology.main.object-type.ot1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Employee"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/metadata", handler.LoadMetadataV2)

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/ri.ontology.main.ontology.1/metadata", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	if _, ok := resp["ontology"]; !ok {
		t.Fatal("expected ontology to be present even with empty request")
	}
	// No subsets should be present
	for _, key := range []string{"objectTypes", "linkTypes", "actionTypes", "interfaceTypes", "queryTypes"} {
		if _, ok := resp[key]; ok {
			t.Errorf("%s should not be present when not requested", key)
		}
	}
}

func TestLoadMetadata_OntologyNotFound(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/metadata", handler.LoadMetadataV2)

	body := `{"objectTypes":{}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/nonexistent/metadata", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLoadMetadata_InvalidBody(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/metadata", handler.LoadMetadataV2)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/ri.ontology.main.ontology.1/metadata", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLoadMetadata_ByRID(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ri.ontology.main.object-type.ot1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Employee"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/metadata", handler.LoadMetadataV2)

	body := `{"objectTypes":{}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/ri.ontology.main.ontology.1/metadata", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	ontology, ok := resp["ontology"].(map[string]interface{})
	if !ok {
		t.Fatal("expected ontology to be an object")
	}
	if ontology["apiName"] != "northwind" {
		t.Errorf("expected ontology apiName 'northwind', got %v", ontology["apiName"])
	}
	checkDataArray(t, resp, "objectTypes", 1)
}

func TestLoadMetadata_MultipleSubsets(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "northwind"},
		},
		actionTypes: []oms.ActionType{
			{RID: "ri.ontology.main.action-type.at1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "promote"},
			{RID: "ri.ontology.main.action-type.at2", OntologyRID: "ri.ontology.main.ontology.1", APIName: "transfer"},
		},
		queryTypes: []oms.QueryType{
			{RID: "ri.ontology.main.query-type.qt1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "topEmployees"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/metadata", handler.LoadMetadataV2)

	body := `{"actionTypes":{},"queryTypes":{}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/ri.ontology.main.ontology.1/metadata", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	checkDataArray(t, resp, "actionTypes", 2)
	checkDataArray(t, resp, "queryTypes", 1)

	// objectTypes not requested — should be absent
	if _, ok := resp["objectTypes"]; ok {
		t.Error("objectTypes should not be present when not requested")
	}
}

// checkDataArray verifies a response key contains an array with the expected count.
func checkDataArray(t *testing.T, resp map[string]interface{}, key string, expectedCount int) {
	t.Helper()
	raw, ok := resp[key]
	if !ok {
		t.Fatalf("expected %s to be present in response", key)
	}
	arr, ok := raw.([]interface{})
	if !ok {
		t.Fatalf("expected %s to be an array, got %T", key, raw)
	}
	if len(arr) != expectedCount {
		t.Errorf("expected %s to have %d items, got %d", key, expectedCount, len(arr))
	}
}

// Ensure json import is used
var _ = json.Marshal
