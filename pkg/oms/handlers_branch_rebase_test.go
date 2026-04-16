package oms_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// --- RebaseBranch Tests ---

func TestRebaseBranch_Success_AlreadyUpToDate(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/rebase", Status: "open", BaseVersion: 0},
		},
		ontologyVersion: 0,
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/rebase", handler.RebaseBranch)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/branches/br-1/rebase", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	if resp["id"] != "br-1" {
		t.Errorf("id = %v, want %q", resp["id"], "br-1")
	}
	// baseVersion should remain 0
	if bv, ok := resp["baseVersion"].(float64); !ok || int64(bv) != 0 {
		t.Errorf("baseVersion = %v, want 0", resp["baseVersion"])
	}
}

func TestRebaseBranch_Success_NoConflicts(t *testing.T) {
	// Branch at version 0; main at version 2 (added new ObjectType).
	// Branch has a MODIFIED change on an entity that main did NOT modify.
	otJSON := mustMarshal(t, oms.ObjectType{
		RID:         "ri.ontology.main.objectType.existing",
		OntologyRID: "ri.ontology.main.ontology.1",
		APIName:     "Existing",
		DisplayName: "Existing",
		Status:      "ACTIVE",
	})

	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ri.ontology.main.objectType.existing", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Existing", DisplayName: "Existing", Status: "ACTIVE"},
			// Main added this after branch creation
			{RID: "ri.ontology.main.objectType.new1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "NewType", DisplayName: "New Type", Status: "ACTIVE"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/rebase", Status: "open", BaseVersion: 0},
		},
		branchChanges: []oms.BranchChange{
			{
				ID:          "chg-1",
				BranchID:    "br-1",
				ChangeType:  "MODIFIED",
				EntityType:  "objectType",
				EntityRID:   "ri.ontology.main.objectType.existing",
				BeforeState: otJSON,
				AfterState:  mustMarshal(t, oms.ObjectType{RID: "ri.ontology.main.objectType.existing", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Existing", DisplayName: "Existing Modified", Status: "ACTIVE"}),
			},
		},
		ontologyVersion: 2,
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/rebase", handler.RebaseBranch)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/branches/br-1/rebase", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	// baseVersion should be updated to 2
	if bv, ok := resp["baseVersion"].(float64); !ok || int64(bv) != 2 {
		t.Errorf("baseVersion = %v, want 2", resp["baseVersion"])
	}

	// Verify branch was actually updated in repo
	for _, b := range repo.GetBranches() {
		if b.ID == "br-1" {
			if b.BaseVersion != 2 {
				t.Errorf("repo branch BaseVersion = %d, want 2", b.BaseVersion)
			}
		}
	}
}

func TestRebaseBranch_Conflict_SameEntityModified(t *testing.T) {
	// Branch at version 1; main at version 2 (modified same entity as branch).
	// Branch has a MODIFIED change on "Existing" with before_state at V1.
	// Main also modified "Existing" → before_state != current → conflict.
	beforeJSON := mustMarshal(t, oms.ObjectType{
		RID:         "ri.ontology.main.objectType.existing",
		OntologyRID: "ri.ontology.main.ontology.1",
		APIName:     "Existing",
		DisplayName: "Existing V1",
		Status:      "ACTIVE",
	})

	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
		objectTypes: []oms.ObjectType{
			// Current main state — modified since branch creation
			{RID: "ri.ontology.main.objectType.existing", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Existing", DisplayName: "Existing V2 Main", Status: "ACTIVE"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/conflict", Status: "open", BaseVersion: 1},
		},
		branchChanges: []oms.BranchChange{
			{
				ID:          "chg-1",
				BranchID:    "br-1",
				ChangeType:  "MODIFIED",
				EntityType:  "objectType",
				EntityRID:   "ri.ontology.main.objectType.existing",
				BeforeState: beforeJSON,
				AfterState:  mustMarshal(t, oms.ObjectType{RID: "ri.ontology.main.objectType.existing", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Existing", DisplayName: "Existing Branch", Status: "ACTIVE"}),
			},
		},
		ontologyVersion: 2,
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/rebase", handler.RebaseBranch)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/branches/br-1/rebase", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected status 409, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	if resp["errorCode"] != "REBASE_CONFLICT" {
		t.Errorf("errorCode = %v, want %q", resp["errorCode"], "REBASE_CONFLICT")
	}
	conflicts, ok := resp["conflicts"].([]interface{})
	if !ok {
		t.Fatal("expected conflicts to be an array")
	}
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(conflicts))
	}

	conflict := conflicts[0].(map[string]interface{})
	if conflict["entityType"] != "objectType" {
		t.Errorf("conflict entityType = %v, want %q", conflict["entityType"], "objectType")
	}
	if conflict["entityRid"] != "ri.ontology.main.objectType.existing" {
		t.Errorf("conflict entityRid = %v, want %q", conflict["entityRid"], "ri.ontology.main.objectType.existing")
	}

	// Verify base_version was NOT updated (rebase aborted)
	for _, b := range repo.GetBranches() {
		if b.ID == "br-1" {
			if b.BaseVersion != 1 {
				t.Errorf("repo branch BaseVersion = %d, want 1 (should not change on conflict)", b.BaseVersion)
			}
		}
	}
}

func TestRebaseBranch_BranchNotFound(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/rebase", handler.RebaseBranch)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/branches/nonexistent/rebase", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestRebaseBranch_BranchNotOpen(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "closed-branch", Status: "closed", BaseVersion: 0},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/rebase", handler.RebaseBranch)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/branches/br-1/rebase", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected status 409, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestRebaseBranch_MainAddsObjectType_BranchSeesIt(t *testing.T) {
	// Integration-style: main adds ObjectType after branch creation → rebase → branch sees new type
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ri.ontology.main.objectType.original", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Original", DisplayName: "Original", Status: "ACTIVE"},
			// Added on main after branch creation
			{RID: "ri.ontology.main.objectType.new1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "NewType", DisplayName: "New Type", Status: "ACTIVE"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/rebase", Status: "open", BaseVersion: 1},
		},
		branchChanges: []oms.BranchChange{
			// Branch added an unrelated entity
			{
				ID:         "chg-1",
				BranchID:   "br-1",
				ChangeType: "ADDED",
				EntityType: "objectType",
				EntityRID:  "ri.ontology.main.objectType.branchonly",
				AfterState: mustMarshal(t, oms.ObjectType{RID: "ri.ontology.main.objectType.branchonly", APIName: "BranchOnly", DisplayName: "Branch Only", Status: "ACTIVE"}),
			},
		},
		ontologyVersion: 2,
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/rebase", handler.RebaseBranch)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/branches/br-1/rebase", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	// baseVersion should be updated to 2
	if bv, ok := resp["baseVersion"].(float64); !ok || int64(bv) != 2 {
		t.Errorf("baseVersion = %v, want 2", resp["baseVersion"])
	}
}

