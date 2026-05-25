package auth

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/liyang/weave/pkg/apierror"
)

// QueryCheckBatchRequest is the JSON body of
// POST /api/v2/me/checks/queryTypes — round-115 third axis of the
// bulk probe trio (after r107 OT and r109 action).
type QueryCheckBatchRequest struct {
	OntologyAPIName   string   `json:"ontologyApiName"`
	QueryTypeAPINames []string `json:"queryTypeApiNames"`
}

// QueryCheckBatchEntry is one row in the bulk response. Same
// found:bool discriminator pattern as round-107/109: distinguishes
// "removed from config" from "exists but no perm". found=false
// entries always carry canExecute=false regardless of caller perms.
type QueryCheckBatchEntry struct {
	QueryTypeAPIName string `json:"queryTypeApiName"`
	Found            bool   `json:"found"`
	// QueryTypeRID is empty when Found is false.
	QueryTypeRID string `json:"queryTypeRid,omitempty"`
	CanExecute   bool   `json:"canExecute"`
}

// QueryCheckBatchResponse preserves input order so the SPA can
// correlate row N → row N without a name→row map.
type QueryCheckBatchResponse struct {
	OntologyAPIName string                 `json:"ontologyApiName"`
	Results         []QueryCheckBatchEntry `json:"results"`
}

// QueryCheckBatchHandler returns an http.Handler for
// POST /api/v2/me/checks/queryTypes — round-115 third axis of the
// Foundry-parity bulk probe trio. Together with round-107 OT bulk
// and round-109 action bulk, a freshly-loaded SPA page can resolve
// its full per-resource gating matrix (OT read/write + action apply
// + query execute) in THREE POSTs instead of K+M+P parallel GETs.
//
// Zero new interfaces — reuses round-95 OntologyResolver +
// round-113 QueryTypeResolver. omsOntologyResolver adapter already
// implements both, no new wiring concerns.
//
// 401 unauthenticated. 400 on malformed body or empty array. 404
// OntologyNotFound when ontology missing.
func QueryCheckBatchHandler(ontResolver OntologyResolver, queryResolver QueryTypeResolver) http.Handler {
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
		var req QueryCheckBatchRequest
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
		if len(req.QueryTypeAPINames) == 0 {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
				"reason": "queryTypeApiNames must be a non-empty array",
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
			apierror.WriteJSON(w, apierror.NewInternal("QueryCheckBatchFailed", nil))
			return
		}

		// Effective role set — compute once, reuse for every entry.
		// Same shape as r107/r109; canExecute checks queryType.read
		// (read-only computed views).
		roles := append([]string(nil), u.Roles...)
		if u.OntologyRoles != nil {
			if scoped := u.OntologyRoles[o.RID]; scoped != "" {
				roles = append(roles, scoped)
			}
		}
		canExecute := HasPermission(roles, PermQueryTypeRead)

		results := make([]QueryCheckBatchEntry, 0, len(req.QueryTypeAPINames))
		for _, qtName := range req.QueryTypeAPINames {
			entry := QueryCheckBatchEntry{QueryTypeAPIName: qtName}
			qt, err := queryResolver.GetQueryType(r.Context(), o.RID, qtName)
			if err != nil {
				results = append(results, entry)
				continue
			}
			entry.Found = true
			entry.QueryTypeRID = qt.RID
			entry.CanExecute = canExecute
			results = append(results, entry)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(QueryCheckBatchResponse{
			OntologyAPIName: o.APIName,
			Results:         results,
		})
	})
}
