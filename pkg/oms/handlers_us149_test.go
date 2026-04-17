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

// US-149: ActionType admin CRUD endpoints under /api/v2/ontologies/...

func TestListActionTypesForOntologyAdmin_IncludesRules(t *testing.T) {
	rules := json.RawMessage(`[{"type":"createObject","objectType":"employee"}]`)
	params := json.RawMessage(`[{"id":"name","type":"string","required":true}]`)

	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test"},
		},
		actionTypes: []oms.ActionType{
			{
				RID:         "ri.ontology.main.action-type.at1",
				OntologyRID: "ri.ontology.main.ontology.1",
				APIName:     "createEmployee",
				DisplayName: "Create Employee",
				Status:      "ACTIVE",
				Parameters:  params,
				Rules:       rules,
			},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypesAdmin", handler.ListActionTypesForOntologyAdmin)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1/actionTypesAdmin", nil)
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
		t.Fatalf("expected 1 action type, got %d", len(data))
	}
	at := data[0].(map[string]interface{})
	if at["apiName"] != "createEmployee" {
		t.Errorf("apiName = %v", at["apiName"])
	}
	// Must expose rules in admin view.
	if _, ok := at["rules"]; !ok {
		t.Errorf("expected rules in admin response, got %v", at)
	}
	// Must expose parameters in V2 wire format (Record<id, ActionParameterV2>).
	paramsMap, ok := at["parameters"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected parameters to be object, got %T", at["parameters"])
	}
	name, ok := paramsMap["name"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected name param entry, got %v", paramsMap)
	}
	if dt, ok := name["dataType"].(map[string]interface{}); !ok || dt["type"] != "string" {
		t.Errorf("expected dataType.type = string, got %v", name["dataType"])
	}
}

func TestCreateActionType_V2Route(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/actionTypes", handler.CreateActionType)

	bodyJSON := map[string]interface{}{
		"apiName":     "createEmployee",
		"displayName": "Create Employee",
		"status":      "ACTIVE",
		"parameters": []interface{}{
			map[string]interface{}{"id": "name", "type": "string", "required": true},
		},
		"rules": []interface{}{
			map[string]interface{}{"type": "createObject", "objectType": "employee"},
		},
	}
	bodyBytes, _ := json.Marshal(bodyJSON)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/ri.ontology.main.ontology.1/actionTypes", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	rid, ok := resp["rid"].(string)
	if !ok || !strings.HasPrefix(rid, "ri.ontology.main.action-type.") {
		t.Errorf("expected action-type RID, got %v", resp["rid"])
	}
	if len(repo.actionTypes) != 1 {
		t.Fatalf("expected 1 action type stored, got %d", len(repo.actionTypes))
	}
	stored := repo.actionTypes[0]
	if stored.APIName != "createEmployee" {
		t.Errorf("stored APIName = %s", stored.APIName)
	}
	if len(stored.Rules) == 0 {
		t.Errorf("expected rules persisted")
	}
}

func TestUpdateActionType_V2Route(t *testing.T) {
	existing := oms.ActionType{
		RID:         "ri.ontology.main.action-type.at1",
		OntologyRID: "ri.ontology.main.ontology.1",
		APIName:     "createEmployee",
		DisplayName: "Create Employee",
		Status:      "ACTIVE",
		Parameters:  json.RawMessage(`[]`),
		Rules:       json.RawMessage(`[]`),
	}
	repo := &mockRepo{
		ontologies:  []oms.Ontology{{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test"}},
		actionTypes: []oms.ActionType{existing},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Put("/api/v2/ontologies/{ontologyApiName}/actionTypes/byRid/{actionTypeRid}", handler.UpdateActionType)

	bodyJSON := map[string]interface{}{
		"displayName": "Create New Employee",
		"status":      "ACTIVE",
		"parameters": []interface{}{
			map[string]interface{}{"id": "name", "type": "string"},
		},
		"rules": []interface{}{
			map[string]interface{}{"type": "createObject", "objectType": "employee"},
		},
	}
	bodyBytes, _ := json.Marshal(bodyJSON)

	req := httptest.NewRequest(http.MethodPut,
		"/api/v2/ontologies/ri.ontology.main.ontology.1/actionTypes/byRid/"+existing.RID,
		bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	stored := repo.actionTypes[0]
	if stored.DisplayName != "Create New Employee" {
		t.Errorf("expected displayName updated, got %s", stored.DisplayName)
	}
	if !strings.Contains(string(stored.Rules), "createObject") {
		t.Errorf("expected rules persisted, got %s", string(stored.Rules))
	}
}

func TestDeleteActionType_V2Route(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test"}},
		actionTypes: []oms.ActionType{{
			RID:         "ri.ontology.main.action-type.at1",
			OntologyRID: "ri.ontology.main.ontology.1",
			APIName:     "createEmployee",
			DisplayName: "Create Employee",
			Status:      "ACTIVE",
			Parameters:  json.RawMessage(`[]`),
			Rules:       json.RawMessage(`[]`),
		}},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Delete("/api/v2/ontologies/{ontologyApiName}/actionTypes/byRid/{actionTypeRid}", handler.DeleteActionType)

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v2/ontologies/ri.ontology.main.ontology.1/actionTypes/byRid/ri.ontology.main.action-type.at1",
		nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if len(repo.actionTypes) != 0 {
		t.Errorf("expected action types empty after delete, got %d", len(repo.actionTypes))
	}
}