func TestRebaseBranch_DeletedEntityConflict(t *testing.T) {
	// Branch wants to delete an entity, but main modified it → conflict
	beforeJSON := mustMarshal(t, oms.ObjectType{
		RID:         "ri.ontology.main.objectType.target",
		OntologyRID: "ri.ontology.main.ontology.1",
		APIName:     "Target",
		DisplayName: "Target V1",
		Status:      "ACTIVE",
	})

	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
		objectTypes: []oms.ObjectType{
			// Main modified the entity the branch wants to delete
			{RID: "ri.ontology.main.objectType.target", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Target", DisplayName: "Target V2 Main Modified", Status: "ACTIVE"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/del-conflict", Status: "open", BaseVersion: 1},
		},
		branchChanges: []oms.BranchChange{
			{
				ID:          "chg-1",
				BranchID:    "br-1",
				ChangeType:  "DELETED",
				EntityType:  "objectType",
				EntityRID:   "ri.ontology.main.objectType.target",
				BeforeState: beforeJSON,
			},
		},
		ontologyVersion: 2,
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/rebase", handler.RebaseBranch)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/branches/br-1/rebase", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected status 409, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	conflicts, ok := resp["conflicts"].([]interface{})
	if !ok {
		t.Fatal("expected conflicts array")
	}
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(conflicts))
	}
	conflict := conflicts[0].(map[string]interface{})
	if conflict["changeType"] != "DELETED" {
		t.Errorf("conflict changeType = %v, want %q", conflict["changeType"], "DELETED")
	}
}

func TestRebaseBranch_BeforeStateUpdated(t *testing.T) {
	// Verify that after successful rebase, before_state of MODIFIED changes
	// is updated to reflect current main state.
	otJSON := mustMarshal(t, oms.ObjectType{
		RID:         "ri.ontology.main.objectType.existing",
		OntologyRID: "ri.ontology.main.ontology.1",
		APIName:     "Existing",
		DisplayName: "Existing",
		Status:      "ACTIVE",
	})

	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ri.ontology.main.objectType.existing", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Existing", DisplayName: "Existing", Status: "ACTIVE"},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/rebase", Status: "open", BaseVersion: 0},
		},
		branchChanges: []oms.BranchChange{
			{
				ID:          "chg-1",
				BranchID:    "br-1",
				ChangeType:  "MODIFIED",
				EntityType:  "objectType",
				EntityRID:   "ri.ontology.main.objectType.existing",
				BeforeState: otJSON,
				AfterState:  mustMarshal(t, oms.ObjectType{RID: "ri.ontology.main.objectType.existing", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Existing", DisplayName: "Branch Modified", Status: "ACTIVE"}),
			},
		},
		ontologyVersion: 2,
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/rebase", handler.RebaseBranch)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/branches/br-1/rebase", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Verify before_state was updated in the branch change
	changes := repo.GetBranchChanges()
	for _, c := range changes {
		if c.ID == "chg-1" {
			// Before state should now match the current main entity
			currentJSON := mustMarshal(t, oms.ObjectType{
				RID:         "ri.ontology.main.objectType.existing",
				OntologyRID: "ri.ontology.main.ontology.1",
				APIName:     "Existing",
				DisplayName: "Existing",
				Status:      "ACTIVE",
			})
			if !jsonBytesEqual(c.BeforeState, currentJSON) {
				t.Errorf("before_state not updated to current main state.\ngot:  %s\nwant: %s", string(c.BeforeState), string(currentJSON))
			}
		}
	}
}

// --- Helpers ---

func mustMarshal(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("mustMarshal: %v", err)
	}
	return data
}

func jsonBytesEqual(a, b json.RawMessage) bool {
	var av, bv interface{}
	if err := json.Unmarshal(a, &av); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return false
	}
	return jsonDeepEqual(av, bv)
}

func jsonDeepEqual(a, b interface{}) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return string(aj) == string(bj)
}
