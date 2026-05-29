package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
)

// OntologyResolver is the narrow interface OntologyMeHandler needs to
// translate an ontology API name into its RID. Lives here (not in
// pkg/oms) so pkg/auth can compose the handler without taking an
// import cycle on pkg/oms — cmd/server wires the concrete repository
// at boot.
type OntologyResolver interface {
	GetOntology(ctx context.Context, apiNameOrRID string) (*ResolvedOntology, error)
}

// OntologyLister is the round-99 sibling of OntologyResolver: list
// every ontology the system knows about (the handler filters down
// to those the caller has scoped roles on). Separate interface so
// degraded-mode (no OMS repo) installs can wire OntologyResolver
// without committing to ListOntologies — the /me/ontologies route
// is mounted only when a lister is available.
type OntologyLister interface {
	ListOntologies(ctx context.Context) ([]ResolvedOntology, error)
}

// ResolvedOntology is the minimal projection of pkg/oms.Ontology this
// handler needs. Keeping it local avoids a cross-package struct
// import and pins the dependency surface to just three fields.
// DisplayName surfaces in the round-99 /me/ontologies response so
// SPA picker UIs can render labels without a second fetch.
type ResolvedOntology struct {
	RID         string
	APIName     string
	DisplayName string
}

// ErrOntologyNotFound is the sentinel resolvers return when the
// requested ontology is absent. cmd/server's adapter translates
// pkg/oms.ErrNotFound to this so the handler can stay free of
// pkg/oms imports.
var ErrOntologyNotFound = errors.New("ontology not found")

// OntologyMeResponse is the JSON shape returned by
// GET /api/v2/ontologies/{ontologyApiName}/me.
type OntologyMeResponse struct {
	OntologyRID     string `json:"ontologyRid"`
	OntologyAPIName string `json:"ontologyApiName"`
	// Role is the scoped per-ontology role the caller holds for this
	// specific ontology — empty string when none. Distinct from the
	// global Roles array on MeResponse: callers can have multiple
	// global roles but at most one role per ontology.
	Role        string   `json:"role"`
	Permissions []string `json:"permissions"`
	Markings    []string `json:"markings"`
}

// OntologyMeHandler returns an http.Handler for GET
// /api/v2/ontologies/{ontologyApiName}/me — the per-ontology
// caller-scope query (round 95, Foundry-parity).
//
// Returns the calling user's resolved role and effective permissions
// for ONE specific ontology — narrower than /api/v2/me, which exposes
// the full per-ontology role map. The SPA uses this shape to gate UI
// affordances on a page that has already scoped itself to an ontology
// ("can I see the Create button on THIS ontology?") without parsing
// the global OntologyRoles map client-side.
//
// 401 when no User is in context (auth middleware was not applied or
// rejected). 404 when the requested ontology does not exist. Other
// failures bubble up as 500.
func OntologyMeHandler(resolver OntologyResolver) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := UserFromContext(r.Context())
		if u == nil {
			apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
				"reason": "no authenticated user in request context",
			}))
			return
		}

		apiName := chi.URLParam(r, "ontologyApiName")
		o, err := resolver.GetOntology(r.Context(), apiName)
		if err != nil {
			if errors.Is(err, ErrOntologyNotFound) {
				apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{
					"ontologyApiName": apiName,
				}))
				return
			}
			apierror.WriteJSON(w, apierror.NewInternal("GetOntologyMeFailed", nil))
			return
		}

		scopedRole := ""
		if u.OntologyRoles != nil {
			scopedRole = u.OntologyRoles[o.RID]
		}

		// Effective permission set: union of global roles + the one
		// scoped role for this ontology. Matches MeResponse shape but
		// restricted to a single ontology rather than summing every
		// scoped role.
		roles := append([]string(nil), u.Roles...)
		if scopedRole != "" {
			roles = append(roles, scopedRole)
		}
		perms := PermissionsForRoles(roles)
		if perms == nil {
			perms = []string{}
		}

		markings := Markings(r.Context())
		if markings == nil {
			markings = []string{}
		}

		resp := OntologyMeResponse{
			OntologyRID:     o.RID,
			OntologyAPIName: o.APIName,
			Role:            scopedRole,
			Permissions:     perms,
			Markings:        markings,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})
}
