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

// --- CreateBranch Tests ---

func TestCreateBranch_Success(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches", handler.CreateBranch)

	body := `{"name":"feature/add-buildings"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/branches", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	if resp["name"] != "feature/add-buildings" {
		t.Errorf("name = %v, want %q", resp["name"], "feature/add-buildings")
	}
	if resp["status"] != "open" {
		t.Errorf("status = %v, want %q", resp["status"], "open")
	}
	if resp["ontologyRid"] != "ri.ontology.main.ontology.1" {
		t.Errorf("ontologyRid = %v, want %q", resp["ontologyRid"], "ri.ontology.main.ontology.1")
	}
	if resp["id"] == nil || resp["id"] == "" {
		t.Error("expected id to be generated")
	}
}

func TestCreateBranch_MissingName(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches", handler.CreateBranch)

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/branches", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestCreateBranch_DuplicateName(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/dup", Status: "open"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches", handler.CreateBranch)

	body := `{"name":"feature/dup"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/branches", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected status 409, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestCreateBranch_OntologyNotFound(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches", handler.CreateBranch)

	body := `{"name":"feature/x"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/nonexistent/branches", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

// --- ListBranches Tests ---

func TestListBranches_ReturnsOpenBranches(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "branch-a", Status: "open"},
			{ID: "br-2", OntologyRID: "ri.ontology.main.ontology.1", Name: "branch-b", Status: "open"},
			{ID: "br-3", OntologyRID: "ri.ontology.main.ontology.1", Name: "branch-closed", Status: "closed"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/branches", handler.ListBranches)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test/branches", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	data, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}
	if len(data) != 2 {
		t.Errorf("expected 2 open branches, got %d", len(data))
	}
}

func TestListBranches_Empty(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/branches", handler.ListBranches)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test/branches", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	resp := parseJSON(t, w.Body.Bytes())
	data, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}
	if len(data) != 0 {
		t.Errorf("expected empty array, got %d items", len(data))
	}
}

// --- GetBranch Tests ---

func TestGetBranch_Success(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/get", Status: "open", BaseVersion: 3},
		},
		branchChanges: []oms.BranchChange{
			{ID: "chg-1", BranchID: "br-1", ChangeType: "ADDED", EntityType: "objectType"},
			{ID: "chg-2", BranchID: "br-1", ChangeType: "MODIFIED", EntityType: "property"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}", handler.GetBranch)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test/branches/br-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	if resp["id"] != "br-1" {
		t.Errorf("id = %v, want %q", resp["id"], "br-1")
	}
	if resp["name"] != "feature/get" {
		t.Errorf("name = %v, want %q", resp["name"], "feature/get")
	}
	// changeCount should be 2
	changeCount, ok := resp["changeCount"].(float64)
	if !ok {
		t.Fatal("expected changeCount to be a number")
	}
	if int(changeCount) != 2 {
		t.Errorf("changeCount = %v, want 2", changeCount)
	}
}

func TestGetBranch_NotFound(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}", handler.GetBranch)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test/branches/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

// --- CloseBranch Tests ---

func TestCloseBranch_Success(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "to-close", Status: "open"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Delete("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}", handler.CloseBranch)

	req := httptest.NewRequest(http.MethodDelete, "/api/v2/ontologies/test/branches/br-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestCloseBranch_NotFound(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Delete("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}", handler.CloseBranch)

	req := httptest.NewRequest(http.MethodDelete, "/api/v2/ontologies/test/branches/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

// --- Integration-style: Create → List → Get → Close lifecycle ---

func TestBranch_FullLifecycle(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches", handler.CreateBranch)
	r.Get("/api/v2/ontologies/{ontologyApiName}/branches", handler.ListBranches)
	r.Get("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}", handler.GetBranch)
	r.Delete("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}", handler.CloseBranch)

	// Step 1: Create branch
	createBody := `{"name":"lifecycle-branch"}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/branches", bytes.NewBufferString(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	r.ServeHTTP(createW, createReq)

	if createW.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d; body: %s", createW.Code, createW.Body.String())
	}

	var createResp map[string]interface{}
	if err := json.Unmarshal(createW.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("parse create response: %v", err)
	}
	branchID, ok := createResp["id"].(string)
	if !ok || branchID == "" {
		t.Fatal("expected branch id in create response")
	}

	// Step 2: List branches
	listReq := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test/branches", nil)
	listW := httptest.NewRecorder()
	r.ServeHTTP(listW, listReq)

	if listW.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", listW.Code)
	}
	listResp := parseJSON(t, listW.Body.Bytes())
	data, ok := listResp["data"].([]interface{})
	if !ok {
		t.Fatal("list: expected data array")
	}
	if len(data) != 1 {
		t.Errorf("list: expected 1 branch, got %d", len(data))
	}

	// Step 3: Get branch
	getReq := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test/branches/"+branchID, nil)
	getW := httptest.NewRecorder()
	r.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d; body: %s", getW.Code, getW.Body.String())
	}
	getResp := parseJSON(t, getW.Body.Bytes())
	if getResp["name"] != "lifecycle-branch" {
		t.Errorf("get: name = %v, want %q", getResp["name"], "lifecycle-branch")
	}

	// Step 4: Close branch
	closeReq := httptest.NewRequest(http.MethodDelete, "/api/v2/ontologies/test/branches/"+branchID, nil)
	closeW := httptest.NewRecorder()
	r.ServeHTTP(closeW, closeReq)

	if closeW.Code != http.StatusNoContent {
		t.Fatalf("close: expected 204, got %d; body: %s", closeW.Code, closeW.Body.String())
	}

	// Step 5: List should show no open branches (closed branch excluded)
	listReq2 := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test/branches", nil)
	listW2 := httptest.NewRecorder()
	r.ServeHTTP(listW2, listReq2)

	if listW2.Code != http.StatusOK {
		t.Fatalf("list after close: expected 200, got %d", listW2.Code)
	}
	listResp2 := parseJSON(t, listW2.Body.Bytes())
	data2, ok := listResp2["data"].([]interface{})
	if !ok {
		t.Fatal("list after close: expected data array")
	}
	if len(data2) != 0 {
		t.Errorf("list after close: expected 0 open branches, got %d", len(data2))
	}
}
