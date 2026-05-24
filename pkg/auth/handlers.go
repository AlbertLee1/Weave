package auth

import (
	"encoding/json"
	"net/http"

	"github.com/liyang/weave/pkg/apierror"
)

// MeResponse is the JSON shape returned by GET /api/v2/me.
type MeResponse struct {
	ID            string            `json:"id"`
	Email         string            `json:"email"`
	Name          string            `json:"name"`
	Roles         []string          `json:"roles"`
	OntologyRoles map[string]string `json:"ontologyRoles"`
	Permissions   []string          `json:"permissions"`
	// Markings is the caller's held marking set sourced from
	// user.Attributes[MarkingsAttributeKey]. Populated by US-054 so the UI
	// can render the access chip row and so the policy editor can preview
	// RuleTypeMarkingSubset evaluation without re-fetching the JWT.
	// Emitted as an empty array (never null) when the caller has no
	// markings so the frontend type can stay string[] rather than
	// string[]|null.
	Markings []string `json:"markings"`
}

// MeHandler returns an http.Handler for GET /api/v2/me. It serializes the
// authenticated user from the request context, including the resolved
// permission set computed from their roles.
//
// 401 if no User is in context (auth middleware was not applied or rejected).
func MeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := UserFromContext(r.Context())
		if u == nil {
			apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
				"reason": "no authenticated user in request context",
			}))
			return
		}

		// Compute permissions from both global and scoped roles. Scoped
		// roles also contribute permissions because the frontend uses this
		// list to enable/disable buttons; the backend always re-checks at
		// the route or handler level.
		allRoles := append([]string(nil), u.Roles...)
		for _, r := range u.OntologyRoles {
			allRoles = append(allRoles, r)
		}
		perms := PermissionsForRoles(allRoles)
		if perms == nil {
			perms = []string{}
		}

		roles := u.Roles
		if roles == nil {
			roles = []string{}
		}
		ontologyRoles := u.OntologyRoles
		if ontologyRoles == nil {
			ontologyRoles = map[string]string{}
		}

		markings := Markings(r.Context())
		if markings == nil {
			markings = []string{}
		}

		resp := MeResponse{
			ID:            u.ID,
			Email:         u.Email,
			Name:          u.Name,
			Roles:         roles,
			OntologyRoles: ontologyRoles,
			Permissions:   perms,
			Markings:      markings,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	})
}
