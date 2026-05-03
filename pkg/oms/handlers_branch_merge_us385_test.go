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

// US-385 — Branch 合并冲突检测
//
// Acceptance criteria:
// 1. POST /branches/{branchId}/diff returns added / modified / deleted
//    entries plus the inline conflict list.
// 2. Conflict identification: same apiName modified on branch and main.
// 3. POST /branches/{branchId}/merge accepts
//    conflictResolution: { "<entityType>:<apiName>": "use-branch"|"use-main" }.

// --- POST /branches/{branchId}/diff ---

func TestPostBranchDiff_CategorisesEntries_NoConflicts(t *testing.T) {
	otAdd := mustMarshal(t, oms.ObjectType{
		RID: "ri.ontology.main.objectType.added", APIName: "Added",
		DisplayName: "Added", Status: "ACTIVE",
	})
	otBefore := mustMarshal(t, oms.ObjectType{
		RID: "ri.ontology.main.objectType.existing", APIName: "Existing",
		DisplayName: "Existing", Status: "ACTIVE",
	})
	otAfter := mustMarshal(t, oms.ObjectType{
		RID: "ri.ontology.main.objectType.existing", APIName: "Existing",
		DisplayName: "Existing v2", Status: "ACTIVE",
	})
	otDeletedBefore := mustMarshal(t, oms.ObjectType{
		RID: "ri.ontology.main.objectType.gone", APIName: "Gone",
		DisplayName: "Gone", Status: "ACTIVE",
	})

	repo := &mockRepo{
		ontologies: []oms.Ontology{{RID: "ri.ontology.main.ontology.1", APIName: "test"}},
		objectTypes: []oms.ObjectType{
			{RID: "ri.ontology.main.objectType.existing", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Existing", DisplayName: "Existing", Status: "ACTIVE"},
			{RID: "ri.ontology.main.objectType.gone", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Gone", DisplayName: "Gone", Status: "ACTIVE"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/x", Status: "open", BaseVersion: 0},
		},
		branchChanges: []oms.BranchChange{
			{ID: "c1", BranchID: "br-1", ChangeType: "ADDED", EntityType: "objectType", EntityRID: "ri.ontology.main.objectType.added", AfterState: otAdd},
			{ID: "c2", BranchID: "br-1", ChangeType: "MODIFIED", EntityType: "objectType", EntityRID: "ri.ontology.main.objectType.existing", BeforeState: otBefore, AfterState: otAfter},
			{ID: "c3", BranchID: "br-1", ChangeType: "DELETED", EntityType: "objectType", EntityRID: "ri.ontology.main.objectType.gone", BeforeState: otDeletedBefore},
		},
		ontologyVersion: 0,
	}

	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/diff", handler.PostBranchDiff)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/branches/br-1/diff", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Added        []map[string]interface{} `json:"added"`
		Modified     []map[string]interface{} `json:"modified"`
		Deleted      []map[string]interface{} `json:"deleted"`
		Conflicts    []map[string]interface{} `json:"conflicts"`
		HasConflicts bool                     `json:"hasConflicts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(resp.Added) != 1 {
		t.Errorf("len(added) = %d, want 1", len(resp.Added))
	}
	if len(resp.Modified) != 1 {
		t.Errorf("len(modified) = %d, want 1", len(resp.Modified))
	}
	if len(resp.Deleted) != 1 {
		t.Errorf("len(deleted) = %d, want 1", len(resp.Deleted))
	}
	if len(resp.Conflicts) != 0 {
		t.Errorf("len(conflicts) = %d, want 0", len(resp.Conflicts))
	}
	if resp.HasConflicts {
		t.Errorf("hasConflicts = true, want false")
	}
	if v, _ := resp.Added[0]["apiName"].(string); v != "Added" {
		t.Errorf("added[0].apiName = %q, want %q", v, "Added")
	}
	if v, _ := resp.Modified[0]["apiName"].(string); v != "Existing" {
		t.Errorf("modified[0].apiName = %q, want %q", v, "Existing")
	}
	if v, _ := resp.Deleted[0]["apiName"].(string); v != "Gone" {
		t.Errorf("deleted[0].apiName = %q, want %q", v, "Gone")
	}
}

func TestPostBranchDiff_FlagsConflicts_WhenMainModifiedSameApiName(t *testing.T) {
	branchBefore := mustMarshal(t, oms.ObjectType{
		RID: "ri.ontology.main.objectType.existing", APIName: "Existing",
		DisplayName: "Existing v1", Status: "ACTIVE",
	})
	branchAfter := mustMarshal(t, oms.ObjectType{
		RID: "ri.ontology.main.objectType.existing", APIName: "Existing",
		DisplayName: "Existing branch", Status: "ACTIVE",
	})

	repo := &mockRepo{
		ontologies: []oms.Ontology{{RID: "ri.ontology.main.ontology.1", APIName: "test"}},
		objectTypes: []oms.ObjectType{
			// Main was independently modified after branch creation.
			{RID: "ri.ontology.main.objectType.existing", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Existing", DisplayName: "Existing main", Status: "ACTIVE"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/x", Status: "open", BaseVersion: 1},
		},
		branchChanges: []oms.BranchChange{
			{ID: "c1", BranchID: "br-1", ChangeType: "MODIFIED", EntityType: "objectType", EntityRID: "ri.ontology.main.objectType.existing", BeforeState: branchBefore, AfterState: branchAfter},
		},
		ontologyVersion: 2,
	}

	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/diff", handler.PostBranchDiff)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/branches/br-1/diff", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Modified     []map[string]interface{} `json:"modified"`
		Conflicts    []map[string]interface{} `json:"conflicts"`
		HasConflicts bool                     `json:"hasConflicts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !resp.HasConflicts {
		t.Errorf("hasConflicts = false, want true")
	}
	if len(resp.Conflicts) != 1 {
		t.Fatalf("len(conflicts) = %d, want 1", len(resp.Conflicts))
	}
	c := resp.Conflicts[0]
	if c["apiName"] != "Existing" {
		t.Errorf("conflict apiName = %v, want %q", c["apiName"], "Existing")
	}
	if c["entityType"] != "objectType" {
		t.Errorf("conflict entityType = %v, want %q", c["entityType"], "objectType")
	}
	if c["resolutionKey"] != "objectType:Existing" {
		t.Errorf("conflict resolutionKey = %v, want %q", c["resolutionKey"], "objectType:Existing")
	}
	if _, ok := c["mainState"]; !ok {
		t.Errorf("conflict missing mainState")
	}
	if _, ok := c["branchState"]; !ok {
		t.Errorf("conflict missing branchState")
	}
}

