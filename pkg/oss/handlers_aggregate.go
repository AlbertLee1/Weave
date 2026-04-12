package oss

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oss/aggregation"
)

// PropertyFilterProvider resolves the column-level allow list for the
// caller on ctx against objectType. Return convention mirrors
// security.Engine.AllowedProperties: a nil slice means "no PROPERTY-scope
// policy attached, every field is allowed", while a non-nil slice
// (including zero-length) is an explicit allow list. Kept as a narrow
// interface so pkg/oss does not import pkg/security directly; cmd/server
// wires a thin adapter that forwards to *security.Engine.
type PropertyFilterProvider interface {
	AllowedProperties(ctx context.Context, objectType string) ([]string, error)
}

// SetAggregation wires the aggregation engine and index manager so the
// handler can serve the /objects/{objectType}/aggregate endpoint. Pass nil
// (or never call) to disable aggregation; the route will return
// AggregationNotConfigured.
func (h *Handler) SetAggregation(engine *aggregation.Engine, mgr *index.Manager) {
	h.aggEngine = engine
	h.indexMgr = mgr
}

// SetPropertyFilterProvider wires the optional US-049 column-level policy
// gate into the aggregation handler. When attached, AggregateObjects
// rejects requests that reference fields outside the caller's allow list
// with 403 + errorName PropertyNotAccessible. A nil provider (or a nil
// return from AllowedProperties) is a no-op — un-policied callers keep
// unrestricted access.
func (h *Handler) SetPropertyFilterProvider(p PropertyFilterProvider) {
	h.propertyFilter = p
}

// rejectFilteredAggregationFields enforces the US-049 column-level gate on
// an AggregationRequest. Returns nil when the request is allowed and a
// populated APIError when a groupBy.field or metric.field is outside the
// caller's allow list. A nil provider, a nil return from AllowedProperties
// ("no PROPERTY-scope policy attached"), or a blank-field metric (e.g.
// count without a target) all short-circuit to "allow".
func (h *Handler) rejectFilteredAggregationFields(ctx context.Context, objectType string, req *aggregation.AggregationRequest) *apierror.APIError {
	if h.propertyFilter == nil {
		return nil
	}
	allowed, err := h.propertyFilter.AllowedProperties(ctx, objectType)
	if err != nil {
		return apierror.NewInternal("PropertyFilterFailed", map[string]string{
			"reason": err.Error(),
		})
	}
	if allowed == nil {
		return nil
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, p := range allowed {
		allowedSet[p] = struct{}{}
	}
	deny := func(field string) *apierror.APIError {
		return apierror.NewPermissionDenied("PropertyNotAccessible", map[string]string{
			"objectType": objectType,
			"property":   field,
		})
	}
	for _, gb := range req.GroupBy {
		if gb.Field == "" {
			continue
		}
		if _, ok := allowedSet[gb.Field]; !ok {
			return deny(gb.Field)
		}
	}
	for _, spec := range req.Aggregations {
		if spec.Field == "" {
			// count and other field-less metrics are unaffected by
			// column visibility.
			continue
		}
		if _, ok := allowedSet[spec.Field]; !ok {
			return deny(spec.Field)
		}
	}
	return nil
}

// AggregateObjects handles POST /api/v2/ontologies/{ontologyApiName}/objects/{objectType}/aggregate.
func (h *Handler) AggregateObjects(w http.ResponseWriter, r *http.Request) {
	if h.aggEngine == nil || h.indexMgr == nil {
		apierror.WriteJSON(w, apierror.NewInternal("AggregationNotConfigured", nil))
		return
	}

	ontologyAPIName := chi.URLParam(r, "ontologyApiName")
	objectType := chi.URLParam(r, "objectType")
	ctx := index.WithOntologyScope(r.Context(), ontologyAPIName)

	var req aggregation.AggregationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidAggregationRequest", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	req.ObjectType = objectType

	if apiErr := h.rejectFilteredAggregationFields(ctx, objectType, &req); apiErr != nil {
		apierror.WriteJSON(w, apiErr)
		return
	}

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
