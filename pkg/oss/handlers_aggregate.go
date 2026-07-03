package oss

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"

	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oss/aggregation"
	"github.com/liyang/weave/pkg/oss/where"
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

// RowPolicyQueryProvider compiles the caller's row-level policy into a Bleve
// query for objectType on ctx. The contract mirrors
// pkg/oss/objectset.PolicyQueryProvider so cmd/server can wire the SAME
// *policyQueryAdapter into both the ObjectSet aggregate path and the direct
// /objects/{type}/aggregate path: return a *query.MatchAllQuery (or nil)
// when no ROW-scope policy is attached, otherwise the compiled filter. Kept
// as a narrow interface so pkg/oss does not import pkg/security / pkg/rls
// directly. An error fails the request closed (500) rather than silently
// aggregating over the whole index.
type RowPolicyQueryProvider interface {
	PolicyQuery(ctx context.Context, objectType string) (query.Query, error)
}

// SetRowPolicyQueryProvider wires the row-level policy gate into the
// aggregation handler. When attached, AggregateObjects pushes the compiled
// policy query down as the aggregation base query (AND-combined with the
// caller's where), so count/sum/avg only ever observe rows the caller is
// permitted to read. A nil provider leaves the legacy MatchAll behaviour
// untouched.
func (h *Handler) SetRowPolicyQueryProvider(p RowPolicyQueryProvider) {
	h.rowPolicyProvider = p
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
	for _, field := range aggregationWhereFields(req.Where) {
		if _, ok := allowedSet[field]; !ok {
			return deny(field)
		}
	}
	return rejectFilteredSubAggregationFields(req.SubAggregations, allowedSet, deny)
}

func rejectFilteredSubAggregationFields(subs []aggregation.SubAggregationSpec, allowedSet map[string]struct{}, deny func(string) *apierror.APIError) *apierror.APIError {
	for _, sub := range subs {
		for _, gb := range sub.GroupBy {
			if gb.Field == "" {
				continue
			}
			if _, ok := allowedSet[gb.Field]; !ok {
				return deny(gb.Field)
			}
		}
		for _, spec := range sub.Aggregations {
			if spec.Field == "" {
				continue
			}
			if _, ok := allowedSet[spec.Field]; !ok {
				return deny(spec.Field)
			}
		}
		if apiErr := rejectFilteredSubAggregationFields(sub.SubAggregations, allowedSet, deny); apiErr != nil {
			return apiErr
		}
	}
	return nil
}

func aggregationWhereFields(clause *where.WhereClause) []string {
	fields := map[string]struct{}{}
	collectAggregationWhereFields(clause, fields)
	out := make([]string, 0, len(fields))
	for field := range fields {
		out = append(out, field)
	}
	sort.Strings(out)
	return out
}

func collectAggregationWhereFields(clause *where.WhereClause, fields map[string]struct{}) {
	if clause == nil {
		return
	}
	switch clause.Type {
	case "and", "or":
		var subs []where.WhereClause
		if err := json.Unmarshal(clause.Value, &subs); err != nil {
			return
		}
		for i := range subs {
			collectAggregationWhereFields(&subs[i], fields)
		}
	case "not":
		// Keep the column-visibility walker aligned with pkg/oss/where's
		// executable semantics: `not` accepts the Palantir V2 single-element
		// array form as well as the older single-object form. Missing the
		// array form would let hidden fields influence aggregate counts before
		// the PropertyNotAccessible gate sees them.
		var subs []where.WhereClause
		if err := json.Unmarshal(clause.Value, &subs); err == nil && len(subs) > 0 {
			collectAggregationWhereFields(&subs[0], fields)
			return
		}
		var sub where.WhereClause
		if err := json.Unmarshal(clause.Value, &sub); err != nil {
			return
		}
		collectAggregationWhereFields(&sub, fields)
	default:
		if clause.Field != "" {
			fields[clause.Field] = struct{}{}
		}
	}
}

