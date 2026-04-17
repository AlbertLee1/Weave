package oms_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// US-150: Interface admin CRUD endpoints under /api/v2/ontologies/...

func TestListInterfacesForOntologyAdmin_ReturnsInterfaces(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test"},
		},
		interfaces: []oms.Interface{
			{
				RID:               "ri.ontology.main.interface.i1",
				OntologyRID:       "ri.ontology.main.ontology.1",
				APIName:           "Addressable",
				DisplayName:       "Addressable",
				SharedProperties:  json.RawMessage(`[]`),
				OutgoingLinkTypes: json.RawMessage(`[]`),
			},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/interfacesAdmin", handler.ListInterfacesForOntologyAdmin)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1/interfacesAdmin", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	body := parseJSON(t, w.Body.Bytes())
	data, ok := body["data"].([]interface{})
	if !ok {
		t.Fatalf("expected data to be array, got %T", body["data"])
	}
	if len(data) != 1 {
		t.Fatalf("expected 1 interface, got %d", len(data))
	}
	iface := data[0].(map[string]interface{})
	if iface["apiName"] != "Addressable" {
		t.Errorf("apiName = %v", iface["apiName"])
	}
}

func TestCreateInterface_V2Route_PersistsOutgoingLinkTypes(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/interfaces", handler.CreateInterface)

	bodyJSON := map[string]interface{}{
		"apiName":     "Locatable",
		"displayName": "Locatable",
		"sharedProperties": []interface{}{
			map[string]interface{}{"apiName": "latitude", "baseType": "double", "isArray": false},
		},
		"outgoingLinkTypes": []interface{}{
			map[string]interface{}{
				"apiName":                 "locatedAt",
				"displayName":             "Located At",
				"linkedEntityTypeApiName": "Address",
				"cardinality":             "ONE_TO_ONE",
			},
		},
	}
	bodyBytes, _ := json.Marshal(bodyJSON)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/ri.ontology.main.ontology.1/interfaces", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if len(repo.interfaces) != 1 {
		t.Fatalf("expected 1 interface stored, got %d", len(repo.interfaces))
	}
	stored := repo.interfaces[0]
	if stored.APIName != "Locatable" {
		t.Errorf("APIName = %s", stored.APIName)
	}
	if !strings.Contains(string(stored.SharedProperties), "latitude") {
		t.Errorf("expected sharedProperties persisted, got %s", string(stored.SharedProperties))
	}
	if !strings.Contains(string(stored.OutgoingLinkTypes), "locatedAt") {
		t.Errorf("expected outgoingLinkTypes persisted, got %s", string(stored.OutgoingLinkTypes))
	}
}

func TestUpdateInterface_V2Route_PersistsOutgoingLinkTypes(t *testing.T) {
	existing := oms.Interface{
		RID:               "ri.ontology.main.interface.i1",
		OntologyRID:       "ri.ontology.main.ontology.1",
		APIName:           "Locatable",
		DisplayName:       "Locatable",
		SharedProperties:  json.RawMessage(`[]`),
		OutgoingLinkTypes: json.RawMessage(`[]`),
	}
	repo := &mockRepo{
		ontologies: []oms.Ontology{{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test"}},
		interfaces: []oms.Interface{existing},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Put("/api/v2/ontologies/{ontologyApiName}/interfaces/byRid/{interfaceRid}", handler.UpdateInterface)

	bodyJSON := map[string]interface{}{
		"displayName": "Locatable v2",
		"outgoingLinkTypes": []interface{}{
			map[string]interface{}{
				"apiName":                 "locatedAt",
				"displayName":             "Located At",
				"linkedEntityTypeApiName": "Address",
				"cardinality":             "ONE_TO_ONE",
			},
		},
	}
	bodyBytes, _ := json.Marshal(bodyJSON)

	req := httptest.NewRequest(http.MethodPut,
		"/api/v2/ontologies/ri.ontology.main.ontology.1/interfaces/byRid/"+existing.RID,
		bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	stored := repo.interfaces[0]
	if stored.DisplayName != "Locatable v2" {
		t.Errorf("expected displayName updated, got %s", stored.DisplayName)
	}
	if !strings.Contains(string(stored.OutgoingLinkTypes), "locatedAt") {
		t.Errorf("expected outgoingLinkTypes persisted, got %s", string(stored.OutgoingLinkTypes))
	}
}

func TestDeleteInterface_V2Route(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test"}},
		interfaces: []oms.Interface{{
			RID:               "ri.ontology.main.interface.i1",
			OntologyRID:       "ri.ontology.main.ontology.1",
			APIName:           "Locatable",
			DisplayName:       "Locatable",
			SharedProperties:  json.RawMessage(`[]`),
			OutgoingLinkTypes: json.RawMessage(`[]`),
		}},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Delete("/api/v2/ontologies/{ontologyApiName}/interfaces/byRid/{interfaceRid}", handler.DeleteInterface)

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v2/ontologies/ri.ontology.main.ontology.1/interfaces/byRid/ri.ontology.main.interface.i1",
		nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if len(repo.interfaces) != 0 {
		t.Errorf("expected interfaces empty after delete, got %d", len(repo.interfaces))
	}
}
