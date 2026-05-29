package auth

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/liyang/weave/pkg/apierror"
)

// PermissionsCheckRequest is the JSON body for POST /api/v2/me/permissions/check.
type PermissionsCheckRequest struct {
	Permissions []string `json:"permissions"`
	// Ontology, when non-empty, narrows the caller's effective role
	// set to (global roles ∪ this ontology's scoped role) instead of
	// the union of every per-ontology role. Mirrors the same scoping
	// logic the round-95 OntologyMeHandler uses.
	Ontology string `json:"ontology,omitempty"`
}

// PermissionsCheckResponse is the JSON response for POST /api/v2/me/permissions/check.
// Granted and Denied always partition the input permission set with
// no overlap and no missing entries — callers can rely on
// len(granted)+len(denied) == len(request.permissions).
type PermissionsCheckResponse struct {
	Granted []string `json:"granted"`
	Denied  []string `json:"denied"`
}

// PermissionsCheckHandler returns an http.Handler for
// POST /api/v2/me/permissions/check — round-97 Foundry-parity
// fine-grained permission probe. Given a list of permission names,
// returns which the caller holds and which they lack, optionally
// scoped to one ontology.
//
// The SPA uses this for batch-gating dynamic UI ("for each row in
// this table, can the user run THIS action?"). The shape avoids N
// round-trips to /api/v2/me + client-side filtering. Foundry-parity
// sibling of the role-based gating exposed by /api/v2/me and round-95's
// /api/v2/ontologies/{ontology}/me.
//
// 401 when no User in context. 400 when body is malformed or
// permissions list is empty (catches client bugs — an empty probe
// is never useful and likely a coding error). 404 when Ontology
// field is set but the ontology does not exist.
//
// The resolver argument may be nil, in which case any non-empty
// ontology field returns 400 InvalidParameter ("ontology scoping
// not configured"). This keeps the handler usable in degraded
// mode (no OMS repo) where global-role checks still work.
func PermissionsCheckHandler(resolver OntologyResolver) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := UserFromContext(r.Context())
		if u == nil {
			apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
				"reason": "no authenticated user in request context",
			}))
			return
		}

		raw, err := io.ReadAll(r.Body)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
				"reason": err.Error(),
			}))
			return
		}
		var req PermissionsCheckRequest
		if len(raw) == 0 {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
				"reason": "request body is required",
			}))
			return
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
				"reason": err.Error(),
			}))
			return
		}
		if len(req.Permissions) == 0 {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
				"reason": "permissions array must be non-empty",
			}))
			return
		}

		// Compose the role set we'll evaluate against. By default it's
		// (global + every per-ontology role) which matches /api/v2/me's
		// permission union. When Ontology is set, narrow to (global +
		// just that ontology's scoped role) mirroring round-95.
		roles := append([]string(nil), u.Roles...)
		if req.Ontology == "" {
			for _, role := range u.OntologyRoles {
				roles = append(roles, role)
			}
		} else {
			if resolver == nil {
				apierror.WriteJSON(w, apierror.NewInvalidParameter("OntologyScopingNotConfigured", map[string]string{
					"reason": "server runs in degraded mode without ontology resolver; omit `ontology` field for global check",
				}))
				return
			}
			o, err := resolver.GetOntology(r.Context(), req.Ontology)
			if err != nil {
				if errors.Is(err, ErrOntologyNotFound) {
					apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{
						"ontology": req.Ontology,
					}))
					return
				}
				apierror.WriteJSON(w, apierror.NewInternal("PermissionsCheckFailed", nil))
				return
			}
			if u.OntologyRoles != nil {
				if scoped := u.OntologyRoles[o.RID]; scoped != "" {
					roles = append(roles, scoped)
				}
			}
		}

		granted := make([]string, 0, len(req.Permissions))
		denied := make([]string, 0, len(req.Permissions))
		for _, p := range req.Permissions {
			if HasPermission(roles, p) {
				granted = append(granted, p)
			} else {
				denied = append(denied, p)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(PermissionsCheckResponse{
			Granted: granted,
			Denied:  denied,
		})
	})
}
