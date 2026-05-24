package oss_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
)

// failingOSSService is a one-shot test double whose every entry point
// returns the configured error. Used to drive the "non-ErrNotFound"
// branch of the OSS HTTP handlers' error ladder so the BDD scenarios
// can assert HTTP 500 INTERNAL. Each call increments the counter so a
// scenario can verify the handler reached the service layer instead of
// short-circuiting on URL validation.
type failingOSSService struct {
	err   error
	calls int
}

func (f *failingOSSService) GetObject(_ context.Context, _ oss.GetObjectRequest) (*oss.WireObject, error) {
	f.calls++
	return nil, f.err
}

func (f *failingOSSService) ListObjects(_ context.Context, _ oss.ListObjectsRequest) (*oss.ObjectPage, error) {
	f.calls++
	return nil, f.err
}

func (f *failingOSSService) SearchObjects(_ context.Context, _ oss.SearchObjectsRequest) (*oss.ObjectPage, error) {
	f.calls++
	return nil, f.err
}

func (f *failingOSSService) ListLinkedObjects(_ context.Context, _ oss.LinkedObjectsRequest) (*oss.ObjectPage, error) {
	f.calls++
	return nil, f.err
}

func (f *failingOSSService) GetLinkedObject(_ context.Context, _ oss.GetLinkedObjectRequest) (*oss.WireObject, error) {
	f.calls++
	return nil, f.err
}

func (f *failingOSSService) CountObjects(_ context.Context, _ oss.CountObjectsRequest) (*oss.CountObjectsResponse, error) {
	f.calls++
	return nil, f.err
}

var _ oss.Service = (*failingOSSService)(nil)

// TestBDD_OSSHandlers_DownstreamServiceErrorsReturnHTTP500 covers a
// systematic wire-shape correctness bug in pkg/oss/handlers*.go: 12
// call sites used `apierror.NewInvalidParameter(\"…Failed\", …)` for
// the "service layer returned a non-ErrNotFound error" branch — i.e.
// PG / Bleve / OMS-repo outage. That returned HTTP 400 with errorCode
// INVALID_ARGUMENT — telling Foundry SDK clients "you sent a bad
// request, fix your input" when the caller's input (PK from URL path)
// was already validated and the actual failure is server-side.
//
// Per Foundry's wire contract, downstream service failures surface as
// HTTP 500 INTERNAL so SDKs route to retry / oncall rather than the
// user-side "fix your request" branch. Round 24 fixed the equivalent
// pattern in pkg/oms (which had been miscoded as NewNotFound); round
// 25 fixed pkg/oss/objectset; this round closes pkg/oss/handlers*.go.
//
// Sites fixed (file:line pre-commit):
//
//	handlers.go:196   GetObjectFailed
//	handlers.go:256   ListObjectsFailed
//	handlers.go:586   GetLinkedObjectFailed
//	handlers.go:637   ListLinkedObjectsFailed
//	handlers_geotemporal.go:81           GetObjectFailed
//	handlers_cipher.go:52                GetObjectFailed
//	handlers_attachment_property.go:100  GetObjectFailed
//	handlers_timeseries.go:150           GetObjectFailed
//	handlers_timeseries_transform.go:94  GetObjectFailed
//	handlers_media_property.go:172       GetObjectFailed
//	handlers_activity.go:66              ListActivityFailed
//	handlers_interface.go:205            InterfaceLinkedObjectsFailed
//
// Sites NOT changed in this round (mixed user-input / server-error
// cases — need sentinel error types in the service layer to safely
// disambiguate; deferred):
//
//	handlers.go:434   SearchObjectsFailed  (regex test enshrines 400)
//	handlers.go:552   CountObjectsFailed   (mirrors SearchObjects)
//	handlers_aggregate.go:250   ScenarioAggregationFailed
//	                                       (BDD test enshrines 400)
//
// This BDD covers the three highest-leverage paths in handlers.go via
// failingOSSService and an ErrNotFound regression guard. The remaining
// 9 same-pattern sites ship with the same one-line fix.
func TestBDD_OSSHandlers_DownstreamServiceErrorsReturnHTTP500(t *testing.T) {
	t.Run("GetObject service error returns HTTP 500 INTERNAL", func(t *testing.T) {
		svc := &failingOSSService{err: errors.New("postgres: connection lost")}
		router := newTestRouter(svc, nil)

		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/myOntology/objects/Employee/E001", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertOSSInternalError(t, rec, "GetObjectFailed", "postgres: connection lost")
		if svc.calls != 1 {
			t.Errorf("service should have been called once; got %d", svc.calls)
		}
	})

	t.Run("ListObjects service error returns HTTP 500 INTERNAL", func(t *testing.T) {
		svc := &failingOSSService{err: errors.New("bleve: index unavailable")}
		router := newTestRouter(svc, nil)

		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/myOntology/objects/Employee", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertOSSInternalError(t, rec, "ListObjectsFailed", "bleve: index unavailable")
	})

	t.Run("GetLinkedObject service error returns HTTP 500 INTERNAL", func(t *testing.T) {
		svc := &failingOSSService{err: errors.New("postgres: deadlock detected")}
		router := newTestRouter(svc, nil)

		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/myOntology/objects/Employee/E001/links/manager/M001", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertOSSInternalError(t, rec, "GetLinkedObjectFailed", "postgres: deadlock detected")
	})

	t.Run("ListLinkedObjects service error returns HTTP 500 INTERNAL", func(t *testing.T) {
		svc := &failingOSSService{err: errors.New("bleve: timeout")}
		router := newTestRouter(svc, nil)

		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/myOntology/objects/Employee/E001/links/reports", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertOSSInternalError(t, rec, "ListLinkedObjectsFailed", "bleve: timeout")
	})

	t.Run("GetObject genuine ErrNotFound regression guard still returns HTTP 404", func(t *testing.T) {
		// Critical sanity check: the existing ErrNotFound branch MUST
		// continue returning HTTP 404 NOT_FOUND for ObjectNotFound when
		// the resource is genuinely missing. Only the non-ErrNotFound
		// downstream-error branch is being changed.
		svc := &failingOSSService{err: oms.ErrNotFound}
		router := newTestRouter(svc, nil)

		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/myOntology/objects/Employee/E001", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("genuine ErrNotFound: status = %d, want 404; body = %s", rec.Code, rec.Body.String())
		}
		var env struct {
			ErrorCode string `json:"errorCode"`
			ErrorName string `json:"errorName"`
		}
		_ = json.NewDecoder(rec.Body).Decode(&env)
		if env.ErrorCode != "NOT_FOUND" {
			t.Errorf("errorCode = %q, want NOT_FOUND", env.ErrorCode)
		}
		if env.ErrorName != "ObjectNotFound" {
			t.Errorf("errorName = %q, want ObjectNotFound", env.ErrorName)
		}
	})
}

func assertOSSInternalError(t *testing.T, rec *httptest.ResponseRecorder, wantErrorName, wantReasonSubstring string) {
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
		t.Errorf("errorCode = %q, want INTERNAL (400 INVALID_ARGUMENT would mislead the SDK into a 'fix your input' branch when the caller's input is fine and the server's downstream dependency failed)", env.ErrorCode)
	}
	if env.ErrorName != wantErrorName {
		t.Errorf("errorName = %q, want %q", env.ErrorName, wantErrorName)
	}
	if wantReasonSubstring != "" {
		got := env.Parameters["reason"]
		if got == "" || !contains(got, wantReasonSubstring) {
			t.Errorf("parameters.reason = %q, want it to mention %q", got, wantReasonSubstring)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
