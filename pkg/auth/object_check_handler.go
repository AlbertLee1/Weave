package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
)

// ResolvedObjectType is the minimal projection of pkg/oms.ObjectType
// the round-105 ObjectCheckHandler needs — two fields keep the
// pkg/auth ↔ pkg/oms bridge narrow (same rationale as
// ResolvedActionType from round 103).
type ResolvedObjectType struct {
	RID     string
	APIName string
}

// ObjectTypeResolver looks up an object type by its API name within
// a specific ontology RID. Sibling of round-103 ActionTypeResolver.
// cmd/server wires the concrete oms.Repository adapter at boot.
type ObjectTypeResolver interface {
	GetObjectType(ctx context.Context, ontologyRID, objectTypeApiName string) (*ResolvedObjectType, error)
}

// ErrObjectTypeNotFound is the sentinel resolvers return when the
// requested object type is absent. cmd/server's adapter translates
// pkg/oms.ErrNotFound to this so the handler stays free of pkg/oms
// imports.
var ErrObjectTypeNotFound = errors.New("object type not found")

// ObjectCheckResponse is the JSON body of
// GET /api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/check.
type ObjectCheckResponse struct {
	OntologyAPIName   string `json:"ontologyApiName"`
	ObjectTypeAPIName string `json:"objectTypeApiName"`
	ObjectTypeRID     string `json:"objectTypeRid"`
	// CanRead tells the SPA whether the caller's roles grant
	// PermObjectRead on this ontology's scope (global ∪ scoped role).
	CanRead bool `json:"canRead"`
	// CanWrite tells the SPA whether the caller can mutate object
	// rows of this type — checks PermObjectWrite. Used to disable
	// inline-edit affordances on per-row UI.
	CanWrite bool `json:"canWrite"`
}

// ObjectCheckHandler returns an http.Handler for
// GET /api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/check
// — round-105 Foundry-parity object-type applicability probe.
//
// Sibling of round-103 ActionCheckHandler. SPA uses this to gate
// per-object-type UI affordances ("show the inline edit pencil",
// "show the row-detail drawer at all") with a single GET instead
// of N permission-name lookups against PermissionsCheckHandler.
//
// Distinct from round-103 ActionCheckHandler in that it returns
// TWO booleans (read + write) — the two-axis matrix matters most
// for object UI because read=show-the-row, write=show-the-pencil.
//
// 401 when no User. 404 OntologyNotFound when ontology missing,
// 404 ObjectTypeNotFound when object type missing. Otherwise 200
// with canRead/canWrite reflecting the caller's effective role
// set (global ∪ scoped) on this ontology.
func ObjectCheckHandler(ontResolver OntologyResolver, objResolver ObjectTypeResolver) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := UserFromContext(r.Context())
		if u == nil {
			apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
				"reason": "no authenticated user in request context",
			}))
			return
		}

		ontApiName := chi.URLParam(r, "ontologyApiName")
		otApiName := chi.URLParam(r, "objectTypeApiName")
		if otApiName == "" {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingObjectTypeApiName", map[string]string{
				"reason": "objectTypeApiName path parameter is required",
			}))
			return
		}

		o, err := ontResolver.GetOntology(r.Context(), ontApiName)
		if err != nil {
			if errors.Is(err, ErrOntologyNotFound) {
				apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{
					"ontologyApiName": ontApiName,
				}))
				return
			}
			apierror.WriteJSON(w, apierror.NewInternal("ObjectCheckFailed", nil))
			return
		}

		ot, err := objResolver.GetObjectType(r.Context(), o.RID, otApiName)
		if err != nil {
			if errors.Is(err, ErrObjectTypeNotFound) {
				apierror.WriteJSON(w, apierror.NewNotFound("ObjectTypeNotFound", map[string]string{
					"ontologyApiName":   ontApiName,
					"objectTypeApiName": otApiName,
				}))
				return
			}
			apierror.WriteJSON(w, apierror.NewInternal("ObjectCheckFailed", nil))
			return
		}

		// Effective role set: global + this ontology's scoped role.
		// Matches the convention shared by OntologyMeHandler (r95),
		// PermissionsCheckHandler (r97), ActionCheckHandler (r103).
		roles := append([]string(nil), u.Roles...)
		if u.OntologyRoles != nil {
			if scoped := u.OntologyRoles[o.RID]; scoped != "" {
				roles = append(roles, scoped)
			}
		}

		resp := ObjectCheckResponse{
			OntologyAPIName:   o.APIName,
			ObjectTypeAPIName: ot.APIName,
			ObjectTypeRID:     ot.RID,
			CanRead:           HasPermission(roles, PermObjectRead),
			CanWrite:          HasPermission(roles, PermObjectWrite),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})
}
