package auth

import (
	"net/http"

	"github.com/liyang/weave/pkg/apierror"
)

// RequirePermission returns an HTTP middleware that allows the request to
// proceed only if the authenticated user holds at least one role that grants
// the given permission.
//
// Three outcomes:
//   - 401 UNAUTHORIZED if no User is present in the request context (the
//     upstream auth middleware never ran or rejected the request).
//   - 403 PERMISSION_DENIED if the User has no role that grants the permission
//     (checked against both global roles and ontology-scoped roles).
//   - next.ServeHTTP otherwise.
//
// For resource-scoped writes (e.g. updating ontology X requires
// ontology-owner of X specifically), handlers should additionally call
// EnforceOntologyScope. RequirePermission only enforces the type-level
// permission so that the surrounding handler can read the chi URL params.
func RequirePermission(perm string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := UserFromContext(r.Context())
			if u == nil {
				apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
					"reason": "no authenticated user in request context",
				}))
				return
			}

			if userHasPermission(u, perm) {
				next.ServeHTTP(w, r)
				return
			}

			apierror.WriteJSON(w, apierror.NewPermissionDenied("PermissionDenied", map[string]string{
				"permission": perm,
				"userId":     u.ID,
			}))
		})
	}
}

// userHasPermission returns true if the user holds any role (global or
// ontology-scoped) that grants the permission.
func userHasPermission(u *User, perm string) bool {
	if u == nil {
		return false
	}
	if HasPermission(u.Roles, perm) {
		return true
	}
	for _, role := range u.OntologyRoles {
		if HasPermission([]string{role}, perm) {
			return true
		}
	}
	return false
}
