package objectset_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestBDD_ObjectSetExecutor_SplitsUserSideFromServerSide covers
// round 37 of the wire-shape correctness series: the
// pkg/oss/objectset handler's executeError() helper + the
// LoadLinks endpoint at handler_objectset.go:70 previously
// funneled BOTH user-side definition errors (unknown ObjectSet
// type, missing required field, bad where clause) AND server-side
// failures (Bleve outage, policy resolver error) through
// `NewInvalidParameter("ObjectSetFailed", …)` returning HTTP 400.
//
// Round 37 introduces an ErrInvalidObjectSetDefinition sentinel
// (paired with round 36's where.ErrInvalidWhereClause) and routes:
//
//   - ErrQueryTooLarge          → 422 SearchAroundQueryTooLarge (existing)
//   - ErrInvalidObjectSetDef'n  → 400 InvalidObjectSet (NEW user-side)
//   - where.ErrInvalidWhereCl.  → 400 InvalidObjectSet (NEW user-side)
//   - else                      → 500 ObjectSetFailed/LoadLinksFailed
//
// Acceptance criteria (Given → When → Then):
//
//	Given a loadObjects request with an unknown ObjectSet type
//	When  the handler runs
//	Then  it returns HTTP 400 with errorName "InvalidObjectSet"
//	      and reason mentioning the bad type
//
//	Given a loadObjects request whose definition shape is invalid
//	      (e.g. "base" type missing objectType field)
//	When  the handler runs
//	Then  it returns HTTP 400 with errorName "InvalidObjectSet"
//
//	Given a loadObjects request whose base ObjectType has no index
//	      (simulates a server-side Bleve outage — base executor
//	      fails on the index manager call)
//	When  the handler runs
//	Then  it returns HTTP 500 with errorName "ObjectSetFailed"
//	      (was incorrectly 400 before round 37)
//
//	Given a loadLinks request with a filter sub-objectSet missing
//	      the required where clause
//	When  the handler runs
//	Then  it returns HTTP 400 with errorName "InvalidObjectSet"
func TestBDD_ObjectSetExecutor_SplitsUserSideFromServerSide(t *testing.T) {
	t.Run("LoadObjects unknown objectSet type → 400 InvalidObjectSet", func(t *testing.T) {
		handler, _, _ := setupHandlerTest(t)
		r := chi.NewRouter()
		r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjects", handler.LoadObjects)

		// Definition.Validate accepts shapes it doesn't recognize (no
		// case in the validator). The executor's catch-all default
		// case ("unknown objectSet type") rejects it — that's our
		// round-37 sentinel-wrapped path.
		body := `{"objectSet":{"type":"never-heard-of-this"},"select":["id"]}`
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/myOntology/objectSets/loadObjects",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assertExecutorSentinel(t, rec, http.StatusBadRequest, "INVALID_ARGUMENT",
			"InvalidObjectSet", "never-heard-of-this")
	})

	t.Run("LoadObjects missing required field (base.objectType) → 400 InvalidObjectSet", func(t *testing.T) {
		handler, _, _ := setupHandlerTest(t)
		r := chi.NewRouter()
		r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjects", handler.LoadObjects)

		// "base" type requires objectType. Definition.Validate catches
		// the omission — that's our round-37 sentinel-wrapped path via
		// the Validate() branch of Execute().
		body := `{"objectSet":{"type":"base"},"select":["id"]}`
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/myOntology/objectSets/loadObjects",
			bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assertExecutorSentinel(t, rec, http.StatusBadRequest, "INVALID_ARGUMENT",
			"InvalidObjectSet", "requires objectType")
	})

	t.Run("LoadObjects against missing index → 500 ObjectSetFailed", func(t *testing.T) {
		handler, _, _ := setupHandlerTest(t)
		r := chi.NewRouter()
		r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjects", handler.LoadObjects)

		// "ghost" ObjectType has no index in setupHandlerTest's fixture
		// (which only seeds "employee"). The executeBase branch tries to
		// search the missing index and returns a non-sentinel error →
		// must surface as HTTP 500 INTERNAL, not 400.
		body := `{"objectSet":{"type":"base","objectType":"ghost"},"select":["id"]}`
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/myOntology/objectSets/loadObjects",
			bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assertExecutorSentinel(t, rec, http.StatusInternalServerError, "INTERNAL",
			"ObjectSetFailed", "ghost")
	})

	t.Run("LoadLinks with filter base missing where clause → 400 InvalidObjectSet", func(t *testing.T) {
		handler, _, _ := setupHandlerTest(t)
		r := chi.NewRouter()
		r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadLinks", handler.LoadLinks)

		// "filter" type requires both objectSet AND where. Omitting
		// where triggers Definition.Validate → ErrInvalidObjectSet
		// Definition. The LoadLinks handler routes the sentinel to
		// 400 InvalidObjectSet (round-37 wiring).
		body := `{"objectSet":{"type":"filter","objectSet":{"type":"base","objectType":"employee"}},"linkTypeApiName":"employeeDept"}`
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/myOntology/objectSets/loadLinks",
			bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assertExecutorSentinel(t, rec, http.StatusBadRequest, "INVALID_ARGUMENT",
			"InvalidObjectSet", "where")
	})
}

func assertExecutorSentinel(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode, wantName, wantReasonSub string) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, wantStatus, rec.Body.String())
	}
	var env struct {
		ErrorCode  string            `json:"errorCode"`
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&env)
	if env.ErrorCode != wantCode {
		t.Errorf("errorCode = %q, want %q", env.ErrorCode, wantCode)
	}
	if env.ErrorName != wantName {
		t.Errorf("errorName = %q, want %q", env.ErrorName, wantName)
	}
	if wantReasonSub != "" && !strings.Contains(env.Parameters["error"], wantReasonSub) {
		t.Errorf("parameters.error = %q, want it to mention %q", env.Parameters["error"], wantReasonSub)
	}
}
