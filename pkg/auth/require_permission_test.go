package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
}

func TestRequirePermission_AllowsWhenRoleGrants(t *testing.T) {
	mw := RequirePermission(PermActionExecute)
	srv := mw(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := WithUser(req.Context(), &User{ID: "alice", Roles: []string{RoleEditor}})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestRequirePermission_DenyWhenRoleLacks(t *testing.T) {
	mw := RequirePermission(PermActionExecute)
	srv := mw(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := WithUser(req.Context(), &User{ID: "bob", Roles: []string{RoleViewer}})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected JSON error response, got %q", rec.Header().Get("Content-Type"))
	}
}

func TestRequirePermission_Unauthenticated(t *testing.T) {
	mw := RequirePermission(PermOntologyRead)
	srv := mw(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for unauthenticated request, got %d", rec.Code)
	}
}

func TestRequirePermission_OntologyOwnerScopedGrants(t *testing.T) {
	mw := RequirePermission(PermObjectTypeWrite)
	srv := mw(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := WithUser(req.Context(), &User{
		ID:    "carol",
		Roles: []string{RoleViewer},
		OntologyRoles: map[string]string{
			"ri.ontology.main.ontology.northwind": RoleOntologyOwner,
		},
	})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	// At the route level, having ANY ontology-owner grant lets the request
	// through; the handler then calls EnforceOntologyScope to verify the
	// specific resource. This mirrors how chi route middleware vs. handler
	// checks compose.
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for user with ontology-owner role, got %d", rec.Code)
	}
}

func TestRequirePermission_AdminBypass(t *testing.T) {
	mw := RequirePermission(PermSecurityPolicyManage)
	srv := mw(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := WithUser(req.Context(), &User{ID: "root", Roles: []string{RoleAdmin}})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for admin, got %d", rec.Code)
	}
}
