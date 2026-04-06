package auth

import (
	"context"

	"github.com/liyang/weave/pkg/apierror"
)

// EnforceOntologyScope checks whether the authenticated user in ctx is
// allowed to perform an action requiring `perm` against the ontology
// identified by `ontologyRID`.
//
// Allow rules:
//   - User has a global role that grants `perm` (admin, etc).
//   - User has an ontology-scoped role on this exact ontology that grants
//     `perm` (e.g. ontology-owner of the matching ontology).
//
// Returns nil on allow, an *apierror.APIError (PermissionDenied) on deny.
// The caller is expected to write the returned APIError to the response.
func EnforceOntologyScope(ctx context.Context, ontologyRID string, perm string) error {
	u := UserFromContext(ctx)
	if u == nil {
		return apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		})
	}

	if HasPermission(u.Roles, perm) {
		return nil
	}

	if scopedRole, ok := u.OntologyRoles[ontologyRID]; ok {
		if HasPermission([]string{scopedRole}, perm) {
			return nil
		}
	}

	return apierror.NewPermissionDenied("PermissionDenied", map[string]string{
		"permission":  perm,
		"ontologyRid": ontologyRID,
		"userId":      u.ID,
	})
}
