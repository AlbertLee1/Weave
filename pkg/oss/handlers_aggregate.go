package oss

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oss/aggregation"
)

// SetAggregation wires the aggregation engine and index manager so the
// handler can serve the /objects/{objectType}/aggregate endpoint. Pass nil
// (or never call) to disable aggregation; the route will return
// AggregationNotConfigured.
func (h *Handler) SetAggregation(engine *aggregation.Engine, mgr *index.Manager) {
	h.aggEngine = engine
	h.indexMgr = mgr
}

// AggregateObjects handles POST /api/v2/ontologies/{ontologyApiName}/objects/{objectType}/aggregate.
func (h *Handler) AggregateObjects(w http.ResponseWriter, r *http.Request) {
	if h.aggEngine == nil || h.indexMgr == nil {
		apierror.WriteJSON(w, apierror.NewInternal("AggregationNotConfigured", nil))
		return
	}

	ontologyAPIName := chi.URLParam(r, "ontologyApiName")
	objectType := chi.URLParam(r, "objectType")

	var req aggregation.AggregationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidAggregationRequest", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	req.ObjectType = objectType

	idx := h.indexMgr.GetIndex(scopedBleveKey(h.indexMgr, ontologyAPIName, objectType))
	if idx == nil {
		apierror.WriteJSON(w, apierror.NewNotFound("IndexNotFound", map[string]string{
			"ontologyApiName": ontologyAPIName,
			"objectType":      objectType,
		}))
		return
	}

	result, err := h.aggEngine.Aggregate(idx, &req)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("AggregationFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
