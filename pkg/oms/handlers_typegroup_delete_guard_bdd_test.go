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

// TestBDD_TypeGroup_DeleteInUseGuard covers the round-58 fix for
// the TypeGroup delete-in-use gap. Round 54 closed the same hole
// for SharedProperty; this round mirrors it for TypeGroup. Before
// this round DeleteTypeGroup unconditionally executed the DELETE
// SQL even when ObjectTypes were still assigned via the
// object_type_groups join table — the assignments rows then
// dangled with a non-resolvable typeGroupRid, and ListTypeGroups
// ForObjectType happily returned them as ghost references.
// Foundry's parity contract: refuse 409 Conflict on a TypeGroup
// with current assignments, with the usage count surfaced.
//
// Wire shape:
//
//   DELETE /api/admin/type-groups/{tgRID}
//     204 + empty body                  → was unused, row removed
//     409 TypeGroupInUse + {typeGroupRid, usageCount}
//                                       → at least one ObjectType
//                                         still assigned; refused
//
// Scenarios:
//   - Unused TypeGroup deletes cleanly with 204.
//   - In-use TypeGroup refuses 409 + body shape (typeGroupRid +
//     usageCount).
//   - After consumers cleared, retry succeeds.
//   - Repository.CountObjectTypesInTypeGroup invoked exactly once
//     per request so the guard adds at most one DB query.
func TestBDD_TypeGroup_DeleteInUseGuard(t *testing.T) {
	const ontRID = "ri.ontology.main.ontology.1"
	const tgRID = "ri.ontology.main.type-group.people"

	newServer := func(t *testing.T, assignedCount int) (*chi.Mux, *mockRepo) {
		t.Helper()
		repo := &mockRepo{}
		repo.ontologies = append(repo.ontologies, oms.Ontology{
			RID: ontRID, APIName: "test", DisplayName: "Test",
		})
		repo.typeGroups = append(repo.typeGroups, oms.TypeGroup{
			RID:         tgRID,
			OntologyRID: ontRID,
			APIName:     "people",
			DisplayName: "People",
		})
		repo.typeGroupAssignments = map[string][]string{}
		for i := 0; i < assignedCount; i++ {
			otRID := "ri.ontology.main.object-type." + string(rune('a'+i))
			repo.typeGroupAssignments[otRID] = []string{tgRID}
		}
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Delete("/api/admin/type-groups/{typeGroupRid}", handler.DeleteTypeGroup)
		return r, repo
	}

	doDelete := func(t *testing.T, r *chi.Mux) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodDelete,
			"/api/admin/type-groups/"+tgRID, bytes.NewReader(nil))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	t.Run("Unused TypeGroup deletes cleanly", func(t *testing.T) {
		r, repo := newServer(t, 0)
		rec := doDelete(t, r)
		if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
			t.Fatalf("status=%d, want 200/204; body=%s", rec.Code, rec.Body.String())
		}
		if repo.countTypeGroupAssignmentsCalls != 1 {
			t.Errorf("CountObjectTypesInTypeGroup calls=%d, want 1", repo.countTypeGroupAssignmentsCalls)
		}
		if len(repo.typeGroups) != 0 {
			t.Errorf("typeGroups=%v, want empty after delete", repo.typeGroups)
		}
	})

	t.Run("In-use TypeGroup returns 409 TypeGroupInUse with usageCount", func(t *testing.T) {
		r, repo := newServer(t, 3)
		rec := doDelete(t, r)
		if rec.Code != http.StatusConflict {
			t.Fatalf("status=%d, want 409; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&body)
		if body["errorName"] != "TypeGroupInUse" {
			t.Errorf("errorName=%v, want TypeGroupInUse", body["errorName"])
		}
		params, _ := body["parameters"].(map[string]interface{})
		if params["typeGroupRid"] != tgRID {
			t.Errorf("parameters.typeGroupRid=%v, want %s", params["typeGroupRid"], tgRID)
		}
		switch v := params["usageCount"].(type) {
		case float64:
			if int(v) != 3 {
				t.Errorf("usageCount=%v, want 3", v)
			}
		case string:
			if v != "3" {
				t.Errorf("usageCount=%v, want \"3\"", v)
			}
		default:
			t.Errorf("usageCount missing/wrong type: %v", params["usageCount"])
		}
		if len(repo.typeGroups) != 1 {
			t.Errorf("typeGroups=%v, want preserved on 409", repo.typeGroups)
		}
	})

	t.Run("After assignments cleared, retry deletes cleanly", func(t *testing.T) {
		r, repo := newServer(t, 1)
		rec1 := doDelete(t, r)
		if rec1.Code != http.StatusConflict {
			t.Fatalf("first delete status=%d, want 409", rec1.Code)
		}
		// Clear assignments.
		repo.typeGroupAssignments = map[string][]string{}
		rec2 := doDelete(t, r)
		if rec2.Code != http.StatusOK && rec2.Code != http.StatusNoContent {
			t.Fatalf("second delete status=%d, want 200/204; body=%s", rec2.Code, rec2.Body.String())
		}
		if len(repo.typeGroups) != 0 {
			t.Errorf("typeGroups=%v, want removed", repo.typeGroups)
		}
	})

	t.Run("Count method invoked exactly once per request", func(t *testing.T) {
		r, repo := newServer(t, 5)
		rec := doDelete(t, r)
		_ = rec.Code
		if repo.countTypeGroupAssignmentsCalls != 1 {
			t.Errorf("CountObjectTypesInTypeGroup calls=%d, want 1 (guard runs once before DELETE)",
				repo.countTypeGroupAssignmentsCalls)
		}
	})
}
