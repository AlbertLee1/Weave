package oms_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

func TestGetBranchBreakingChanges_HappyPath(t *testing.T) {
	repo := setupBaseRepo()
	// Branch deletes the fullName property.
	repo.branchChanges = []oms.BranchChange{
		{
			ID: "bc-h1", BranchID: "br-1", ChangeType: "DELETED", EntityType: "property", EntityRID: "p-name",
			BeforeState: mustJSON(t, repo.properties[0]),
		},
	}
	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/breaking-changes", handler.GetBranchBreakingChanges)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test/branches/br-1/breaking-changes", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp oms.BreakingChangesReport
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v: %s", err, w.Body.String())
	}
	if resp.BranchID != "br-1" {
		t.Errorf("expected branchId=br-1, got %s", resp.BranchID)
	}
	c := findChange(resp, oms.BreakingChangeKindPropertyDeleted, "fullName")
	if c == nil {
		t.Fatalf("expected PROPERTY_DELETED entry, got %+v", resp)
	}
}

func TestGetBranchBreakingChanges_BranchNotFound(t *testing.T) {
	repo := setupBaseRepo()
	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/breaking-changes", handler.GetBranchBreakingChanges)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test/branches/missing/breaking-changes", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetBranchBreakingChanges_OntologyNotFound(t *testing.T) {
	repo := setupBaseRepo()
	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/breaking-changes", handler.GetBranchBreakingChanges)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/missing/branches/br-1/breaking-changes", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetBranchBreakingChanges_NoChanges_EmptyReport(t *testing.T) {
	repo := setupBaseRepo()
	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/breaking-changes", handler.GetBranchBreakingChanges)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test/branches/br-1/breaking-changes", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp oms.BreakingChangesReport
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v: %s", err, w.Body.String())
	}
	if resp.BranchID != "br-1" {
		t.Errorf("expected branchId=br-1, got %s", resp.BranchID)
	}
	if len(resp.Changes) != 0 {
		t.Errorf("expected 0 changes, got %d", len(resp.Changes))
	}
}

func TestGetBranchBreakingChanges_WithSavedSetsLister(t *testing.T) {
	repo := setupBaseRepo()
	repo.branchChanges = []oms.BranchChange{
		{
			ID: "bc-h2", BranchID: "br-1", ChangeType: "DELETED", EntityType: "property", EntityRID: "p-name",
			BeforeState: mustJSON(t, repo.properties[0]),
		},
	}
	def := mustJSON(t, map[string]interface{}{
		"type": "filter",
		"objectSet": map[string]interface{}{
			"type":       "base",
			"objectType": "employee",
		},
		"where": map[string]interface{}{
			"type":  "eq",
			"field": "fullName",
			"value": "Alice",
		},
	})
	lister := &stubSavedObjectSetLister{
		sets: []oms.SavedObjectSetRef{{ID: "sos-h", Definition: def}},
	}
	handler := oms.NewOMSHandler(repo)
	handler.SetSavedObjectSetLister(lister)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/breaking-changes", handler.GetBranchBreakingChanges)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test/branches/br-1/breaking-changes", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp oms.BreakingChangesReport
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	c := findChange(resp, oms.BreakingChangeKindPropertyDeleted, "fullName")
	if c == nil {
		t.Fatalf("expected PROPERTY_DELETED, got %+v", resp)
	}
	if len(c.AffectedSavedObjectSets) != 1 || c.AffectedSavedObjectSets[0] != "sos-h" {
		t.Errorf("expected AffectedSavedObjectSets=[sos-h], got %v", c.AffectedSavedObjectSets)
	}
}

// Sanity check that the lister type contract works with context plumbing.
func TestSavedObjectSetLister_ContextPlumbing(t *testing.T) {
	lister := &stubSavedObjectSetLister{}
	if _, err := lister.ListSavedObjectSets(context.Background(), "any"); err != nil {
		t.Fatal(err)
	}
}
