package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/liyang/weave/pkg/oms"
)

// mockRepo returns nil for all methods — we only need to verify route registration, not handler logic.
type routeTestRepo struct{ oms.Repository }

func TestSecurityPolicyRoutesRegistered(t *testing.T) {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer) // recover from nil repo panics; we only check route existence
	handler := oms.NewOMSHandler(routeTestRepo{})
	RegisterRoutes(r, handler)

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/admin/objectTypes/ri.test/securityPolicies"},
		{http.MethodGet, "/api/admin/objectTypes/ri.test/securityPolicies"},
		{http.MethodGet, "/api/admin/securityPolicies/ri.test"},
		{http.MethodPut, "/api/admin/securityPolicies/ri.test"},
		{http.MethodDelete, "/api/admin/securityPolicies/ri.test"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			// A 405 (Method Not Allowed) or actual response means route exists.
			// A 404 means the route is NOT registered.
			if w.Code == http.StatusNotFound {
				t.Errorf("route %s %s returned 404 — not registered", tt.method, tt.path)
			}
		})
	}
}
