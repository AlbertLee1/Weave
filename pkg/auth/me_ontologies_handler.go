package auth

import (
	"encoding/json"
	"net/http"

	"github.com/liyang/weave/pkg/apierror"
)

// MeOntologiesEntry is one row in the /api/v2/me/ontologies response.
// Mirrors the camelCase wire form the existing OntologiesAPI uses so
// SDK consumers can deserialise either shape with the same model.
type MeOntologiesEntry struct {
	RID         string `json:"rid"`
	APIName     string `json:"apiName"`
	DisplayName string `json:"displayName"`
	// Role is the caller's scoped role on this ontology. Always
	// populated (entries are filtered to ontologies where Role != "").
	Role string `json:"role"`
}

// MeOntologiesResponse is the JSON shape for GET /api/v2/me/ontologies.
type MeOntologiesResponse struct {
	// Ontologies is the subset of system ontologies where the caller
	// holds a scoped role (i.e. u.OntologyRoles has an entry whose RID
	// matches a known ontology). Always an array, never null, so the
	// SPA can iterate without nil-checks.
	Ontologies []MeOntologiesEntry `json:"ontologies"`
}

// MeOntologiesHandler returns an http.Handler for GET /api/v2/me/ontologies
// — round-99 Foundry-parity caller-scoped ontology inventory.
//
// Returns the subset of system ontologies where the caller holds a
// scoped per-ontology role. The SPA's ontology-picker uses this to
// render "your ontologies" without listing every ontology + filtering
// against the OntologyRoles map client-side. Foundry exposes the
// equivalent /me/ontologies endpoint for the same reason.
//
// Filtering rule: an ontology appears iff u.OntologyRoles[ontology.RID]
// is non-empty. Users with only global roles (no scoped grants) get an
// empty array — they're expected to use the regular /api/v2/ontologies
// listing in that case.
//
// 401 when no User in context. 500 when the lister fails.
func MeOntologiesHandler(lister OntologyLister) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := UserFromContext(r.Context())
		if u == nil {
			apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
				"reason": "no authenticated user in request context",
			}))
			return
		}

		// Empty scoped role map -> empty response. Short-circuit so we
		// skip the (potentially expensive) ListOntologies call when
		// the caller has no scoped roles to filter against.
		if len(u.OntologyRoles) == 0 {
			writeMeOntologies(w, []MeOntologiesEntry{})
			return
		}

		all, err := lister.ListOntologies(r.Context())
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("MeOntologiesFailed", map[string]string{
				"reason": err.Error(),
			}))
			return
		}

		out := make([]MeOntologiesEntry, 0, len(u.OntologyRoles))
		for _, o := range all {
			role := u.OntologyRoles[o.RID]
			if role == "" {
				continue
			}
			out = append(out, MeOntologiesEntry{
				RID:         o.RID,
				APIName:     o.APIName,
				DisplayName: o.DisplayName,
				Role:        role,
			})
		}
		writeMeOntologies(w, out)
	})
}

func writeMeOntologies(w http.ResponseWriter, entries []MeOntologiesEntry) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(MeOntologiesResponse{Ontologies: entries})
}
