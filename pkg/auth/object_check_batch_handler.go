package auth

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/liyang/weave/pkg/apierror"
)

// ObjectCheckBatchRequest is the JSON body of
// POST /api/v2/me/checks/objectTypes.
type ObjectCheckBatchRequest struct {
	OntologyAPIName    string   `json:"ontologyApiName"`
	ObjectTypeAPINames []string `json:"objectTypeApiNames"`
}

// ObjectCheckBatchEntry is one row in the bulk response. Found tells
// the SPA whether the object type actually exists in the ontology;
// CanRead/CanWrite reflect the caller's perms when Found is true and
// are both false when Found is false (so the SPA never accidentally
// shows UI for a deleted/renamed type).
type ObjectCheckBatchEntry struct {
	ObjectTypeAPIName string `json:"objectTypeApiName"`
	Found             bool   `json:"found"`
	// ObjectTypeRID is empty when Found is false.
	ObjectTypeRID string `json:"objectTypeRid,omitempty"`
	CanRead       bool   `json:"canRead"`
	CanWrite      bool   `json:"canWrite"`
}

// ObjectCheckBatchResponse is the JSON shape returned by the bulk
// probe. Results preserves the input order so the SPA can correlate
// row N of the response with row N of the request without an
// objectTypeApiName→row map.
type ObjectCheckBatchResponse struct {
	OntologyAPIName string                  `json:"ontologyApiName"`
	Results         []ObjectCheckBatchEntry `json:"results"`
}

// ObjectCheckBatchHandler returns an http.Handler for
// POST /api/v2/me/checks/objectTypes — round-107 Foundry-parity
// bulk object-type applicability probe. Trades the round-105 single
// GET for one POST that resolves N object types in one round-trip
// — the common SPA page-load pattern is "this page renders rows
// for K object types; tell me read/write for all K at once".
//
// Single-ontology scope keeps the design tight (typical SPA pages
// scope themselves to one ontology). Bulk-across-ontologies would
// be a separate endpoint if needed.
//
// Each result carries `found: bool` so the SPA can distinguish
// "object type doesn't exist" from "exists but no permission" —
// the round-105 single endpoint expressed this with HTTP 404, but
// a batch endpoint can't 404 per-entry, so the discriminator moves
// into the payload.
//
// 401 unauthenticated. 400 on malformed body or empty
// objectTypeApiNames. 404 OntologyNotFound when the ontology is
// missing (the entire request fails — can't reason about object
// types without an ontology).
func ObjectCheckBatchHandler(ontResolver OntologyResolver, objResolver ObjectTypeResolver) http.Handler {
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
		var req ObjectCheckBatchRequest
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
		if len(req.ObjectTypeAPINames) == 0 {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
				"reason": "objectTypeApiNames must be a non-empty array",
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
			apierror.WriteJSON(w, apierror.NewInternal("ObjectCheckBatchFailed", nil))
			return
		}

		// Effective role set — compute once, reuse for every entry.
		roles := append([]string(nil), u.Roles...)
		if u.OntologyRoles != nil {
			if scoped := u.OntologyRoles[o.RID]; scoped != "" {
				roles = append(roles, scoped)
			}
		}
		canRead := HasPermission(roles, PermObjectRead)
		canWrite := HasPermission(roles, PermObjectWrite)

		results := make([]ObjectCheckBatchEntry, 0, len(req.ObjectTypeAPINames))
		for _, otName := range req.ObjectTypeAPINames {
			entry := ObjectCheckBatchEntry{ObjectTypeAPIName: otName}
			ot, err := objResolver.GetObjectType(r.Context(), o.RID, otName)
			if err != nil {
				// Per-entry "doesn't exist" — leave CanRead/CanWrite
				// false (no UI for a missing type) regardless of the
				// caller's perms. Found discriminator lets the SPA
				// surface a "type removed from config" hint.
				results = append(results, entry)
				continue
			}
			entry.Found = true
			entry.ObjectTypeRID = ot.RID
			entry.CanRead = canRead
			entry.CanWrite = canWrite
			results = append(results, entry)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(ObjectCheckBatchResponse{
			OntologyAPIName: o.APIName,
			Results:         results,
		})
	})
}
