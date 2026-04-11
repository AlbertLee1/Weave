package auth

import (
	"net/http"
	"strings"

	"github.com/liyang/weave/pkg/apierror"
)

// ontologyAPINameFromPath extracts the ontology api name from a request path
// when the path matches `/api/v2/ontologies/{ontologyApiName}/...`. Returns
// "" when the path is not ontology-scoped (auth/me, sqlQueries, attachments,
// etc) so the middleware can fall through.
//
// We parse the path string directly rather than using chi.URLParam because
// chi populates URL params during routing, and a r.Use() middleware attached
// at the auth group level runs BEFORE chi has matched the route — at that
// point chi.URLParam returns the empty string for every parameter and we
// would silently allow every request.
func ontologyAPINameFromPath(p string) string {
	const prefix = "/api/v2/ontologies/"
	rest, ok := strings.CutPrefix(p, prefix)
	if !ok {
		return ""
	}
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		rest = rest[:i]
	}
	// Foundry attachments live at /api/v2/ontologies/attachments — that path
	// segment is the resource group, not an ontology api name. Skip it so we
	// don't try to enforce scope on the global attachment endpoints.
	if rest == "" || rest == "attachments" {
		return ""
	}
	return rest
}

// OntologyScopeMiddleware enforces auth.EnforceOntologyScope on every request
// whose URL contains an {ontologyApiName} segment under /api/v2/ontologies/.
// On rejection it writes a 403 PermissionDenied via apierror.WriteJSON.
//
// US-044: Foundry-style multi-ontology isolation requires every ontology
// route to gate on the caller's role for that specific ontology. Wiring this
// as a chi middleware means handlers do not have to repeat the check; the
// router does it once and short-circuits before the handler runs.
//
// In dev mode (auth middleware injects an admin user) the global admin role
// grants every permission, so this middleware is a no-op for the existing
// dev/test surface. It only "bites" when AUTH_MODE=jwt and the JWT user has a
// scoped (ontology-owner / editor) role rather than the global admin role.
//
// Routes that do not carry an {ontologyApiName} parameter (e.g. global
// attachment uploads, sql queries, /api/v2/me) skip the check because there
// is no ontology to scope to — the caller may still be denied at the
// per-resource level by other middlewares such as RequirePermission.
func OntologyScopeMiddleware(perm string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ontologyRID := ontologyAPINameFromPath(r.URL.Path)
			if ontologyRID == "" {
				next.ServeHTTP(w, r)
				return
			}
			if err := EnforceOntologyScope(r.Context(), ontologyRID, perm); err != nil {
				if apiErr, ok := err.(*apierror.APIError); ok {
					apierror.WriteJSON(w, apiErr)
					return
				}
				apierror.WriteJSON(w, apierror.NewPermissionDenied("PermissionDenied", map[string]string{
					"reason": err.Error(),
				}))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
