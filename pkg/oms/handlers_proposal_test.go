package oms_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

func TestCreateProposal_Success(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
		branches: []oms.OntologyBranch{
			{ID: "ri.ontology.main.branch.1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/x", Status: "open"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals", handler.CreateProposal)

	body := `{"branchId":"ri.ontology.main.branch.1","title":"Add Buildings","description":"Adds building object type","author":"alice"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/proposals", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	if resp["title"] != "Add Buildings" {
		t.Errorf("expected title 'Add Buildings', got %v", resp["title"])
	}
	if resp["status"] != "pending" {
		t.Errorf("expected status 'pending', got %v", resp["status"])
	}
	if resp["branchId"] != "ri.ontology.main.branch.1" {
		t.Errorf("expected branchId, got %v", resp["branchId"])
	}
	id, ok := resp["id"].(string)
	if !ok || !strings.HasPrefix(id, "ri.ontology.main.proposal.") {
		t.Errorf("expected valid proposal RID, got %v", resp["id"])
	}
	if len(repo.proposals) != 1 {
		t.Errorf("expected 1 proposal in repo, got %d", len(repo.proposals))
	}
}

func TestCreateProposal_MissingTitle(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
		branches: []oms.OntologyBranch{
			{ID: "ri.ontology.main.branch.1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/x", Status: "open"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals", handler.CreateProposal)

	body := `{"branchId":"ri.ontology.main.branch.1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/proposals", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	if resp["errorCode"] != "INVALID_ARGUMENT" {
		t.Errorf("expected errorCode INVALID_ARGUMENT, got %v", resp["errorCode"])
	}
}

func TestCreateProposal_MissingBranchId(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals", handler.CreateProposal)

	body := `{"title":"Add Buildings"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/proposals", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestCreateProposal_BranchNotFound(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals", handler.CreateProposal)

	body := `{"branchId":"nonexistent","title":"Add Buildings"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/proposals", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestCreateProposal_BranchNotOpen(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
		branches: []oms.OntologyBranch{
			{ID: "ri.ontology.main.branch.1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/x", Status: "closed"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals", handler.CreateProposal)

	body := `{"branchId":"ri.ontology.main.branch.1","title":"Add Buildings"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/proposals", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestCreateProposal_OntologyNotFound(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals", handler.CreateProposal)

	body := `{"branchId":"b1","title":"Add Buildings"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/nonexistent/proposals", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestListProposals_Success(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
		proposals: []oms.OntologyProposal{
			{ID: "p1", OntologyRID: "ri.ontology.main.ontology.1", Title: "Prop A", Status: "pending"},
			{ID: "p2", OntologyRID: "ri.ontology.main.ontology.1", Title: "Prop B", Status: "approved"},
			{ID: "p3", OntologyRID: "ri.ontology.main.ontology.1", Title: "Prop C", Status: "rejected"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/proposals", handler.ListProposals)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test/proposals", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	data, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}
	if len(data) != 3 {
		t.Errorf("expected 3 proposals, got %d", len(data))
	}
}

func TestListProposals_WithStatusFilter(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
		proposals: []oms.OntologyProposal{
			{ID: "p1", OntologyRID: "ri.ontology.main.ontology.1", Title: "Prop A", Status: "pending"},
			{ID: "p2", OntologyRID: "ri.ontology.main.ontology.1", Title: "Prop B", Status: "approved"},
			{ID: "p3", OntologyRID: "ri.ontology.main.ontology.1", Title: "Prop C", Status: "rejected"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/proposals", handler.ListProposals)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test/proposals?status=pending", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	data := resp["data"].([]interface{})
	if len(data) != 1 {
		t.Errorf("expected 1 pending proposal, got %d", len(data))
	}
	first := data[0].(map[string]interface{})
	if first["status"] != "pending" {
		t.Errorf("expected status pending, got %v", first["status"])
	}
}

func TestListProposals_Empty(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/proposals", handler.ListProposals)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test/proposals", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	resp := parseJSON(t, w.Body.Bytes())
	data := resp["data"].([]interface{})
	if len(data) != 0 {
		t.Errorf("expected empty array, got %d items", len(data))
	}
}

func TestGetProposal_Success(t *testing.T) {
	repo := &mockRepo{
		proposals: []oms.OntologyProposal{
			{ID: "p1", OntologyRID: "ri.ontology.main.ontology.1", BranchID: "b1", Title: "Add Buildings", Status: "pending", Author: "alice"},
		},
		proposalReviews: []oms.ProposalReview{
			{ID: "r1", ProposalID: "p1", Reviewer: "bob", Decision: "approve"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}", handler.GetProposal)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test/proposals/p1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	if resp["title"] != "Add Buildings" {
		t.Errorf("expected title 'Add Buildings', got %v", resp["title"])
	}
	if resp["author"] != "alice" {
		t.Errorf("expected author 'alice', got %v", resp["author"])
	}
	reviews, ok := resp["reviews"].([]interface{})
	if !ok {
		t.Fatal("expected reviews to be an array")
	}
	if len(reviews) != 1 {
		t.Errorf("expected 1 review, got %d", len(reviews))
	}
	review := reviews[0].(map[string]interface{})
	if review["reviewer"] != "bob" {
		t.Errorf("expected reviewer 'bob', got %v", review["reviewer"])
	}
	if review["decision"] != "approve" {
		t.Errorf("expected decision 'approve', got %v", review["decision"])
	}
}

func TestGetProposal_NotFound(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}", handler.GetProposal)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test/proposals/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestProposal_FullLifecycle(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
		branches: []oms.OntologyBranch{
			{ID: "ri.ontology.main.branch.1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/x", Status: "open"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	router := chi.NewRouter()
	router.Post("/api/v2/ontologies/{ontologyApiName}/proposals", handler.CreateProposal)
	router.Get("/api/v2/ontologies/{ontologyApiName}/proposals", handler.ListProposals)
	router.Get("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}", handler.GetProposal)

	// Step 1: Create proposal
	createBody := `{"branchId":"ri.ontology.main.branch.1","title":"Add Buildings","author":"alice"}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/proposals", strings.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	router.ServeHTTP(createW, createReq)

	if createW.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d; body: %s", createW.Code, createW.Body.String())
	}
	createResp := parseJSON(t, createW.Body.Bytes())
	proposalID := createResp["id"].(string)

	// Step 2: List proposals — should return the created one
	listReq := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test/proposals", nil)
	listW := httptest.NewRecorder()
	router.ServeHTTP(listW, listReq)

	if listW.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", listW.Code)
	}
	listResp := parseJSON(t, listW.Body.Bytes())
	data := listResp["data"].([]interface{})
	if len(data) != 1 {
		t.Fatalf("list: expected 1 proposal, got %d", len(data))
	}

	// Step 3: Get proposal with reviews
	getReq := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test/proposals/"+proposalID, nil)
	getW := httptest.NewRecorder()
	router.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d; body: %s", getW.Code, getW.Body.String())
	}
	getResp := parseJSON(t, getW.Body.Bytes())
	if getResp["title"] != "Add Buildings" {
		t.Errorf("get: expected title 'Add Buildings', got %v", getResp["title"])
	}
	reviews := getResp["reviews"].([]interface{})
	if len(reviews) != 0 {
		t.Errorf("get: expected 0 reviews, got %d", len(reviews))
	}
}
