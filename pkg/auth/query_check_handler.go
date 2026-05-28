package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
)

// ResolvedQueryType is the minimal projection of pkg/oms.QueryType
// the round-113 QueryCheckHandler needs — two fields keep the
// pkg/auth ↔ pkg/oms bridge narrow (same rationale as
// ResolvedActionType from round 103 and ResolvedObjectType from
// round 105).
type ResolvedQueryType struct {
	RID     string
	APIName string
}

// QueryTypeResolver looks up a query type by its API name within
// a specific ontology RID. Sibling of round-103 ActionTypeResolver
// and round-105 ObjectTypeResolver. cmd/server wires the concrete
// oms.Repository adapter at boot.
type QueryTypeResolver interface {
	GetQueryType(ctx context.Context, ontologyRID, queryTypeAPIName string) (*ResolvedQueryType, error)
}

// ErrQueryTypeNotFound is the sentinel resolvers return when the
// requested query type is absent. cmd/server's adapter translates
// pkg/oms.ErrNotFound to this so the handler stays free of pkg/oms
// imports.
var ErrQueryTypeNotFound = errors.New("query type not found")

// QueryCheckResponse is the JSON body of
// GET /api/v2/ontologies/{ontologyApiName}/queryTypes/{queryTypeApiName}/check.
type QueryCheckResponse struct {
	OntologyAPIName  string `json:"ontologyApiName"`
	QueryTypeAPIName string `json:"queryTypeApiName"`
	QueryTypeRID     string `json:"queryTypeRid"`
	// CanExecute tells the SPA whether the caller's roles grant
	// PermQueryTypeRead — Weave query types are read-only
	// computed views so the read permission gates execution. The
	// wire field is canExecute (Foundry-parity external name)
	// while the internal permission is queryType.read; the
	// distinction matches the action.apply/action.execute mapping
	// from round 103.
	CanExecute bool `json:"canExecute"`
}

// QueryCheckHandler returns an http.Handler for
// GET /api/v2/ontologies/{ontologyApiName}/queryTypes/{queryTypeApiName}/check
// — round-113 Foundry-parity query applicability probe.
//
// Third axis of the per-resource check family: round-103 actions,
// round-105 object-types, and now query-types. SPA uses this to
// gate per-query-type "Run Query" affordances. canExecute reflects
// the caller's effective role set on this ontology (global ∪
// scoped role).
//
// 401 when no User. 404 OntologyNotFound when ontology missing,
// 404 QueryTypeNotFound when query type missing. Otherwise 200
// with canExecute boolean. Always 200 with boolean — the probe
// is informational, never 403 (same convention as round 103/105).
func QueryCheckHandler(ontResolver OntologyResolver, queryResolver QueryTypeResolver) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := UserFromContext(r.Context())
		if u == nil {
			apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
				"reason": "no authenticated user in request context",
			}))
			return
		}

		ontAPIName := chi.URLParam(r, "ontologyApiName")
		qtAPIName := chi.URLParam(r, "queryTypeApiName")
		if qtAPIName == "" {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingQueryTypeApiName", map[string]string{
				"reason": "queryTypeAPIName path parameter is required",
			}))
			return
		}

		o, err := ontResolver.GetOntology(r.Context(), ontAPIName)
		if err != nil {
			if errors.Is(err, ErrOntologyNotFound) {
				apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{
					"ontologyApiName": ontAPIName,
				}))
				return
			}
			apierror.WriteJSON(w, apierror.NewInternal("QueryCheckFailed", nil))
			return
		}

		qt, err := queryResolver.GetQueryType(r.Context(), o.RID, qtAPIName)
		if err != nil {
			if errors.Is(err, ErrQueryTypeNotFound) {
				apierror.WriteJSON(w, apierror.NewNotFound("QueryTypeNotFound", map[string]string{
					"ontologyApiName":  ontAPIName,
					"queryTypeApiName": qtAPIName,
				}))
				return
			}
			apierror.WriteJSON(w, apierror.NewInternal("QueryCheckFailed", nil))
			return
		}

		roles := append([]string(nil), u.Roles...)
		if u.OntologyRoles != nil {
			if scoped := u.OntologyRoles[o.RID]; scoped != "" {
				roles = append(roles, scoped)
			}
		}

		resp := QueryCheckResponse{
			OntologyAPIName:  o.APIName,
			QueryTypeAPIName: qt.APIName,
			QueryTypeRID:     qt.RID,
			CanExecute:       HasPermission(roles, PermQueryTypeRead),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})
}