func TestPostBranchDiff_BranchNotFound(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{{RID: "ri.ontology.main.ontology.1", APIName: "test"}},
	}
	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/diff", handler.PostBranchDiff)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/branches/missing/diff", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

// --- POST /branches/{branchId}/merge ---

func TestMergeBranch_NoConflicts_AppliesAllChanges(t *testing.T) {
	addOT := oms.ObjectType{
		RID: "ri.ontology.main.objectType.added", OntologyRID: "ri.ontology.main.ontology.1",
		APIName: "Added", DisplayName: "Added", Status: "ACTIVE",
	}
	otBefore := mustMarshal(t, oms.ObjectType{
		RID: "ri.ontology.main.objectType.existing", APIName: "Existing",
		DisplayName: "Existing", Status: "ACTIVE",
	})
	otAfter := oms.ObjectType{
		RID: "ri.ontology.main.objectType.existing", OntologyRID: "ri.ontology.main.ontology.1",
		APIName: "Existing", DisplayName: "Existing v2", Status: "ACTIVE",
	}

	repo := &mockRepo{
		ontologies: []oms.Ontology{{RID: "ri.ontology.main.ontology.1", APIName: "test"}},
		objectTypes: []oms.ObjectType{
			{RID: "ri.ontology.main.objectType.existing", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Existing", DisplayName: "Existing", Status: "ACTIVE"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/x", Status: "open", BaseVersion: 0},
		},
		branchChanges: []oms.BranchChange{
			{ID: "c1", BranchID: "br-1", ChangeType: "ADDED", EntityType: "objectType", EntityRID: addOT.RID, AfterState: mustMarshal(t, addOT)},
			{ID: "c2", BranchID: "br-1", ChangeType: "MODIFIED", EntityType: "objectType", EntityRID: otAfter.RID, BeforeState: otBefore, AfterState: mustMarshal(t, otAfter)},
		},
		ontologyVersion: 0,
	}

	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/merge", handler.MergeBranch)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/branches/br-1/merge", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Branch       map[string]interface{} `json:"branch"`
		AppliedCount int                    `json:"appliedCount"`
		SkippedCount int                    `json:"skippedCount"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.AppliedCount != 2 {
		t.Errorf("appliedCount = %d, want 2", resp.AppliedCount)
	}
	if resp.SkippedCount != 0 {
		t.Errorf("skippedCount = %d, want 0", resp.SkippedCount)
	}
	if resp.Branch["status"] != "merged" {
		t.Errorf("branch status = %v, want %q", resp.Branch["status"], "merged")
	}

	// Repo side-effects: the new ObjectType must exist; the existing one
	// must show the v2 displayName.
	got, err := repo.GetObjectType(req.Context(), addOT.RID)
	if err != nil {
		t.Fatalf("GetObjectType(added) = %v", err)
	}
	if got.APIName != "Added" {
		t.Errorf("added apiName = %q, want %q", got.APIName, "Added")
	}
	upd, err := repo.GetObjectType(req.Context(), otAfter.RID)
	if err != nil {
		t.Fatalf("GetObjectType(existing) = %v", err)
	}
	if upd.DisplayName != "Existing v2" {
		t.Errorf("existing displayName = %q, want %q", upd.DisplayName, "Existing v2")
	}
}

func TestMergeBranch_Conflict_NoResolution_Returns409(t *testing.T) {
	branchBefore := mustMarshal(t, oms.ObjectType{
		RID: "ri.ontology.main.objectType.existing", APIName: "Existing",
		DisplayName: "Existing v1", Status: "ACTIVE",
	})
	branchAfter := mustMarshal(t, oms.ObjectType{
		RID: "ri.ontology.main.objectType.existing", APIName: "Existing",
		DisplayName: "Existing branch", Status: "ACTIVE",
	})

	repo := &mockRepo{
		ontologies: []oms.Ontology{{RID: "ri.ontology.main.ontology.1", APIName: "test"}},
		objectTypes: []oms.ObjectType{
			{RID: "ri.ontology.main.objectType.existing", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Existing", DisplayName: "Existing main", Status: "ACTIVE"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/x", Status: "open", BaseVersion: 1},
		},
		branchChanges: []oms.BranchChange{
			{ID: "c1", BranchID: "br-1", ChangeType: "MODIFIED", EntityType: "objectType", EntityRID: "ri.ontology.main.objectType.existing", BeforeState: branchBefore, AfterState: branchAfter},
		},
		ontologyVersion: 2,
	}

	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/merge", handler.MergeBranch)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/branches/br-1/merge", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["errorCode"] != "MERGE_CONFLICT" {
		t.Errorf("errorCode = %v, want %q", resp["errorCode"], "MERGE_CONFLICT")
	}
	conflicts, _ := resp["conflicts"].([]interface{})
	if len(conflicts) != 1 {
		t.Fatalf("conflicts len = %d, want 1", len(conflicts))
	}

	// Branch must remain open and main must remain unchanged.
	b, _ := repo.GetBranch(req.Context(), "br-1")
	if b.Status != "open" {
		t.Errorf("branch status = %q, want %q", b.Status, "open")
	}
	cur, _ := repo.GetObjectType(req.Context(), "ri.ontology.main.objectType.existing")
	if cur.DisplayName != "Existing main" {
		t.Errorf("main displayName = %q, want %q (main unchanged)", cur.DisplayName, "Existing main")
	}
}

func TestMergeBranch_ConflictResolution_UseBranch_AppliesBranchState(t *testing.T) {
	branchBefore := mustMarshal(t, oms.ObjectType{
		RID: "ri.ontology.main.objectType.existing", APIName: "Existing",
		DisplayName: "Existing v1", Status: "ACTIVE",
	})
	branchAfter := mustMarshal(t, oms.ObjectType{
		RID: "ri.ontology.main.objectType.existing", APIName: "Existing",
		DisplayName: "Existing branch wins", Status: "ACTIVE",
	})

	repo := &mockRepo{
		ontologies: []oms.Ontology{{RID: "ri.ontology.main.ontology.1", APIName: "test"}},
		objectTypes: []oms.ObjectType{
			{RID: "ri.ontology.main.objectType.existing", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Existing", DisplayName: "Existing main", Status: "ACTIVE"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/x", Status: "open", BaseVersion: 1},
		},
		branchChanges: []oms.BranchChange{
			{ID: "c1", BranchID: "br-1", ChangeType: "MODIFIED", EntityType: "objectType", EntityRID: "ri.ontology.main.objectType.existing", BeforeState: branchBefore, AfterState: branchAfter},
		},
		ontologyVersion: 2,
	}

	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/merge", handler.MergeBranch)

	body := `{"conflictResolution":{"objectType:Existing":"use-branch"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/branches/br-1/merge", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		AppliedCount int `json:"appliedCount"`
		SkippedCount int `json:"skippedCount"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.AppliedCount != 1 || resp.SkippedCount != 0 {
		t.Errorf("applied=%d skipped=%d, want 1/0", resp.AppliedCount, resp.SkippedCount)
	}

	cur, _ := repo.GetObjectType(req.Context(), "ri.ontology.main.objectType.existing")
	if cur.DisplayName != "Existing branch wins" {
		t.Errorf("main displayName = %q, want %q", cur.DisplayName, "Existing branch wins")
	}
}

func TestMergeBranch_ConflictResolution_UseMain_SkipsBranchChange(t *testing.T) {
	branchBefore := mustMarshal(t, oms.ObjectType{
		RID: "ri.ontology.main.objectType.existing", APIName: "Existing",
		DisplayName: "Existing v1", Status: "ACTIVE",
	})
	branchAfter := mustMarshal(t, oms.ObjectType{
		RID: "ri.ontology.main.objectType.existing", APIName: "Existing",
		DisplayName: "Existing branch should NOT win", Status: "ACTIVE",
	})

	repo := &mockRepo{
		ontologies: []oms.Ontology{{RID: "ri.ontology.main.ontology.1", APIName: "test"}},
		objectTypes: []oms.ObjectType{
			{RID: "ri.ontology.main.objectType.existing", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Existing", DisplayName: "Existing main wins", Status: "ACTIVE"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/x", Status: "open", BaseVersion: 1},
		},
		branchChanges: []oms.BranchChange{
			{ID: "c1", BranchID: "br-1", ChangeType: "MODIFIED", EntityType: "objectType", EntityRID: "ri.ontology.main.objectType.existing", BeforeState: branchBefore, AfterState: branchAfter},
		},
		ontologyVersion: 2,
	}

	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/merge", handler.MergeBranch)

	body := `{"conflictResolution":{"objectType:Existing":"use-main"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/branches/br-1/merge", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		AppliedCount int `json:"appliedCount"`
		SkippedCount int `json:"skippedCount"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.AppliedCount != 0 || resp.SkippedCount != 1 {
		t.Errorf("applied=%d skipped=%d, want 0/1", resp.AppliedCount, resp.SkippedCount)
	}

	cur, _ := repo.GetObjectType(req.Context(), "ri.ontology.main.objectType.existing")
	if cur.DisplayName != "Existing main wins" {
		t.Errorf("main displayName = %q, want %q (main retained)", cur.DisplayName, "Existing main wins")
	}

	// Branch is still merged when conflicts have explicit resolution.
	b, _ := repo.GetBranch(req.Context(), "br-1")
	if b.Status != "merged" {
		t.Errorf("branch status = %q, want %q", b.Status, "merged")
	}
}

func TestMergeBranch_PartialConflictResolution_Returns409(t *testing.T) {
	a := mustMarshal(t, oms.ObjectType{RID: "ri.ontology.main.objectType.a", APIName: "A", DisplayName: "A v1", Status: "ACTIVE"})
	aBranch := mustMarshal(t, oms.ObjectType{RID: "ri.ontology.main.objectType.a", APIName: "A", DisplayName: "A branch", Status: "ACTIVE"})
	b := mustMarshal(t, oms.ObjectType{RID: "ri.ontology.main.objectType.b", APIName: "B", DisplayName: "B v1", Status: "ACTIVE"})
	bBranch := mustMarshal(t, oms.ObjectType{RID: "ri.ontology.main.objectType.b", APIName: "B", DisplayName: "B branch", Status: "ACTIVE"})
	_ = a
	_ = b

	repo := &mockRepo{
		ontologies: []oms.Ontology{{RID: "ri.ontology.main.ontology.1", APIName: "test"}},
		objectTypes: []oms.ObjectType{
			{RID: "ri.ontology.main.objectType.a", OntologyRID: "ri.ontology.main.ontology.1", APIName: "A", DisplayName: "A main", Status: "ACTIVE"},
			{RID: "ri.ontology.main.objectType.b", OntologyRID: "ri.ontology.main.ontology.1", APIName: "B", DisplayName: "B main", Status: "ACTIVE"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/x", Status: "open", BaseVersion: 1},
		},
		branchChanges: []oms.BranchChange{
			{ID: "ca", BranchID: "br-1", ChangeType: "MODIFIED", EntityType: "objectType", EntityRID: "ri.ontology.main.objectType.a", BeforeState: a, AfterState: aBranch},
			{ID: "cb", BranchID: "br-1", ChangeType: "MODIFIED", EntityType: "objectType", EntityRID: "ri.ontology.main.objectType.b", BeforeState: b, AfterState: bBranch},
		},
		ontologyVersion: 2,
	}

	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/merge", handler.MergeBranch)

	// Only A is resolved; B is still unresolved.
	body := `{"conflictResolution":{"objectType:A":"use-branch"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/branches/br-1/merge", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body: %s", w.Code, w.Body.String())
	}

	// Branch must remain open and neither row should be updated.
	br, _ := repo.GetBranch(req.Context(), "br-1")
	if br.Status != "open" {
		t.Errorf("branch status = %q, want %q", br.Status, "open")
	}
	cur, _ := repo.GetObjectType(req.Context(), "ri.ontology.main.objectType.a")
	if cur.DisplayName != "A main" {
		t.Errorf("A displayName = %q, want %q (A must NOT be applied yet)", cur.DisplayName, "A main")
	}
}

func TestMergeBranch_BranchNotOpen_Returns409(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{{RID: "ri.ontology.main.ontology.1", APIName: "test"}},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/x", Status: "merged", BaseVersion: 0},
		},
	}
	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/merge", handler.MergeBranch)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/branches/br-1/merge", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body: %s", w.Code, w.Body.String())
	}
}

func TestMergeBranch_BranchNotFound_Returns404(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{{RID: "ri.ontology.main.ontology.1", APIName: "test"}},
	}
	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/merge", handler.MergeBranch)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/branches/missing/merge", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

func TestMergeBranch_InvalidResolutionValue_Returns400(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{{RID: "ri.ontology.main.ontology.1", APIName: "test"}},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/x", Status: "open", BaseVersion: 0},
		},
	}
	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/merge", handler.MergeBranch)

	body := `{"conflictResolution":{"objectType:Existing":"keep-mine"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/branches/br-1/merge", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

// Sanity helper — mirrors fixture-side jsonEqual for explicit assertions.
func rawEqual(a, b json.RawMessage) bool {
	return jsonBytesEqual(a, b)
}

var _ = rawEqual // silence unused warning if test list shrinks