func rejectUnsupportedAggregationWhere(req *aggregation.AggregationRequest) *apierror.APIError {
	if req == nil || req.Where == nil {
		return nil
	}
	if where.HasRegexClause(req.Where) {
		return apierror.NewInvalidParameter("AggregationWhereRegexUnsupported", map[string]string{
			"reason": "regex where clauses are not supported for aggregation until timeout-safe execution is available",
		})
	}
	if _, err := where.ConvertToBleveQuery(req.Where); err != nil {
		return apierror.NewInvalidParameter("InvalidAggregationWhere", map[string]string{
			"reason": err.Error(),
		})
	}
	return nil
}

// AggregateObjects handles POST /api/v2/ontologies/{ontologyApiName}/objects/{objectType}/aggregate.
func (h *Handler) AggregateObjects(w http.ResponseWriter, r *http.Request) {
	ontologyAPIName := chi.URLParam(r, "ontologyApiName")
	objectType := chi.URLParam(r, "objectType")
	ctx := index.WithOntologyScope(r.Context(), ontologyAPIName)

	overlay, apiErr := h.loadScenarioOverlay(ctx, r, ontologyAPIName)
	if apiErr != nil {
		apierror.WriteJSON(w, apiErr)
		return
	}

	// The Bleve-backed engine is required for the base path; the overlay
	// path runs in pure Go and does not need it.
	if overlay == nil && (h.aggEngine == nil || h.indexMgr == nil) {
		apierror.WriteJSON(w, apierror.NewInternal("AggregationNotConfigured", nil))
		return
	}

	var req aggregation.AggregationRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
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

	if apiErr := rejectUnsupportedAggregationWhere(&req); apiErr != nil {
		apierror.WriteJSON(w, apiErr)
		return
	}

	if overlay != nil {
		// Scenario overlay path: load base rows via the Service layer (one
		// page hop here; pagination follow-up is acknowledged in PRD Open
		// Questions), fold edits over them, run in-memory aggregation.
		page, err := h.svc.ListObjects(ctx, ListObjectsRequest{
			OntologyRID: ontologyAPIName,
			ObjectType:  objectType,
		})
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("ScenarioBaseFetchFailed", map[string]string{
				"reason": err.Error(),
			}))
			return
		}
		var base []*WireObject
		if page != nil {
			base = page.Data
		}
		result, conflicts, err := AggregateWithOverlayAndConflicts(base, overlay.Edits, &req)
		h.scenarioConflictAuditor.Record(ctx, overlay.Scenario.RID, "aggregate", conflicts)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("ScenarioAggregationFailed", map[string]string{
				"reason": err.Error(),
			}))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
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

	// Row-level policy pushdown: compile the caller's policy into a Bleve
	// base query so the facet engine only ever scans rows the caller may
	// read. A nil provider leaves baseQuery nil, which AggregateWithQuery
	// treats as MatchAll — identical to the legacy unrestricted path.
	var baseQuery query.Query
	if h.rowPolicyProvider != nil {
		policyQ, perr := h.rowPolicyProvider.PolicyQuery(ctx, objectType)
		if perr != nil {
			apierror.WriteJSON(w, apierror.NewInternal("RowPolicyEvaluationFailed", map[string]string{
				"reason": perr.Error(),
			}))
			return
		}
		baseQuery = policyQ
	}

	result, err := h.aggEngine.AggregateWithQuery(idx, baseQuery, &req)
	if err != nil {
		// Foundry parity: accuracy=REQUIRE_ACCURATE that cannot be satisfied
		// (scan truncated / scanned rows over the approximate threshold) is a
		// client-visible 4xx, not an opaque 500. errors.Is unwraps the
		// sub-aggregation wrapping so a truncated child still maps here.
		if errors.Is(err, aggregation.ErrAccuracyNotGuaranteed) {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("AccuracyNotGuaranteed", map[string]string{
				"reason": err.Error(),
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("AggregationFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
