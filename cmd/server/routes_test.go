package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/liyang/weave/pkg/auth"
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

func TestSecurityPolicyRoutes_RequireAdmin(t *testing.T) {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	handler := oms.NewOMSHandler(routeTestRepo{})
	RegisterRoutes(r, handler)

	tests := []struct {
		name     string
		method   string
		path     string
		user     *auth.User
		wantCode int
	}{
		{
			name:     "admin can list",
			method:   http.MethodGet,
			path:     "/api/admin/objectTypes/ri.test/securityPolicies",
			user:     &auth.User{ID: "admin", Roles: []string{auth.RoleAdmin}},
			wantCode: http.StatusInternalServerError, // gate passes; nil repo panics into 500
		},
		{
			name:     "viewer denied list",
			method:   http.MethodGet,
			path:     "/api/admin/objectTypes/ri.test/securityPolicies",
			user:     &auth.User{ID: "viewer", Roles: []string{auth.RoleViewer}},
			wantCode: http.StatusForbidden,
		},
		{
			name:     "editor denied create",
			method:   http.MethodPost,
			path:     "/api/admin/objectTypes/ri.test/securityPolicies",
			user:     &auth.User{ID: "editor", Roles: []string{auth.RoleEditor}},
			wantCode: http.StatusForbidden,
		},
		{
			name:     "ontology-owner denied delete",
			method:   http.MethodDelete,
			path:     "/api/admin/securityPolicies/ri.test",
			user:     &auth.User{ID: "owner", Roles: []string{auth.RoleOntologyOwner}},
			wantCode: http.StatusForbidden,
		},
		{
			name:     "unauthenticated denied",
			method:   http.MethodGet,
			path:     "/api/admin/securityPolicies/ri.test",
			user:     nil,
			wantCode: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			if tt.user != nil {
				req = req.WithContext(auth.WithUser(req.Context(), tt.user))
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tt.wantCode {
				t.Errorf("expected status %d, got %d (body=%s)", tt.wantCode, w.Code, w.Body.String())
			}
		})
	}
}
