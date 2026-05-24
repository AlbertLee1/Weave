package oms_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_SharedProperty_DeleteInUseGuard covers the round-54 fix
// for PRD-V2 OMS SharedProperty linkage gap. Before this round,
// DeleteSharedProperty unconditionally executed the DELETE SQL and
// returned 204 even when Properties on existing ObjectTypes still
// carried sharedPropertyRid = X. The orphaned references then
// failed silently at every downstream load (the dangling RID is
// just a string — nothing resolves it back to a deleted row).
// Foundry's contract: a SharedProperty in use returns 409 Conflict
// with the usage count so admins can find consumers before
// retrying.
//
// Wire shape (Foundry parity):
//
//   DELETE /api/admin/shared-properties/{spRID}
//     200/204 + empty body      → spRID was unused, row removed
//     409 SharedPropertyInUse + {sharedPropertyRid, usageCount}
//                                → at least one Property still
//                                  references spRID; delete refused
//
// Scenarios:
//   - Unused SharedProperty deletes cleanly with 200/204.
//   - In-use SharedProperty refuses with 409 + body shape.
//   - After the consuming Property is deleted, retry succeeds.
//   - Repository.CountPropertiesUsingSharedProperty is invoked
//     exactly once on the delete attempt so the guard is observable
//     even on mocks that lack a real PG backend.
func TestBDD_SharedProperty_DeleteInUseGuard(t *testing.T) {
	const ontRID = "ri.ontology.main.ontology.1"
	const spRID = "ri.ontology.main.shared-property.email"
	const otRID = "ri.ontology.main.object-type.user"

	newServer := func(t *testing.T, propertiesUsingShared int) (*chi.Mux, *mockRepo) {
		t.Helper()
		repo := &mockRepo{}
		repo.ontologies = append(repo.ontologies, oms.Ontology{
			RID: ontRID, APIName: "test", DisplayName: "Test",
		})
		repo.sharedProperties = append(repo.sharedProperties,
			oms.SharedProperty{
				RID:         spRID,
				OntologyRID: ontRID,
				APIName:     "email",
				DisplayName: "Email",
				BaseType:    "string",
			},
		)
		for i := 0; i < propertiesUsingShared; i++ {
			repo.properties = append(repo.properties, oms.Property{
				RID:               "ri.property.main.property." + string(rune('a'+i)),
				ObjectTypeRID:     otRID,
				APIName:           "p" + string(rune('a'+i)),
				BaseType:          "string",
				SharedPropertyRID: spRID,
			})
		}
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Delete("/api/admin/shared-properties/{sharedPropertyRid}", handler.DeleteSharedProperty)
		return r, repo
	}

	t.Run("Unused SharedProperty deletes cleanly", func(t *testing.T) {
		r, repo := newServer(t, 0)
		req := httptest.NewRequest(http.MethodDelete,
			"/api/admin/shared-properties/"+spRID, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
			t.Fatalf("status=%d, want 200/204; body=%s", rec.Code, rec.Body.String())
		}
		if repo.countSharedPropertyUsesCalls != 1 {
			t.Errorf("CountPropertiesUsingSharedProperty calls=%d, want 1", repo.countSharedPropertyUsesCalls)
		}
		// Underlying DELETE was reached.
		if len(repo.sharedProperties) != 0 {
			t.Errorf("sharedProperties=%v, want empty after delete", repo.sharedProperties)
		}
	})

	t.Run("In-use SharedProperty returns 409 with usageCount", func(t *testing.T) {
		r, repo := newServer(t, 2)
		req := httptest.NewRequest(http.MethodDelete,
			"/api/admin/shared-properties/"+spRID, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusConflict {
			t.Fatalf("status=%d, want 409; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]interface{}
		if err := json.NewDecoder(bytes.NewReader(rec.Body.Bytes())).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["errorCode"] != "CONFLICT" {
			t.Errorf("errorCode=%v, want CONFLICT", body["errorCode"])
		}
		if body["errorName"] != "SharedPropertyInUse" {
			t.Errorf("errorName=%v, want SharedPropertyInUse", body["errorName"])
		}
		params, ok := body["parameters"].(map[string]interface{})
		if !ok {
			t.Fatalf("parameters missing/wrong shape: %v", body["parameters"])
		}
		if params["sharedPropertyRid"] != spRID {
			t.Errorf("parameters.sharedPropertyRid=%v, want %s", params["sharedPropertyRid"], spRID)
		}
		// Allow either numeric or string-encoded count for SDK ergonomics.
		switch v := params["usageCount"].(type) {
		case float64:
			if int(v) != 2 {
				t.Errorf("usageCount=%v, want 2", v)
			}
		case string:
			if v != "2" {
				t.Errorf("usageCount=%v, want \"2\"", v)
			}
		default:
			t.Errorf("usageCount missing/wrong type: %v", params["usageCount"])
		}
		// Row must remain.
		if len(repo.sharedProperties) != 1 {
			t.Errorf("sharedProperties=%v, want preserved on 409", repo.sharedProperties)
		}
	})

	t.Run("After consumers gone, retry deletes cleanly", func(t *testing.T) {
		r, repo := newServer(t, 1)
		// First call: 409.
		req1 := httptest.NewRequest(http.MethodDelete,
			"/api/admin/shared-properties/"+spRID, nil)
		rec1 := httptest.NewRecorder()
		r.ServeHTTP(rec1, req1)
		if rec1.Code != http.StatusConflict {
			t.Fatalf("first delete status=%d, want 409", rec1.Code)
		}
		// Drop the consuming property.
		repo.properties = nil
		// Second call: clean delete.
		req2 := httptest.NewRequest(http.MethodDelete,
			"/api/admin/shared-properties/"+spRID, nil)
		rec2 := httptest.NewRecorder()
		r.ServeHTTP(rec2, req2)
		if rec2.Code != http.StatusOK && rec2.Code != http.StatusNoContent {
			t.Fatalf("second delete status=%d, want 200/204; body=%s", rec2.Code, rec2.Body.String())
		}
		if len(repo.sharedProperties) != 0 {
			t.Errorf("sharedProperties=%v, want removed", repo.sharedProperties)
		}
	})

	t.Run("Count method invoked exactly once per request", func(t *testing.T) {
		r, repo := newServer(t, 5)
		req := httptest.NewRequest(http.MethodDelete,
			"/api/admin/shared-properties/"+spRID, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		_ = rec.Code
		if repo.countSharedPropertyUsesCalls != 1 {
			t.Errorf("CountPropertiesUsingSharedProperty calls=%d, want 1 (handler should check once before DELETE)",
				repo.countSharedPropertyUsesCalls)
		}
	})
}

// Tiny static check: mockRepo must satisfy oms.Repository after the
// new method is added — surfaces the "9 mock files" drift at compile
// time so we don't ship a half-updated mock by accident.
var _ oms.Repository = (*mockRepo)(nil)

// Compile-time use of context so this file doesn't drift its imports
// if the BDD body is later refactored to drop the explicit ctx.
var _ = context.Background
