package auth

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/liyang/weave/pkg/apierror"
)

// ActionCheckBatchRequest is the JSON body of
// POST /api/v2/me/checks/actionTypes — round-109 sibling of
// round-107's object-type bulk probe.
type ActionCheckBatchRequest struct {
	OntologyAPIName    string   `json:"ontologyApiName"`
	ActionTypeAPINames []string `json:"actionTypeApiNames"`
}

// ActionCheckBatchEntry is one row in the bulk response. Found tells
// the SPA whether the action type actually exists in the ontology;
// CanApply reflects the caller's perms when Found is true and is
// false when Found is false (so the SPA never accidentally shows
// an Apply button for a deleted/renamed action).
type ActionCheckBatchEntry struct {
	ActionTypeAPIName string `json:"actionTypeApiName"`
	Found             bool   `json:"found"`
	// ActionTypeRID is empty when Found is false.
	ActionTypeRID string `json:"actionTypeRid,omitempty"`
	CanApply      bool   `json:"canApply"`
}

// ActionCheckBatchResponse preserves input order — row N of results
// corresponds to row N of the request without a name→row map.
type ActionCheckBatchResponse struct {
	OntologyAPIName string                  `json:"ontologyApiName"`
	Results         []ActionCheckBatchEntry `json:"results"`
}

// ActionCheckBatchHandler returns an http.Handler for
// POST /api/v2/me/checks/actionTypes — round-109 Foundry-parity
// bulk action applicability probe. Sibling of round-107
// ObjectCheckBatchHandler; together the two endpoints let a freshly-
// loaded SPA page resolve both its OT read/write matrix AND its
// applicable-actions list in TWO POSTs instead of K+M parallel GETs
// against the per-resource round-105/103 single endpoints.
//
// Same found:bool discriminator pattern as round-107 (the batch
// surface can't 404 per-entry so the discriminator moves into the
// payload). Same role-set-compute-once optimization (O(K)×O(1) perm
// check, not O(K) role rebuilds).
//
// Architecture note: zero new interfaces — reuses round-95
// OntologyResolver and round-103 ActionTypeResolver. The cmd/server
// omsOntologyResolver adapter already implements both.
//
// 401 unauthenticated. 400 on malformed body or empty
// actionTypeApiNames. 404 OntologyNotFound when ontology missing
// (whole request fails — can't reason about actions without an
// ontology).
func ActionCheckBatchHandler(ontResolver OntologyResolver, actionResolver ActionTypeResolver) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := UserFromContext(r.Context())
		if u == nil {
			apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
				"reason": "no authenticated user in request context",
			}))
			return
		}

		raw, err := io.ReadAll(r.Body)
		if err != nil || len(raw) == 0 {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
				"reason": "request body is required",
			}))
			return
		}
		var req ActionCheckBatchRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
				"reason": err.Error(),
			}))
			return
		}
		if req.OntologyAPIName == "" {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
				"reason": "ontologyApiName is required",
			}))
			return
		}
		if len(req.ActionTypeAPINames) == 0 {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
				"reason": "actionTypeApiNames must be a non-empty array",
			}))
			return
		}

		o, err := ontResolver.GetOntology(r.Context(), req.OntologyAPIName)
		if err != nil {
			if errors.Is(err, ErrOntologyNotFound) {
				apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{
					"ontologyApiName": req.OntologyAPIName,
				}))
				return
			}
			apierror.WriteJSON(w, apierror.NewInternal("ActionCheckBatchFailed", nil))
			return
		}

		// Effective role set — compute once, reuse for every entry.
		// Same shape as round-107 ObjectCheckBatchHandler.
		roles := append([]string(nil), u.Roles...)
		if u.OntologyRoles != nil {
			if scoped := u.OntologyRoles[o.RID]; scoped != "" {
				roles = append(roles, scoped)
			}
		}
		canApply := HasPermission(roles, PermActionExecute)

		results := make([]ActionCheckBatchEntry, 0, len(req.ActionTypeAPINames))
		for _, atName := range req.ActionTypeAPINames {
			entry := ActionCheckBatchEntry{ActionTypeAPIName: atName}
			at, err := actionResolver.GetActionType(r.Context(), o.RID, atName)
			if err != nil {
				// Per-entry "doesn't exist" — CanApply stays false
				// regardless of caller perms. Found discriminator lets
				// the SPA surface "action removed from config".
				results = append(results, entry)
				continue
			}
			entry.Found = true
			entry.ActionTypeRID = at.RID
			entry.CanApply = canApply
			results = append(results, entry)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(ActionCheckBatchResponse{
			OntologyAPIName: o.APIName,
			Results:         results,
		})
	})
}
