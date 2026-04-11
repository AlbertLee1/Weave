package main

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
)

// AdminIndexRebuildDeps is the dependency set for the admin index rebuild
// handler. Each field is optional: a zero-value IndexMgr means the server is
// running without a Bleve backend, and the handler returns 503; a zero-value
// DocSource means the rebuild will recreate the index shell but leave it
// empty (useful when WEAVE_DATA_DIR was wiped and operators only need to
// unblock queries).
type AdminIndexRebuildDeps struct {
	IndexMgr  *index.Manager
	Repo      index.RebuildRepo
	DocSource index.LatestDocumentSource
}

// AdminIndexRebuildRequest is the wire shape of POST /api/admin/indexes/rebuild.
type AdminIndexRebuildRequest struct {
	Ontology   string `json:"ontology"`
	ObjectType string `json:"objectType"`
}

// AdminIndexRebuildResponse is the success shape returned on 200.
type AdminIndexRebuildResponse struct {
	ScopedKey    string `json:"scopedKey"`
	IndexedCount int    `json:"indexedCount"`
}

// NewAdminIndexRebuildHandler builds the HTTP handler that rebuilds a single
// ObjectType's Bleve index. The handler does NOT enforce authentication on
// its own; the surrounding router is expected to wrap it in
// auth.RequirePermission so only admins can call it.
func NewAdminIndexRebuildHandler(deps AdminIndexRebuildDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if deps.IndexMgr == nil || deps.Repo == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"errorCode": "SERVICE_UNAVAILABLE",
				"errorName": "IndexRebuildNotConfigured",
			})
			return
		}

		var req AdminIndexRebuildRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
				"reason": "invalid JSON",
			}))
			return
		}
		if req.Ontology == "" {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:ontology", map[string]string{
				"parameter": "ontology",
				"reason":    "ontology is required",
			}))
			return
		}
		if req.ObjectType == "" {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:objectType", map[string]string{
				"parameter": "objectType",
				"reason":    "objectType is required",
			}))
			return
		}

		res, err := index.Rebuild(r.Context(), deps.IndexMgr, deps.Repo, deps.DocSource, index.RebuildRequest{
			OntologyAPIName:   req.Ontology,
			ObjectTypeAPIName: req.ObjectType,
		})
		if err != nil {
			if errors.Is(err, oms.ErrNotFound) {
				apierror.WriteJSON(w, apierror.NewNotFound("IndexRebuildTargetNotFound", map[string]string{
					"ontology":   req.Ontology,
					"objectType": req.ObjectType,
				}))
				return
			}
			apierror.WriteJSON(w, apierror.NewInternal("IndexRebuildFailed", map[string]string{
				"reason": err.Error(),
			}))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(AdminIndexRebuildResponse{
			ScopedKey:    res.ScopedKey,
			IndexedCount: res.IndexedCount,
		})
	})
}
