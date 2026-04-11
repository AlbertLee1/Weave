package main

// PR-02: rename action apply routes to embed {action} in the path,
// matching Foundry OSv2 SDK:
//
//   POST /api/v2/ontologies/{ontology}/actions/{action}/apply
//   POST /api/v2/ontologies/{ontology}/actions/{action}/applyBatch
//
// The old body-driven form (`POST .../actions/apply` with body.actionType)
// is removed (rip-and-replace per project decision). These tests lock the
// new routing boundary on NewFullRouter.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/actions"
	"github.com/liyang/weave/pkg/oms"
)

// pr02StubOMSRepo embeds oms.Repository as a nil interface. The action
// executor only calls repo methods when it actually resolves an action,
// which the PR-02 routing tests do not exercise — they only check that
// chi resolves the new paths to the action handler.
type pr02StubOMSRepo struct {
	oms.Repository
}

// TestPR02_NewActionPathsRegistered locks that the Foundry-aligned action
// routes exist on the router once ActionExecutor is wired.
func TestPR02_NewActionPathsRegistered(t *testing.T) {
	deps := &ServerDeps{
		OmsRepo:        pr02StubOMSRepo{},
		ActionExecutor: actions.NewExecutor(pr02StubOMSRepo{}, nil),
	}
	router := NewFullRouter(deps)

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v2/ontologies/test/actions/renameEmployee/apply"},
		{http.MethodPost, "/api/v2/ontologies/test/actions/renameEmployee/applyBatch"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			// Body is intentionally empty — we only care that chi resolves
			// the route to the action handler. The handler will reject the
			// request (400 / 500) but that is still not a chi 404.
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			// Route is unmounted only if chi falls through to the default
			// text/plain 404 handler. Any application/json apierror response
			// means the handler was reached.
			if rec.Code == http.StatusNotFound &&
				!strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
				t.Errorf("route %s %s returned chi default 404 — not registered (content-type=%q body=%s)",
					tt.method, tt.path, rec.Header().Get("Content-Type"), rec.Body.String())
			}
		})
	}
}

// TestPR02_OldActionPathsRemoved locks the rip-and-replace: the legacy
// body-driven routes must not exist on the new router.
func TestPR02_OldActionPathsRemoved(t *testing.T) {
	deps := &ServerDeps{
		OmsRepo:        pr02StubOMSRepo{},
		ActionExecutor: actions.NewExecutor(pr02StubOMSRepo{}, nil),
	}
	router := NewFullRouter(deps)

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v2/ontologies/test/actions/apply"},
		{http.MethodPost, "/api/v2/ontologies/test/actions/applyBatch"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path,
				strings.NewReader(`{"actionType":"foo","parameters":{}}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			// Expect chi's default 404 (text/plain) because the route is
			// gone. If an application/json apierror comes back the legacy
			// route is still registered — PR-02 is incomplete.
			if rec.Code != http.StatusNotFound {
				t.Errorf("expected 404 for removed path %s %s, got %d (body=%s)",
					tt.method, tt.path, rec.Code, rec.Body.String())
			}
			if strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
				t.Errorf("removed path %s %s still reaches a Weave handler (content-type=%q)",
					tt.method, tt.path, rec.Header().Get("Content-Type"))
			}
		})
	}
}

// TestPR02_Handler_ReadsActionFromPath directly exercises the handler's
// ability to read the {action} path parameter. A body without actionType
// must still work because the path is the source of truth.
func TestPR02_Handler_ReadsActionFromPath(t *testing.T) {
	// A nil executor would panic inside h.executor.Apply. Passing a real
	// executor with a nil OMS repo causes the executor to return a typed
	// error (ontology not found / action not found) rather than panic,
	// which is fine because we only care about what path segment the
	// handler extracted before delegating.
	_ = context.TODO()

	deps := &ServerDeps{
		OmsRepo:        pr02StubOMSRepo{},
		ActionExecutor: actions.NewExecutor(pr02StubOMSRepo{}, nil),
	}
	router := NewFullRouter(deps)

	// Body has no actionType at all — Foundry's body schema is only
	// { parameters, options? }. The handler must derive the action from
	// the path and not complain about a missing body field.
	body := `{"parameters":{"x":1}}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/test/actions/renameEmployee/apply",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// The handler will hit the executor with "renameEmployee" as the
	// action name and the stub OMS repo will fail the action lookup —
	// that produces a 400/500 apierror, NOT a MissingActionType error.
	// If we still see "MissingActionType", the handler never reached
	// the URL parameter and is still demanding it from the body.
	if strings.Contains(rec.Body.String(), "MissingActionType") {
		t.Errorf("handler still requires actionType in body — path parameter not wired (body=%s)",
			rec.Body.String())
	}
}
