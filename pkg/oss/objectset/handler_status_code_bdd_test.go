package objectset_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/oss/objectset"
)

// fakePropertyFilterProvider is a one-shot test double whose
// AllowedProperties always returns the configured error. Used to drive
// the PropertyFilterFailed branch from applyPropertyVisibility on both
// the live and time-travel paths.
type fakePropertyFilterProvider struct {
	err error
}

func (f *fakePropertyFilterProvider) AllowedProperties(_ context.Context, _ string) ([]string, error) {
	return nil, f.err
}

// TestBDD_ObjectSetHandler_DownstreamErrorsReturnHTTP500 covers a
// systematic wire-shape correctness bug in pkg/oss/objectset/handler.go:
// 7 call sites used `apierror.NewInvalidParameter(\"…Failed\", …)` for
// what were actually downstream provider / engine failures. That
// returned HTTP 400 with errorCode INVALID_ARGUMENT — telling Foundry
// SDK clients "you sent a bad request, fix your input" when in reality
// the caller's input was fine and the server's downstream dependency
// (policy provider, branch overlay store, history-snapshot reader,
// transaction resolver, aggregation engine) failed. Three pre-existing
// tests (TestBranchScope_PropagatesProviderError,
// TestLoadObjects_AsOf_PropagatesProviderError,
// TestLoadObjects_AsOfTx_PropagatesResolverError) enshrined the bug
// with editorial comments stating "we want this visible without leaking
// the 404 code"; HTTP 500 INTERNAL satisfies that reasoning while also
// matching Foundry's wire contract that server-side failures surface as
// 500 INTERNAL.
//
// The fix swaps `NewInvalidParameter` → `NewInternal` at:
//
//	376  applyPropertyVisibility failure (live LoadObjects path)
//	445  branchScopeError helper (non-sentinel BranchScopeProvider error)
//	481  resolveAsOf helper      (non-sentinel TransactionResolver error)
//	527  loadObjectsAsOf         (non-nil HistorySnapshotProvider error)
//	615  applyPropertyVisibility failure (asOf path)
//	687  aggregateWithDerived    (derived-aggregation engine error)
//	721  Aggregate (Bleve path)  (aggregation engine error)
//
// Each site sits in the "non-sentinel error" branch of an
// error-classification ladder — the sentinel cases (ErrBranchNotFound,
// ErrTransactionNotFound, ErrQueryTooLarge) keep their specific
// envelopes. The L200 `executeError` site is intentionally left as
// INVALID_ARGUMENT for now because executor errors include both
// user-input issues (bad filter expressions) and server-side issues
// (PG/Bleve outage) — a proper taxonomy needs sentinel error types in
// the executor, not a single-line swap.
//
// This BDD covers the three highest-leverage paths via the providers
// that already have testable injection points (PropertyFilter, Branch,
// History). The remaining four sites ship with the same fix and the
// same pattern. The "PropertyFilter on the live path" sub-scenario
// also implicitly validates the asOf-path PropertyFilter fix because
// they share the helper function.
func TestBDD_ObjectSetHandler_DownstreamErrorsReturnHTTP500(t *testing.T) {
	t.Run("PropertyFilterProvider error on live path returns HTTP 500 INTERNAL", func(t *testing.T) {
		handler, _, _ := setupHandlerTest(t)
		filter := &fakePropertyFilterProvider{err: errors.New("policy backend unreachable")}
		handler.SetPropertyFilterProvider(filter)

		body := `{"objectSet":{"type":"base","objectType":"employee"},"select":["id","name"]}`
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/myOntology/objectSets/loadObjects",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		newAsOfRouter(t, handler).ServeHTTP(rec, req)

		assertObjectSetInternalError(t, rec, "PropertyFilterFailed", "policy backend unreachable")
	})

	t.Run("BranchScopeProvider error returns HTTP 500 INTERNAL", func(t *testing.T) {
		handler, _, _ := setupHandlerTest(t)
		prov := newFakeBranchScopeProvider()
		prov.err = errors.New("overlay store unreachable")
		handler.SetBranchScopeProvider(prov)

		body := `{"objectSet":{"type":"base","objectType":"employee"},"select":["id","name"]}`
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/myOntology/objectSets/loadObjects?branch=feature-x",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		newAsOfRouter(t, handler).ServeHTTP(rec, req)

		assertObjectSetInternalError(t, rec, "BranchScopeFailed", "overlay store unreachable")
	})

	t.Run("HistorySnapshotProvider error on asOf path returns HTTP 500 INTERNAL", func(t *testing.T) {
		prov := newFakeSnapshotProvider()
		prov.err = errors.New("history backend unreachable")
		store := objectset.NewStore(0)
		h := objectset.NewHandler(nil, nil, store)
		h.SetHistorySnapshotProvider(prov)

		body := `{"objectSet":{"type":"base","objectType":"Employee"},"select":["name"]}`
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/test/objectSets/loadObjects?asOf=2026-01-15T00:00:00Z",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		newAsOfRouter(t, h).ServeHTTP(rec, req)

		assertObjectSetInternalError(t, rec, "TimeTravelFailed", "history backend unreachable")
	})

	t.Run("TransactionResolver error on tx-asOf path returns HTTP 500 INTERNAL", func(t *testing.T) {
		prov := newFakeSnapshotProvider()
		tx := newFakeTxResolver()
		tx.err = errors.New("resolver unreachable")
		store := objectset.NewStore(0)
		h := objectset.NewHandler(nil, nil, store)
		h.SetHistorySnapshotProvider(prov)
		h.SetTransactionResolver(tx)

		body := `{"objectSet":{"type":"base","objectType":"Employee"},"select":["name"]}`
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/test/objectSets/loadObjects?asOf=tx-001",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		newAsOfRouter(t, h).ServeHTTP(rec, req)

		assertObjectSetInternalError(t, rec, "TimeTravelFailed", "resolver unreachable")
	})

	t.Run("ErrBranchNotFound regression guard: sentinel still surfaces as user-facing 400 BranchNotFound", func(t *testing.T) {
		// Sentinel-NotFound preserves its existing envelope (the
		// resource genuinely doesn't exist; user must pick a valid
		// branch). Only the non-sentinel error branch changes to 500.
		handler, _, _ := setupHandlerTest(t)
		prov := newFakeBranchScopeProvider()
		prov.notFound = map[string]bool{"ghost": true}
		handler.SetBranchScopeProvider(prov)

		body := `{"objectSet":{"type":"base","objectType":"employee"},"select":["id","name"]}`
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/myOntology/objectSets/loadObjects?branch=ghost",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		newAsOfRouter(t, handler).ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (sentinel BranchNotFound stays user-facing); body = %s", rec.Code, rec.Body.String())
		}
		env := decodeJSON[struct {
			ErrorName string `json:"errorName"`
		}](t, rec.Body.Bytes())
		if env.ErrorName != "BranchNotFound" {
			t.Errorf("errorName = %q, want BranchNotFound (sentinel preserved)", env.ErrorName)
		}
	})
}

func assertObjectSetInternalError(t *testing.T, rec *httptest.ResponseRecorder, wantErrorName, wantErrorSubstring string) {
	t.Helper()
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body = %s", rec.Code, rec.Body.String())
	}
	var env struct {
		ErrorCode  string            `json:"errorCode"`
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&env)
	if env.ErrorCode != "INTERNAL" {
		t.Errorf("errorCode = %q, want INTERNAL (400 INVALID_ARGUMENT would mislead the SDK to surface a 'fix your input' message when the caller's input is fine and the server's downstream dependency failed)", env.ErrorCode)
	}
	if env.ErrorName != wantErrorName {
		t.Errorf("errorName = %q, want %q", env.ErrorName, wantErrorName)
	}
	// Parameters carry the underlying error string so oncall can trace
	// the originating downstream failure.
	if wantErrorSubstring != "" {
		gotErr := env.Parameters["error"]
		if !strings.Contains(gotErr, wantErrorSubstring) {
			t.Errorf("parameters.error = %q, want it to mention %q", gotErr, wantErrorSubstring)
		}
	}
}
