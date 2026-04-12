package oms_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

func setupFunctionRouter(repo oms.Repository) *chi.Mux {
	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/functions", handler.CreateFunction)
	r.Get("/api/v2/ontologies/{ontologyApiName}/functions", handler.ListFunctions)
	r.Get("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}", handler.GetFunctionV2)
	r.Put("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}", handler.UpdateFunction)
	r.Delete("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}", handler.DeleteFunction)
	return r
}

func TestCreateFunction(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{{
			RID:         "ri.ontology.main.ontology.o1",
			APIName:     "northwind",
			DisplayName: "Northwind",
		}},
	}
	router := setupFunctionRouter(repo)

	body := `{"name":"helloWorld","sourceCode":"function main() { return 'Hello'; }","createdBy":"user1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/northwind/functions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var fn oms.Function
	if err := json.Unmarshal(w.Body.Bytes(), &fn); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if fn.Name != "helloWorld" {
		t.Errorf("expected name=helloWorld, got %s", fn.Name)
	}
	if fn.SourceCode != "function main() { return 'Hello'; }" {
		t.Errorf("unexpected sourceCode: %s", fn.SourceCode)
	}
	if fn.RID == "" {
		t.Error("expected RID to be set")
	}
}

func TestCreateFunction_MissingName(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{{
			RID:         "ri.ontology.main.ontology.o1",
			APIName:     "northwind",
			DisplayName: "Northwind",
		}},
	}
	router := setupFunctionRouter(repo)

	body := `{"sourceCode":"function main() { return 'Hello'; }"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/northwind/functions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListFunctions(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{{
			RID:         "ri.ontology.main.ontology.o1",
			APIName:     "northwind",
			DisplayName: "Northwind",
		}},
		functions: []oms.Function{
			{RID: "ri.ontology.main.function.f1", OntologyRID: "ri.ontology.main.ontology.o1", Name: "helloWorld", Version: 1, SourceCode: "return 1"},
			{RID: "ri.ontology.main.function.f2", OntologyRID: "ri.ontology.main.ontology.o1", Name: "findOrders", Version: 1, SourceCode: "return 2"},
		},
	}
	router := setupFunctionRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/functions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data []oms.Function `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 functions, got %d", len(resp.Data))
	}
}

func TestGetFunction(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{{
			RID:         "ri.ontology.main.ontology.o1",
			APIName:     "northwind",
			DisplayName: "Northwind",
		}},
		functions: []oms.Function{
			{RID: "ri.ontology.main.function.f1", OntologyRID: "ri.ontology.main.ontology.o1", Name: "helloWorld", Version: 1, SourceCode: "return 1"},
		},
	}
	router := setupFunctionRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/functions/ri.ontology.main.function.f1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var fn oms.Function
	if err := json.Unmarshal(w.Body.Bytes(), &fn); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if fn.Name != "helloWorld" {
		t.Errorf("expected name=helloWorld, got %s", fn.Name)
	}
}

func TestGetFunction_NotFound(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{{
			RID:         "ri.ontology.main.ontology.o1",
			APIName:     "northwind",
			DisplayName: "Northwind",
		}},
	}
	router := setupFunctionRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/functions/ri.ontology.main.function.nonexist", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateFunction(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{{
			RID:         "ri.ontology.main.ontology.o1",
			APIName:     "northwind",
			DisplayName: "Northwind",
		}},
		functions: []oms.Function{
			{RID: "ri.ontology.main.function.f1", OntologyRID: "ri.ontology.main.ontology.o1", Name: "helloWorld", Version: 1, SourceCode: "return 1"},
		},
	}
	router := setupFunctionRouter(repo)

	body := `{"sourceCode":"return 2","version":2}`
	req := httptest.NewRequest(http.MethodPut, "/api/v2/ontologies/northwind/functions/ri.ontology.main.function.f1", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var fn oms.Function
	if err := json.Unmarshal(w.Body.Bytes(), &fn); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if fn.SourceCode != "return 2" {
		t.Errorf("expected sourceCode='return 2', got %s", fn.SourceCode)
	}
}

func TestDeleteFunction(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{{
			RID:         "ri.ontology.main.ontology.o1",
			APIName:     "northwind",
			DisplayName: "Northwind",
		}},
		functions: []oms.Function{
			{RID: "ri.ontology.main.function.f1", OntologyRID: "ri.ontology.main.ontology.o1", Name: "helloWorld", Version: 1, SourceCode: "return 1"},
		},
	}
	router := setupFunctionRouter(repo)

	req := httptest.NewRequest(http.MethodDelete, "/api/v2/ontologies/northwind/functions/ri.ontology.main.function.f1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteFunction_NotFound(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{{
			RID:         "ri.ontology.main.ontology.o1",
			APIName:     "northwind",
			DisplayName: "Northwind",
		}},
	}
	router := setupFunctionRouter(repo)

	req := httptest.NewRequest(http.MethodDelete, "/api/v2/ontologies/northwind/functions/ri.ontology.main.function.nonexist", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}
