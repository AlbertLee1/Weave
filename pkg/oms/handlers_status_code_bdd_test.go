package oms_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_OMSHandlers_InternalErrorsReturnHTTP500 covers a
// systematic wire-shape correctness bug in pkg/oms/handlers.go:
// 13 call sites used `apierror.NewNotFound(…Failed, nil)` for
// downstream DB / serialization errors. That returned HTTP 404
// with errorCode "NOT_FOUND" — misrouting SDKs to their "empty
// state" path (the row doesn't exist; render placeholder) when
// the actual condition required "retry / escalate to oncall"
// (the server screwed up). Foundry's wire contract is that
// non-existent entities surface as 404 NOT_FOUND while
// server-side failures surface as 500 INTERNAL; conflating the
// two is a real SDK-breaking bug because every Foundry-aligned
// SDK has separate error-handling branches keyed off
// errorCode.
//
// Each of the 13 sites sits in the "non-ErrNotFound" branch of
// an error-classification ladder where the real ErrNotFound case
// was already handled correctly above. So the fix is purely a
// constructor swap: NewNotFound → NewInternal.
//
// Round 24's BDD covers the four highest-impact entry points:
//
//   - GET /api/v2/ontologies                  ListOntologies
//   - GET /api/v2/ontologies/{ontologyApiName} GetOntology
//   - GET /api/v2/ontologies/{ontologyApiName}/objectTypes
//                                             ListObjectTypes
//   - GET /api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}
//                                             GetObjectType
//
// Each injects a generic (non-ErrNotFound) error into the mock
// repo and asserts HTTP 500 + errorCode INTERNAL. The remaining
// 9 sites are similar shape (Same File, Same Pattern) and ship
// with the fix in the commit; promoting the lighter sites to
// BDD would just duplicate the pattern.
func TestBDD_OMSHandlers_InternalErrorsReturnHTTP500(t *testing.T) {
	t.Run("ListOntologies repo error returns HTTP 500 INTERNAL", func(t *testing.T) {
		repo := &mockRepo{listErr: errors.New("postgres: connection lost")}
		h := oms.NewOMSHandler(repo)

		req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies", nil)
		rec := httptest.NewRecorder()
		h.ListOntologies(rec, req)
		assertOMSInternalError(t, rec, "ListOntologiesFailed")
	})

	t.Run("GetOntology repo error returns HTTP 500 INTERNAL", func(t *testing.T) {
		repo := &mockRepo{getErr: errors.New("postgres: deadlock detected")}
		// Seed an ontology so getOntologyByAPIName resolves; the getErr
		// triggers on a downstream GetOntology call.
		repo.ontologies = append(repo.ontologies, oms.Ontology{
			RID: "ri.ontology.main.ontology.1", APIName: "test",
		})
		h := oms.NewOMSHandler(repo)

		r := chi.NewRouter()
		r.Get("/api/v2/ontologies/{ontologyApiName}", h.GetOntology)
		req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		assertOMSInternalError(t, rec, "GetOntologyFailed")
	})

	t.Run("ListObjectTypes repo error returns HTTP 500 INTERNAL", func(t *testing.T) {
		repo := &mockRepo{listErr: errors.New("postgres: connection reset")}
		repo.ontologies = append(repo.ontologies, oms.Ontology{
			RID: "ri.ontology.main.ontology.1", APIName: "test",
		})
		h := oms.NewOMSHandler(repo)

		r := chi.NewRouter()
		r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes", h.ListObjectTypes)
		req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test/objectTypes", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		assertOMSInternalError(t, rec, "ListObjectTypesFailed")
	})

	t.Run("GetObjectType repo error (non-NotFound) returns HTTP 500 INTERNAL", func(t *testing.T) {
		repo := &mockRepo{getErr: errors.New("postgres: query cancelled")}
		repo.ontologies = append(repo.ontologies, oms.Ontology{
			RID: "ri.ontology.main.ontology.1", APIName: "test",
		})
		h := oms.NewOMSHandler(repo)

		r := chi.NewRouter()
		r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}", h.GetObjectType)
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/test/objectTypes/Employee", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		assertOMSInternalError(t, rec, "GetObjectTypeFailed")
	})

	t.Run("GetObjectType genuine NotFound still returns HTTP 404 (regression guard)", func(t *testing.T) {
		// Critical sanity check: the existing 404 path for an
		// actually-missing ObjectType MUST stay 404 NOT_FOUND. Only the
		// "non-ErrNotFound" downstream-error branch is being changed.
		repo := &mockRepo{}
		repo.ontologies = append(repo.ontologies, oms.Ontology{
			RID: "ri.ontology.main.ontology.1", APIName: "test",
		})
		h := oms.NewOMSHandler(repo)

		r := chi.NewRouter()
		r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}", h.GetObjectType)
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/test/objectTypes/Nonexistent", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("genuine NotFound: status=%d, want 404; body=%s", rec.Code, rec.Body.String())
		}
		var env struct {
			ErrorCode string `json:"errorCode"`
			ErrorName string `json:"errorName"`
		}
		_ = json.NewDecoder(rec.Body).Decode(&env)
		if env.ErrorCode != "NOT_FOUND" {
			t.Errorf("errorCode: got %q, want NOT_FOUND", env.ErrorCode)
		}
		if env.ErrorName != "ObjectTypeNotFound" {
			t.Errorf("errorName: got %q, want ObjectTypeNotFound", env.ErrorName)
		}
	})
}

func assertOMSInternalError(t *testing.T, rec *httptest.ResponseRecorder, wantErrorName string) {
	t.Helper()
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want 500; body=%s", rec.Code, rec.Body.String())
	}
	var env struct {
		ErrorCode string `json:"errorCode"`
		ErrorName string `json:"errorName"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&env)
	if env.ErrorCode != "INTERNAL" {
		t.Errorf("errorCode: got %q, want INTERNAL (404 NOT_FOUND would mislead SDK to render an 'empty state' instead of retrying)", env.ErrorCode)
	}
	if env.ErrorName != wantErrorName {
		t.Errorf("errorName: got %q, want %q", env.ErrorName, wantErrorName)
	}
}
