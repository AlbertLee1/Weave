package oms_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// US-383 CreateBranch handler tests covering the new parentBranchId / baseTx
// request fields and the status-alias normalisation contract.

func TestCreateBranch_WithParentBranch(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-parent", OntologyRID: "ri.ontology.main.ontology.1", Name: "parent", Status: "open"},
		},
	}
	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches", handler.CreateBranch)

	body := `{"name":"feature/leaf","parentBranchId":"br-parent","baseTx":"tx-abc"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/branches", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	if resp["parentBranchId"] != "br-parent" {
		t.Errorf("parentBranchId = %v, want %q", resp["parentBranchId"], "br-parent")
	}
	if resp["baseTx"] != "tx-abc" {
		t.Errorf("baseTx = %v, want %q", resp["baseTx"], "tx-abc")
	}
}

func TestCreateBranch_RejectsParentNotFound(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
	}
	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches", handler.CreateBranch)

	body := `{"name":"feature/leaf","parentBranchId":"br-missing"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/branches", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing parent, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestCreateBranch_RejectsParentFromOtherOntology(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-other", OntologyRID: "ri.ontology.main.ontology.OTHER", Name: "x", Status: "open"},
		},
	}
	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches", handler.CreateBranch)

	body := `{"name":"feature/leaf","parentBranchId":"br-other"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/branches", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for cross-ontology parent, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestCreateBranch_RejectsClosedParent(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-closed", OntologyRID: "ri.ontology.main.ontology.1", Name: "old", Status: "closed"},
		},
	}
	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches", handler.CreateBranch)

	body := `{"name":"feature/leaf","parentBranchId":"br-closed"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/branches", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for closed parent, got %d; body: %s", w.Code, w.Body.String())
	}
}
