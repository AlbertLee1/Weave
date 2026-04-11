package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestOntologyScopeMiddleware_AdminBypass: an admin user must be allowed
// through every {ontologyApiName} route. This is the dev-mode case where the
// auth middleware injects roles=[admin]; the scope middleware should be a
// no-op for the existing dev surface.
func TestOntologyScopeMiddleware_AdminBypass(t *testing.T) {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := WithUser(req.Context(), &User{ID: "admin", Roles: []string{RoleAdmin}})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Use(OntologyScopeMiddleware(PermObjectRead))
	r.Get("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/objects/Employee", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("admin should be allowed, got status %d body %s", rec.Code, rec.Body.String())
	}
}

// TestOntologyScopeMiddleware_DeniesNonAdminWithoutScopedRole: a viewer with
// no role for the requested ontology must be rejected with PermissionDenied.
func TestOntologyScopeMiddleware_DeniesNonAdminWithoutScopedRole(t *testing.T) {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := WithUser(req.Context(), &User{
				ID:    "alice",
				Roles: []string{},
				OntologyRoles: map[string]string{
					"northwind": RoleOntologyOwner,
				},
			})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Use(OntologyScopeMiddleware(PermObjectRead))
	r.Get("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/chinook/objects/Employee", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when caller has no role for chinook, got %d body %s", rec.Code, rec.Body.String())
	}
}

// TestOntologyScopeMiddleware_NoOntologyParamPassesThrough: routes that do
// not carry an {ontologyApiName} URL param (auth/me, sql queries, attachments)
// should not be gated by this middleware.
func TestOntologyScopeMiddleware_NoOntologyParamPassesThrough(t *testing.T) {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := WithUser(req.Context(), &User{ID: "alice", Roles: []string{RoleViewer}})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Use(OntologyScopeMiddleware(PermObjectRead))
	r.Get("/api/v2/me", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v2/me", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("non-ontology routes should pass through, got status %d", rec.Code)
	}
}
