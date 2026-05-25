package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
)

// ResolvedActionType is the minimal projection of pkg/oms.ActionType
// the round-103 ActionCheckHandler needs — keeping it narrow avoids
// pulling pkg/oms types across the auth/oms package boundary and
// pins the resolver dependency surface to two fields.
type ResolvedActionType struct {
	RID     string
	APIName string
}

// ActionTypeResolver looks up an action type by its API name within
// a specific ontology RID. Sibling of round-95 OntologyResolver.
// cmd/server wires the concrete oms.Repository adapter at boot;
// pkg/auth stays free of pkg/oms imports (which already imports
// pkg/auth — a cycle is forbidden).
type ActionTypeResolver interface {
	GetActionType(ctx context.Context, ontologyRID, actionApiNameOrRID string) (*ResolvedActionType, error)
}

// ErrActionTypeNotFound is the sentinel resolvers return when the
// requested action type is absent. cmd/server's adapter translates
// pkg/oms.ErrNotFound to this so the handler can stay free of
// pkg/oms imports.
var ErrActionTypeNotFound = errors.New("action type not found")

// ActionCheckResponse is the JSON body of
// GET /api/v2/ontologies/{ontologyApiName}/actions/{actionApiName}/check.
type ActionCheckResponse struct {
	OntologyAPIName string `json:"ontologyApiName"`
	ActionAPIName   string `json:"actionApiName"`
	ActionRID       string `json:"actionRid"`
	// CanApply tells the SPA whether the caller's roles grant the
	// action.apply permission for this ontology's scope. False
	// when the caller lacks the permission; combine with a 404
	// (action does not exist) to distinguish "no" from "missing".
	CanApply bool `json:"canApply"`
}

// ActionCheckHandler returns an http.Handler for
// GET /api/v2/ontologies/{ontologyApiName}/actions/{actionApiName}/check
// — round-103 Foundry-parity action applicability probe.
//
// Foundry's SDK exposes this so the SPA can disable a per-row
// "Apply Action" button without round-tripping a real apply call.
// Distinct from round-97 PermissionsCheckHandler in two ways:
//   1. Validates that the action type EXISTS (404 if not) — a raw
//      permission probe always returns granted/denied regardless of
//      whether the action is real
//   2. Single GET (no body) so it fits naturally in row-render code
//      where a POST + body would be awkward
//
// 401 when no User. 404 OntologyNotFound when ontology missing,
// 404 ActionTypeNotFound when action missing. Otherwise 200 with
// canApply reflecting the caller's effective role set on this
// ontology (global ∪ scoped).
func ActionCheckHandler(ontResolver OntologyResolver, actionResolver ActionTypeResolver) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := UserFromContext(r.Context())
		if u == nil {
			apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
				"reason": "no authenticated user in request context",
			}))
			return
		}

		ontApiName := chi.URLParam(r, "ontologyApiName")
		actionApiName := chi.URLParam(r, "actionApiName")
		if actionApiName == "" {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingActionApiName", map[string]string{
				"reason": "actionApiName path parameter is required",
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
			apierror.WriteJSON(w, apierror.NewInternal("ActionCheckFailed", nil))
			return
		}

		at, err := actionResolver.GetActionType(r.Context(), o.RID, actionApiName)
		if err != nil {
			if errors.Is(err, ErrActionTypeNotFound) {
				apierror.WriteJSON(w, apierror.NewNotFound("ActionTypeNotFound", map[string]string{
					"ontologyApiName": ontApiName,
					"actionApiName":   actionApiName,
				}))
				return
			}
			apierror.WriteJSON(w, apierror.NewInternal("ActionCheckFailed", nil))
			return
		}

		// Effective role set: global + this ontology's scoped role.
		// Matches round-95 OntologyMeHandler and round-97 ontology-
		// scoped PermissionsCheckHandler so the three endpoints stay
		// consistent.
		roles := append([]string(nil), u.Roles...)
		if u.OntologyRoles != nil {
			if scoped := u.OntologyRoles[o.RID]; scoped != "" {
				roles = append(roles, scoped)
			}
		}
		// PermActionExecute is the project's canonical "apply this
		// action" permission. Foundry's external naming is "action.apply"
		// but Weave settled on "action.execute" (see pkg/auth/permissions.go
		// const block). The wire field stays canApply to match Foundry.
		canApply := HasPermission(roles, PermActionExecute)

		resp := ActionCheckResponse{
			OntologyAPIName: o.APIName,
			ActionAPIName:   at.APIName,
			ActionRID:       at.RID,
			CanApply:        canApply,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})
}
