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

// --- US-118: Proposal Review Workflow ---

func TestApproveProposal_Success(t *testing.T) {
	repo := &mockRepo{
		proposals: []oms.OntologyProposal{
			{ID: "p1", OntologyRID: "ri.ontology.main.ontology.1", BranchID: "b1", Title: "Add Buildings", Status: "pending", Author: "alice"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/approve", handler.ApproveProposal)

	body := `{"reviewer":"bob"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/proposals/p1/approve", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	if resp["status"] != "approved" {
		t.Errorf("expected status 'approved', got %v", resp["status"])
	}

	// Verify review was created
	if len(repo.proposalReviews) != 1 {
		t.Fatalf("expected 1 review, got %d", len(repo.proposalReviews))
	}
	if repo.proposalReviews[0].Decision != "approve" {
		t.Errorf("expected decision 'approve', got %v", repo.proposalReviews[0].Decision)
	}
	if repo.proposalReviews[0].Reviewer != "bob" {
		t.Errorf("expected reviewer 'bob', got %v", repo.proposalReviews[0].Reviewer)
	}

	// Verify proposal status was updated
	if repo.proposals[0].Status != "approved" {
		t.Errorf("expected proposal status 'approved', got %v", repo.proposals[0].Status)
	}
}

func TestRejectProposal_Success(t *testing.T) {
	repo := &mockRepo{
		proposals: []oms.OntologyProposal{
			{ID: "p1", OntologyRID: "ri.ontology.main.ontology.1", BranchID: "b1", Title: "Add Buildings", Status: "pending", Author: "alice"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/reject", handler.RejectProposal)

	body := `{"reviewer":"bob","reason":"needs more work on property types"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/proposals/p1/reject", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	if resp["status"] != "rejected" {
		t.Errorf("expected status 'rejected', got %v", resp["status"])
	}

	// Verify review was created with reason
	if len(repo.proposalReviews) != 1 {
		t.Fatalf("expected 1 review, got %d", len(repo.proposalReviews))
	}
	if repo.proposalReviews[0].Decision != "reject" {
		t.Errorf("expected decision 'reject', got %v", repo.proposalReviews[0].Decision)
	}
	if repo.proposalReviews[0].Reason != "needs more work on property types" {
		t.Errorf("expected reason, got %v", repo.proposalReviews[0].Reason)
	}

	// Verify proposal status was updated
	if repo.proposals[0].Status != "rejected" {
		t.Errorf("expected proposal status 'rejected', got %v", repo.proposals[0].Status)
	}
}

func TestApproveProposal_SelfReview_Returns403(t *testing.T) {
	repo := &mockRepo{
		proposals: []oms.OntologyProposal{
			{ID: "p1", OntologyRID: "ri.ontology.main.ontology.1", BranchID: "b1", Title: "Add Buildings", Status: "pending", Author: "alice"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/approve", handler.ApproveProposal)

	body := `{"reviewer":"alice"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/proposals/p1/approve", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	if resp["errorCode"] != "PERMISSION_DENIED" {
		t.Errorf("expected errorCode PERMISSION_DENIED, got %v", resp["errorCode"])
	}

	// Verify no review was created
	if len(repo.proposalReviews) != 0 {
		t.Errorf("expected 0 reviews, got %d", len(repo.proposalReviews))
	}
}

func TestRejectProposal_SelfReview_Returns403(t *testing.T) {
	repo := &mockRepo{
		proposals: []oms.OntologyProposal{
			{ID: "p1", OntologyRID: "ri.ontology.main.ontology.1", BranchID: "b1", Title: "Add Buildings", Status: "pending", Author: "alice"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/reject", handler.RejectProposal)

	body := `{"reviewer":"alice","reason":"some reason"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/proposals/p1/reject", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", w.Code, w.Body.String())
	}

	// Verify no review was created
	if len(repo.proposalReviews) != 0 {
		t.Errorf("expected 0 reviews, got %d", len(repo.proposalReviews))
	}
}

func TestApproveProposal_NotFound(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/approve", handler.ApproveProposal)

	body := `{"reviewer":"bob"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/proposals/nonexistent/approve", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestRejectProposal_NotFound(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/reject", handler.RejectProposal)

	body := `{"reviewer":"bob","reason":"bad"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/proposals/nonexistent/reject", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestApproveProposal_AlreadyRejected_Returns409(t *testing.T) {
	repo := &mockRepo{
		proposals: []oms.OntologyProposal{
			{ID: "p1", OntologyRID: "ri.ontology.main.ontology.1", BranchID: "b1", Title: "Add Buildings", Status: "rejected", Author: "alice"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/approve", handler.ApproveProposal)

	body := `{"reviewer":"bob"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/proposals/p1/approve", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestRejectProposal_AlreadyMerged_Returns409(t *testing.T) {
	repo := &mockRepo{
		proposals: []oms.OntologyProposal{
			{ID: "p1", OntologyRID: "ri.ontology.main.ontology.1", BranchID: "b1", Title: "Add Buildings", Status: "merged", Author: "alice"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/reject", handler.RejectProposal)

	body := `{"reviewer":"bob","reason":"too late"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/proposals/p1/reject", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestApproveProposal_RejectionOverridesApproval(t *testing.T) {
	// Scenario: approve first, then reject → status should be "rejected"
	repo := &mockRepo{
		proposals: []oms.OntologyProposal{
			{ID: "p1", OntologyRID: "ri.ontology.main.ontology.1", BranchID: "b1", Title: "Add Buildings", Status: "pending", Author: "alice"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	router := chi.NewRouter()
	router.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/approve", handler.ApproveProposal)
	router.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/reject", handler.RejectProposal)

	// Step 1: Bob approves
	approveBody := `{"reviewer":"bob"}`
	approveReq := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/proposals/p1/approve", strings.NewReader(approveBody))
	approveReq.Header.Set("Content-Type", "application/json")
	approveW := httptest.NewRecorder()
	router.ServeHTTP(approveW, approveReq)

	if approveW.Code != http.StatusOK {
		t.Fatalf("approve: expected 200, got %d; body: %s", approveW.Code, approveW.Body.String())
	}
	if repo.proposals[0].Status != "approved" {
		t.Fatalf("after approve: expected status 'approved', got %v", repo.proposals[0].Status)
	}

	// Step 2: Carol rejects — rejection overrides approval
	rejectBody := `{"reviewer":"carol","reason":"security concern"}`
	rejectReq := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/proposals/p1/reject", strings.NewReader(rejectBody))
	rejectReq.Header.Set("Content-Type", "application/json")
	rejectW := httptest.NewRecorder()
	router.ServeHTTP(rejectW, rejectReq)

	if rejectW.Code != http.StatusOK {
		t.Fatalf("reject: expected 200, got %d; body: %s", rejectW.Code, rejectW.Body.String())
	}
	if repo.proposals[0].Status != "rejected" {
		t.Errorf("after reject: expected status 'rejected', got %v", repo.proposals[0].Status)
	}

	// Verify 2 reviews exist
	if len(repo.proposalReviews) != 2 {
		t.Errorf("expected 2 reviews, got %d", len(repo.proposalReviews))
	}
}

func TestApproveProposal_MissingReviewer(t *testing.T) {
	repo := &mockRepo{
		proposals: []oms.OntologyProposal{
			{ID: "p1", OntologyRID: "ri.ontology.main.ontology.1", BranchID: "b1", Title: "Add Buildings", Status: "pending", Author: "alice"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/approve", handler.ApproveProposal)

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/proposals/p1/approve", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

// --- US-119: Branch Merge with Conflict Detection ---

func TestMergeProposal_Success(t *testing.T) {
	otJSON := `{"rid":"ri.ontology.main.objectType.new1","apiName":"Building","displayName":"Building","status":"ACTIVE","visibility":"NORMAL","primaryKey":"id"}`
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
		branches: []oms.OntologyBranch{
			{ID: "b1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/add-building", BaseVersion: 0, Status: "open"},
		},
		branchChanges: []oms.BranchChange{
			{
				ID: "c1", BranchID: "b1", ChangeType: "ADDED", EntityType: "objectType",
				EntityRID:  "ri.ontology.main.objectType.new1",
				AfterState: json.RawMessage(otJSON),
			},
		},
		proposals: []oms.OntologyProposal{
			{ID: "p1", BranchID: "b1", OntologyRID: "ri.ontology.main.ontology.1", Title: "Add Building", Status: "approved", Author: "alice"},
		},
		ontologyVersion: 0,
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/merge", handler.MergeProposal)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/proposals/p1/merge", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	if resp["status"] != "merged" {
		t.Errorf("expected status 'merged', got %v", resp["status"])
	}

	// Proposal should be marked merged
	if repo.proposals[0].Status != "merged" {
		t.Errorf("expected proposal status 'merged', got %v", repo.proposals[0].Status)
	}

	// Branch should be marked merged
	if repo.branches[0].Status != "merged" {
		t.Errorf("expected branch status 'merged', got %v", repo.branches[0].Status)
	}

	// ObjectType should exist on main
	if len(repo.objectTypes) != 1 {
		t.Fatalf("expected 1 objectType on main, got %d", len(repo.objectTypes))
	}
	if repo.objectTypes[0].APIName != "Building" {
		t.Errorf("expected objectType apiName 'Building', got %v", repo.objectTypes[0].APIName)
	}

	// Ontology version should be incremented
	if repo.ontologyVersion != 1 {
		t.Errorf("expected ontology version 1, got %d", repo.ontologyVersion)
	}
}

func TestMergeProposal_NotApproved_Returns409(t *testing.T) {
	repo := &mockRepo{
		proposals: []oms.OntologyProposal{
			{ID: "p1", BranchID: "b1", OntologyRID: "ri.ontology.main.ontology.1", Title: "Add Building", Status: "pending", Author: "alice"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/merge", handler.MergeProposal)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/proposals/p1/merge", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestMergeProposal_AlreadyMerged_Returns409(t *testing.T) {
	repo := &mockRepo{
		proposals: []oms.OntologyProposal{
			{ID: "p1", BranchID: "b1", OntologyRID: "ri.ontology.main.ontology.1", Title: "Add Building", Status: "merged", Author: "alice"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/merge", handler.MergeProposal)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/proposals/p1/merge", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestMergeProposal_NotFound(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/merge", handler.MergeProposal)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/proposals/nonexistent/merge", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestMergeProposal_Conflict(t *testing.T) {
	// Branch was created at version 0, but main is now at version 1
	// The branch modified Employee (before_state has displayName "Employee"),
	// but main also modified it (current has displayName "Employee Updated On Main")
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ri.ontology.main.objectType.1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Employee", DisplayName: "Employee Updated On Main", Status: "ACTIVE", Visibility: "NORMAL"},
		},
		branches: []oms.OntologyBranch{
			{ID: "b1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/modify-emp", BaseVersion: 0, Status: "open"},
		},
		branchChanges: []oms.BranchChange{
			{
				ID: "c1", BranchID: "b1", ChangeType: "MODIFIED", EntityType: "objectType",
				EntityRID:   "ri.ontology.main.objectType.1",
				BeforeState: json.RawMessage(`{"rid":"ri.ontology.main.objectType.1","apiName":"Employee","displayName":"Employee","status":"ACTIVE","visibility":"NORMAL"}`),
				AfterState:  json.RawMessage(`{"rid":"ri.ontology.main.objectType.1","apiName":"Employee","displayName":"Employee Branch Version","status":"ACTIVE","visibility":"NORMAL"}`),
			},
		},
		proposals: []oms.OntologyProposal{
			{ID: "p1", BranchID: "b1", OntologyRID: "ri.ontology.main.ontology.1", Title: "Modify Employee", Status: "approved", Author: "alice"},
		},
		ontologyVersion: 1, // main modified since branch base_version=0
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/merge", handler.MergeProposal)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/proposals/p1/merge", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	if resp["errorCode"] != "MERGE_CONFLICT" {
		t.Errorf("expected errorCode 'MERGE_CONFLICT', got %v", resp["errorCode"])
	}
	conflicts, ok := resp["conflicts"].([]interface{})
	if !ok || len(conflicts) == 0 {
		t.Fatalf("expected at least 1 conflict, got %v", resp["conflicts"])
	}
	conflict := conflicts[0].(map[string]interface{})
	if conflict["entityType"] != "objectType" {
		t.Errorf("expected conflict entityType 'objectType', got %v", conflict["entityType"])
	}
	if conflict["changeType"] != "MODIFIED" {
		t.Errorf("expected conflict changeType 'MODIFIED', got %v", conflict["changeType"])
	}
}

func TestMergeProposal_NoConflict_SameVersion(t *testing.T) {
	// Branch base_version == ontology version → no conflict detection needed
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ri.ontology.main.objectType.1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Employee", DisplayName: "Employee", Status: "ACTIVE", Visibility: "NORMAL"},
		},
		branches: []oms.OntologyBranch{
			{ID: "b1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/modify-emp", BaseVersion: 0, Status: "open"},
		},
		branchChanges: []oms.BranchChange{
			{
				ID: "c1", BranchID: "b1", ChangeType: "MODIFIED", EntityType: "objectType",
				EntityRID:   "ri.ontology.main.objectType.1",
				BeforeState: json.RawMessage(`{"rid":"ri.ontology.main.objectType.1","apiName":"Employee","displayName":"Employee","status":"ACTIVE","visibility":"NORMAL"}`),
				AfterState:  json.RawMessage(`{"rid":"ri.ontology.main.objectType.1","apiName":"Employee","displayName":"Employee Modified","status":"ACTIVE","visibility":"NORMAL"}`),
			},
		},
		proposals: []oms.OntologyProposal{
			{ID: "p1", BranchID: "b1", OntologyRID: "ri.ontology.main.ontology.1", Title: "Modify Employee", Status: "approved", Author: "alice"},
		},
		ontologyVersion: 0, // same as branch base_version → no conflict detection
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/merge", handler.MergeProposal)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/proposals/p1/merge", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// ObjectType should be updated on main
	if repo.objectTypes[0].DisplayName != "Employee Modified" {
		t.Errorf("expected displayName 'Employee Modified', got %v", repo.objectTypes[0].DisplayName)
	}
}

func TestMergeProposal_FullLifecycle(t *testing.T) {
	// Create branch → add ObjectType on branch → approve → merge → ObjectType visible on main
	otJSON := `{"rid":"ri.ontology.main.objectType.new1","apiName":"Building","displayName":"Building","status":"ACTIVE","visibility":"NORMAL","primaryKey":"id"}`
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
		branches: []oms.OntologyBranch{
			{ID: "b1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/add-building", BaseVersion: 0, Status: "open"},
		},
		branchChanges: []oms.BranchChange{
			{
				ID: "c1", BranchID: "b1", ChangeType: "ADDED", EntityType: "objectType",
				EntityRID:  "ri.ontology.main.objectType.new1",
				AfterState: json.RawMessage(otJSON),
			},
		},
	}
	handler := oms.NewOMSHandler(repo)

	router := chi.NewRouter()
	router.Post("/api/v2/ontologies/{ontologyApiName}/proposals", handler.CreateProposal)
	router.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/approve", handler.ApproveProposal)
	router.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/merge", handler.MergeProposal)
	// Step 1: Create proposal
	createBody := `{"branchId":"b1","title":"Add Building","author":"alice"}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/proposals", strings.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	router.ServeHTTP(createW, createReq)

	if createW.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d; body: %s", createW.Code, createW.Body.String())
	}
	createResp := parseJSON(t, createW.Body.Bytes())
	proposalID := createResp["id"].(string)

	// Step 2: Approve proposal
	approveBody := `{"reviewer":"bob"}`
	approveReq := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/proposals/"+proposalID+"/approve", strings.NewReader(approveBody))
	approveReq.Header.Set("Content-Type", "application/json")
	approveW := httptest.NewRecorder()
	router.ServeHTTP(approveW, approveReq)

	if approveW.Code != http.StatusOK {
		t.Fatalf("approve: expected 200, got %d; body: %s", approveW.Code, approveW.Body.String())
	}

	// Step 3: Merge proposal
	mergeReq := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/proposals/"+proposalID+"/merge", nil)
	mergeW := httptest.NewRecorder()
	router.ServeHTTP(mergeW, mergeReq)

	if mergeW.Code != http.StatusOK {
		t.Fatalf("merge: expected 200, got %d; body: %s", mergeW.Code, mergeW.Body.String())
	}

	mergeResp := parseJSON(t, mergeW.Body.Bytes())
	if mergeResp["status"] != "merged" {
		t.Errorf("merge: expected status 'merged', got %v", mergeResp["status"])
	}

	// Step 4: Verify ObjectType exists on main via repo state
	if len(repo.objectTypes) != 1 {
		t.Fatalf("expected 1 objectType on main, got %d", len(repo.objectTypes))
	}
	if repo.objectTypes[0].APIName != "Building" {
		t.Errorf("expected objectType apiName 'Building', got %v", repo.objectTypes[0].APIName)
	}
	if repo.objectTypes[0].OntologyRID != "ri.ontology.main.ontology.1" {
		t.Errorf("expected objectType ontologyRID set, got %v", repo.objectTypes[0].OntologyRID)
	}

	// Verify branch is merged
	if repo.branches[0].Status != "merged" {
		t.Errorf("expected branch status 'merged', got %v", repo.branches[0].Status)
	}
}

func TestMergeProposal_DeleteChange_Success(t *testing.T) {
	// Merge with DELETE change
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ri.ontology.main.objectType.1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Employee", DisplayName: "Employee", Status: "ACTIVE"},
		},
		branches: []oms.OntologyBranch{
			{ID: "b1", OntologyRID: "ri.ontology.main.ontology.1", BaseVersion: 0, Status: "open"},
		},
		branchChanges: []oms.BranchChange{
			{
				ID: "c1", BranchID: "b1", ChangeType: "DELETED", EntityType: "objectType",
				EntityRID:   "ri.ontology.main.objectType.1",
				BeforeState: json.RawMessage(`{"rid":"ri.ontology.main.objectType.1","apiName":"Employee","displayName":"Employee","status":"ACTIVE"}`),
			},
		},
		proposals: []oms.OntologyProposal{
			{ID: "p1", BranchID: "b1", OntologyRID: "ri.ontology.main.ontology.1", Title: "Delete Employee", Status: "approved", Author: "alice"},
		},
		ontologyVersion: 0,
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/merge", handler.MergeProposal)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/proposals/p1/merge", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// ObjectType should be deleted from main
	if len(repo.objectTypes) != 0 {
		t.Errorf("expected 0 objectTypes on main, got %d", len(repo.objectTypes))
	}
}

func TestMergeProposal_MultipleChanges_Success(t *testing.T) {
	// Merge with ADDED property + MODIFIED linkType
	propJSON := `{"rid":"ri.ontology.main.property.new1","apiName":"address","displayName":"Address","baseType":"String","objectTypeRid":"ri.ontology.main.objectType.1"}`
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
		linkTypes: []oms.LinkType{
			{RID: "ri.ontology.main.linkType.1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "employs", DisplayName: "Employs"},
		},
		branches: []oms.OntologyBranch{
			{ID: "b1", OntologyRID: "ri.ontology.main.ontology.1", BaseVersion: 0, Status: "open"},
		},
		branchChanges: []oms.BranchChange{
			{
				ID: "c1", BranchID: "b1", ChangeType: "ADDED", EntityType: "property",
				EntityRID:  "ri.ontology.main.property.new1",
				AfterState: json.RawMessage(propJSON),
			},
			{
				ID: "c2", BranchID: "b1", ChangeType: "MODIFIED", EntityType: "linkType",
				EntityRID:   "ri.ontology.main.linkType.1",
				BeforeState: json.RawMessage(`{"rid":"ri.ontology.main.linkType.1","apiName":"employs","displayName":"Employs"}`),
				AfterState:  json.RawMessage(`{"rid":"ri.ontology.main.linkType.1","apiName":"employs","displayName":"Employs Updated"}`),
			},
		},
		proposals: []oms.OntologyProposal{
			{ID: "p1", BranchID: "b1", OntologyRID: "ri.ontology.main.ontology.1", Title: "Add Property + Modify Link", Status: "approved", Author: "alice"},
		},
		ontologyVersion: 0,
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/merge", handler.MergeProposal)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/proposals/p1/merge", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Property should be added
	if len(repo.properties) != 1 {
		t.Fatalf("expected 1 property, got %d", len(repo.properties))
	}
	if repo.properties[0].APIName != "address" {
		t.Errorf("expected property apiName 'address', got %v", repo.properties[0].APIName)
	}

	// LinkType should be updated
	if repo.linkTypes[0].DisplayName != "Employs Updated" {
		t.Errorf("expected linkType displayName 'Employs Updated', got %v", repo.linkTypes[0].DisplayName)
	}
}

func TestProposal_FullReviewLifecycle(t *testing.T) {
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
	router.Get("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}", handler.GetProposal)
	router.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/approve", handler.ApproveProposal)

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

	// Step 2: Approve
	approveBody := `{"reviewer":"bob"}`
	approveReq := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/proposals/"+proposalID+"/approve", strings.NewReader(approveBody))
	approveReq.Header.Set("Content-Type", "application/json")
	approveW := httptest.NewRecorder()
	router.ServeHTTP(approveW, approveReq)

	if approveW.Code != http.StatusOK {
		t.Fatalf("approve: expected 200, got %d; body: %s", approveW.Code, approveW.Body.String())
	}
	approveResp := parseJSON(t, approveW.Body.Bytes())
	if approveResp["status"] != "approved" {
		t.Errorf("approve: expected status 'approved', got %v", approveResp["status"])
	}

	// Step 3: Get proposal — should have review
	getReq := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test/proposals/"+proposalID, nil)
	getW := httptest.NewRecorder()
	router.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d; body: %s", getW.Code, getW.Body.String())
	}
	getResp := parseJSON(t, getW.Body.Bytes())
	if getResp["status"] != "approved" {
		t.Errorf("get: expected status 'approved', got %v", getResp["status"])
	}
	reviews := getResp["reviews"].([]interface{})
	if len(reviews) != 1 {
		t.Fatalf("get: expected 1 review, got %d", len(reviews))
	}
	review := reviews[0].(map[string]interface{})
	if review["reviewer"] != "bob" {
		t.Errorf("get: expected reviewer 'bob', got %v", review["reviewer"])
	}
	if review["decision"] != "approve" {
		t.Errorf("get: expected decision 'approve', got %v", review["decision"])
	}
}
