package main

// US-005: Delete inline aggregate closure in main.go.
//
// The per-object-type aggregate route
//   POST /api/v2/ontologies/{ontologyApiName}/objects/{objectType}/aggregate
// must be registered through the OSS handler's RegisterRoutes, not via an
// inline closure in NewFullRouter. The OSS handler receives the aggregation
// engine and index manager through a setter (SetAggregation).

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oss/aggregation"
)

// TestUS005_InlineClosureRemoved verifies that when AggEngine and IndexMgr
// are wired but OssSvc is nil, the aggregate route is NOT registered. In the
// old code, an inline closure in NewFullRouter checked only AggEngine &&
// IndexMgr. After US-005, the route is only registered through the OSS
// handler (which requires OssSvc), so this must return chi's plain-text 404
// (NOT a JSON error from the handler).
func TestUS005_InlineClosureRemoved(t *testing.T) {
	deps := &ServerDeps{
		AggEngine: aggregation.NewEngine(),
		IndexMgr:  index.NewManager(t.TempDir()),
		// OssSvc intentionally nil — old inline closure would still register
	}
	router := NewFullRouter(deps)

	body := `{"aggregation":[{"type":"count"}]}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/test/objects/Employee/aggregate",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// The inline closure (if still present) would return a JSON error like
	// {"errorName":"IndexNotFound"}. After removal, chi's default handler
	// returns plain-text 404. We check that NO JSON response is returned.
	ct := rec.Header().Get("Content-Type")
	if strings.HasPrefix(ct, "application/json") {
		t.Errorf("aggregate route reached a handler when OssSvc is nil — "+
			"inline closure was NOT removed from NewFullRouter "+
			"(status=%d body=%s)", rec.Code, rec.Body.String())
	}
}

// TestUS005_AggregateRouteThroughOSSHandler verifies that when OssSvc is
// wired along with AggEngine and IndexMgr, the aggregate endpoint is
// available and returns a proper JSON response from the OSS handler.
func TestUS005_AggregateRouteThroughOSSHandler(t *testing.T) {
	deps := &ServerDeps{
		OssSvc:    us006StubOSSService{},
		AggEngine: aggregation.NewEngine(),
		IndexMgr:  index.NewManager(t.TempDir()),
	}
	router := NewFullRouter(deps)

	body := `{"aggregation":[{"type":"count"}]}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/test/objects/Employee/aggregate",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// The route must return a JSON response from the OSS aggregate handler.
	// IndexNotFound is expected (no test data loaded) — what matters is that
	// it's a JSON response, not chi's default text 404/405.
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("expected JSON response from aggregate handler, got content-type=%q "+
			"status=%d body=%s", ct, rec.Code, rec.Body.String())
	}
}

// TestUS005_AggregateNotConfiguredWithoutDeps verifies that when OssSvc is
// wired but AggEngine/IndexMgr are not, the aggregate handler returns a
// proper AggregationNotConfigured JSON error (not a panic or 500).
func TestUS005_AggregateNotConfiguredWithoutDeps(t *testing.T) {
	deps := &ServerDeps{
		OssSvc: us006StubOSSService{},
		// AggEngine and IndexMgr intentionally nil
	}
	router := NewFullRouter(deps)

	body := `{"aggregation":[{"type":"count"}]}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/test/objects/Employee/aggregate",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// The route must still be registered (OSS handler registers all routes)
	// but should return an appropriate error when aggregation is not configured.
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("expected JSON error from aggregate handler when not configured, "+
			"got content-type=%q status=%d body=%s", ct, rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "AggregationNotConfigured") {
		t.Errorf("expected AggregationNotConfigured error, got body=%s", rec.Body.String())
	}
}
