package objectset

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/oss/aggregation"
	"github.com/liyang/weave/pkg/oss/pagination"
	"github.com/liyang/weave/pkg/oss/where"
)

// LoadObjectSetRequest is the Palantir V2 request format for loadObjects.
type LoadObjectSetRequest struct {
	ObjectSet *Definition `json:"objectSet"`
	Select    []string    `json:"select,omitempty"`
	OrderBy   *OrderBy    `json:"orderBy,omitempty"`
	PageSize  int         `json:"pageSize,omitempty"`
	PageToken string      `json:"pageToken,omitempty"`
	Snapshot  bool        `json:"snapshot,omitempty"`
}

// OrderBy is the Foundry SearchOrderByV2 ordering shape. Aliased to the
// pkg/oss definition so search and loadObjects share one validation path
// (oss.OrderBy.BleveSortOrder).
type OrderBy = oss.OrderBy

// OrderByField specifies a single field ordering ("asc"/"desc", default
// "asc"). Aliased to the pkg/oss definition.
type OrderByField = oss.OrderByField

// LoadObjectSetResponse is the Palantir V2 response format with string totalCount.
type LoadObjectSetResponse struct {
	Data          []*oss.WireObject `json:"data"`
	NextPageToken string            `json:"nextPageToken,omitempty"`
	TotalCount    string            `json:"totalCount,omitempty"`
	// TotalCountAccuracy is "EXACT" when the ObjectSet was fully materialized
	// and "APPROXIMATE" when the executor hit its hard cap (10000 PKs) and
	// truncated the result. Callers should warn the user that totalCount and
	// data are partial when this is "APPROXIMATE".
	TotalCountAccuracy string `json:"totalCountAccuracy,omitempty"`
}

// CreateTemporaryRequest is the request for creating a temporary ObjectSet.
type CreateTemporaryRequest struct {
	ObjectSet *Definition `json:"objectSet"`
}

// CreateTemporaryResponse is the response for creating a temporary ObjectSet.
type CreateTemporaryResponse struct {
	ObjectSetRID string `json:"objectSetRid"`
}

// PropertyFilterProvider returns the set of property API names that the
// caller in ctx is permitted to see on objectType. The return convention
// mirrors security.Engine.AllowedProperties: a nil slice means "no
// PROPERTY-scope policy attached, allow all fields" and a non-nil slice
// (including zero-length) is an explicit allow list that downstream
// WireObject serialization must enforce by omitting unlisted fields. Kept
// as an interface so pkg/oss/objectset avoids a direct pkg/security import;
// cmd/server wires a thin adapter that forwards to *security.Engine.
type PropertyFilterProvider interface {
	AllowedProperties(ctx context.Context, objectType string) ([]string, error)
}

// DataAccessAuditor records successful loadObjectSet reads for the US-264
// per-ObjectType audit toggle. RecordLoadObjectSet is a best-effort sink —
// implementations decide whether the target ObjectType has opted in and
// silently drop rows for opted-out types. Kept as an interface so
// pkg/oss/objectset does not need to import pkg/audit or pkg/oms directly;
// cmd/server wires a thin adapter that forwards to oss.DataAccessAuditor.
type DataAccessAuditor interface {
	RecordLoadObjectSet(ctx context.Context, ontologyRID, objectTypeAPIName string, details map[string]any)
}

// Handler handles ObjectSet HTTP requests.
type Handler struct {
	executor           *Executor
	indexMgr           *index.Manager
	store              *Store
	aggEngine          *aggregation.Engine
	propertyFilter     PropertyFilterProvider
	historySnapshots   HistorySnapshotProvider
	txResolver         TransactionResolver
	branchScopes       BranchScopeProvider
	persistedSnapshots PersistedSnapshotStore
	dataAccessAuditor  DataAccessAuditor
	markingFilter      MarkingFilterProvider
}

// MarkingFilterProvider enforces Foundry-style mandatory marking access
// control (subset / AND semantics) on the objectSets/loadObjects path. The
// executor's row-policy pushdown already applies the loose marking-OVERLAP
// clause via PolicyQueryProvider, correct for single-valued marking fields;
// this provider adds the strict per-row subset refinement that multi-valued
// markings need (an object marked {A,B} is hidden from a caller holding only
// {A}). Kept narrow so pkg/oss/objectset does not import pkg/security; cmd/
// server wires a thin adapter.
type MarkingFilterProvider interface {
	// MarkingsEnabled reports whether objectType opts into marking filtering,
	// so the handler knows it must fetch the reserved _markings field even
	// when the caller's select list omits it (otherwise the subset check
	// would fail open).
	MarkingsEnabled(ctx context.Context, objectType string) bool
	// FilterByMarkings drops every object whose _markings set is NOT a subset
	// of the caller's markings. A no-op (returns input) when markings are not
	// enabled for objectType or the slice is empty.
	FilterByMarkings(ctx context.Context, objectType string, objs []*oss.WireObject) []*oss.WireObject
}

// markingField is the reserved Bleve keyword field the Funnel consumer writes
// each object's marking set into. Mirrors pkg/security.MarkingField; kept
// local so pkg/oss/objectset does not import pkg/security.
const markingField = "_markings"

// NewHandler creates a new ObjectSet handler.
func NewHandler(executor *Executor, indexMgr *index.Manager, store *Store) *Handler {
	return &Handler{
		executor:  executor,
		indexMgr:  indexMgr,
		store:     store,
		aggEngine: aggregation.NewEngine(),
	}
}

// SetPropertyFilterProvider wires the optional US-048 column-level
// visibility hook. When attached, every Load path (LoadObjects, LoadLinks,
// loadObjectSet) runs its result through the provider and strips any
// WireObject property not in the returned allow list before serialization.
// Passing nil detaches the hook. Safe to call at any point during server
// boot; the Handler re-reads the field on every request.
func (h *Handler) SetPropertyFilterProvider(p PropertyFilterProvider) {
	h.propertyFilter = p
}

// SetMarkingFilterProvider wires the optional mandatory-marking subset gate.
// When attached, LoadObjects fetches the reserved _markings field and drops
// rows whose markings are not a subset of the caller's before serialization.
// Passing nil detaches the hook (back-compat: markings then rely solely on the
// executor's overlap-query pushdown).
func (h *Handler) SetMarkingFilterProvider(p MarkingFilterProvider) {
	h.markingFilter = p
}

// SetDataAccessAuditor wires the optional US-264 loadObjectSet audit sink.
// When attached and the target ObjectType has opted in via AuditDataAccess,
// every successful LoadObjects call emits an audit_events row (action =
// "data.access"). Passing nil detaches the hook.
func (h *Handler) SetDataAccessAuditor(a DataAccessAuditor) {
	h.dataAccessAuditor = a
}

// SetHistorySnapshotProvider wires the optional US-223 time-travel reader.
// When attached, LoadObjects honours the `?asOf=<RFC3339>` query parameter
// by routing through the provider instead of the live Bleve index. Passing
// nil detaches the hook (asOf requests then return 501). The reader is only
// consulted for "base" ObjectSet definitions; composite types (filter,
// union, intersect, ...) reject asOf with a 400 because Bleve has no
// per-instant snapshot to filter against.
func (h *Handler) SetHistorySnapshotProvider(p HistorySnapshotProvider) {
	h.historySnapshots = p
}

// SetTransactionResolver wires the optional US-379 tx_id → committed_at
// resolver. When attached, LoadObjects honours `?asOf=tx-<id>` by
// resolving the transaction's commit timestamp and routing it through
// the existing US-223 history-snapshot scan. Passing nil detaches the
// hook (tx-id asOf requests then return TransactionLookupUnavailable
// 400). The RFC3339 asOf path is unaffected and works without a
// resolver wired.
func (h *Handler) SetTransactionResolver(r TransactionResolver) {
	h.txResolver = r
}

// SetBranchScopeProvider wires the optional US-381 branch overlay. When
// attached, LoadObjects honours `?branch=<name>` by routing the live
// executor result through the provider, which returns the PK set visible
// on that branch (branch-only additions plus base PKs minus branch
// deletions). Passing nil detaches the hook — non-default branches then
// surface as BranchLookupUnavailable 400. The default branch ("main") is
// never sent to the provider; that path stays byte-for-byte identical to
// the pre-US-381 behavior.
func (h *Handler) SetBranchScopeProvider(p BranchScopeProvider) {
	h.branchScopes = p
}

// applyPropertyVisibility is the Handler-side chokepoint that US-048
// column-level policies flow through. It resolves the allow list for the
// caller via the wired PropertyFilterProvider and filters every object in
// objs via WireObject.FilterProperties. A nil provider, nil allowed list,
// or empty input slice short-circuits to the input unchanged so existing
// back-compat tests don't pay the copy cost. Errors surface unchanged so
// callers can emit the proper apierror response.
func (h *Handler) applyPropertyVisibility(ctx context.Context, objectType string, objs []*oss.WireObject) ([]*oss.WireObject, error) {
	if h.propertyFilter == nil || len(objs) == 0 {
		return objs, nil
	}
	allowed, err := h.propertyFilter.AllowedProperties(ctx, objectType)
	if err != nil {
		return nil, err
	}
	if allowed == nil {
		return objs, nil
	}
	out := make([]*oss.WireObject, len(objs))
	for i, o := range objs {
		out[i] = o.FilterProperties(allowed)
	}
	return out, nil
}

// executeError maps an executor error to a typed APIError.
//
//   - ErrQueryTooLarge → 422 WEAVE_QUERY_TOO_LARGE (US-366 multi-hop
//     searchAround intermediate-cap breach)
//   - ErrInvalidObjectSetDefinition OR where.ErrInvalidWhereClause →
//     400 InvalidObjectSet (round 37 wire-shape sentinel: definition
//     shape problems + bad where clauses are user-side)
//   - any other error → 500 ObjectSetFailed (round 37 fix: was 400
//     INVALID_ARGUMENT, but Bleve/PG outages and policy-resolver
//     failures are server-side)
//
// where.ErrInvalidWhereClause from round 36 already gets wrapped at
// the converter; the executor's `%w` chain through executeFilter /
// executeBase preserves it so errors.Is at the handler boundary sees
// both sentinels.
func executeError(err error) *apierror.APIError {
	if errors.Is(err, ErrQueryTooLarge) {
		return apierror.NewQueryTooLarge("SearchAroundQueryTooLarge", map[string]string{
			"error": err.Error(),
			"cap":   strconv.Itoa(SearchAroundIntermediateCap),
		})
	}
	if errors.Is(err, ErrInvalidObjectSetDefinition) || errors.Is(err, where.ErrInvalidWhereClause) {
		return apierror.NewInvalidParameter("InvalidObjectSet", map[string]string{"error": err.Error()})
	}
	return apierror.NewInternal("ObjectSetFailed", map[string]string{"error": err.Error()})
}

// LoadObjects handles POST /api/v2/ontologies/{ont}/objectSets/loadObjects.
func (h *Handler) LoadObjects(w http.ResponseWriter, r *http.Request) {
	var req LoadObjectSetRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{"error": err.Error()}))
		return
	}

	if req.ObjectSet == nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingObjectSet", nil))
		return
	}

	// Foundry V2: select is REQUIRED
	if len(req.Select) == 0 {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("SelectRequired", map[string]string{
			"reason": "LoadObjectSetRequestV2.select is required and must be a non-empty array of property apiNames",
		}))
		return
	}

	// Foundry V2: honour LoadObjectSetRequestV2.orderBy (SearchOrderByV2
	// shape). Validate before any execution so bad input fails fast.
	// orderType "relevance" is rejected here: the executor resolves the
	// ObjectSet to explicit primary keys, so every doc would score
	// identically and relevance ordering would be a silent no-op.
	if req.OrderBy.IsRelevance() {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidOrderBy", map[string]string{
			"reason": `orderType "relevance" is not supported on loadObjects: an ObjectSet resolves to explicit primary keys with uniform scores; use fields ordering instead`,
		}))
		return
	}
	sortOrder, obErr := req.OrderBy.BleveSortOrder()
	if obErr != nil {
		apierror.WriteJSON(w, obErr)
		return
	}

	// Stamp the ontology scope on the context so the executor and downstream
	// Bleve lookups use per-ontology index keys (US-044).
	ontologyAPIName := chi.URLParam(r, "ontologyApiName")
	ctx := WithOntologyScope(r.Context(), ontologyAPIName)

	// US-381: ?branch= scopes the read to a non-default branch overlay. The
	// parameter defaults to DefaultBranch ("main"); any other value is
	// stamped on the context (so HistorySnapshotProvider implementations
	// can opt into branch-aware reads) and routed through the wired
	// BranchScopeProvider on the live path. With no provider wired the
	// non-main path returns BranchLookupUnavailable 400 instead of
	// silently degrading to the main branch.
	branch, apiErr := resolveBranch(branchFromRequest(r))
	if apiErr != nil {
		apierror.WriteJSON(w, apiErr)
		return
	}
	ctx = WithBranchScope(ctx, branch)
	if branch != DefaultBranch && h.branchScopes == nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("BranchLookupUnavailable", map[string]string{
			"branch": branch,
			"reason": "branch scope provider is not configured on this server",
		}))
		return
	}

	// US-223 / US-379: ?asOf= short-circuits to the time-travel path. The
	// parameter accepts either an RFC3339 timestamp (US-223) or a
	// "tx-<id>" reference into dataset_transactions (US-379) which the
	// wired TransactionResolver maps to the committed_at instant. We then
	// scan object_history for the snapshot covering that instant and skip
	// the Bleve fetch entirely. Only "base" ObjectSets are supported
	// because composite types (filter / union / ...) need a per-instant
	// Bleve index that we don't materialize.
	if asOfRaw := r.URL.Query().Get("asOf"); asOfRaw != "" {
		// The time-travel path reads object_history snapshots, not the live
		// Bleve index, so there is nothing to push the sort down to. Reject
		// the combination explicitly instead of silently returning PK order.
		if len(sortOrder) > 0 {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("OrderByUnsupportedWithAsOf", map[string]string{
				"asOf":   asOfRaw,
				"reason": "orderBy is not supported together with asOf time-travel reads; snapshot pages are ordered by primary key",
			}))
			return
		}
		asOf, apiErr := h.resolveAsOf(ctx, asOfRaw)
		if apiErr != nil {
			apierror.WriteJSON(w, apiErr)
			return
		}
		h.loadObjectsAsOf(w, r, ctx, ontologyAPIName, &req, asOf)
		return
	}

	// Execute the ObjectSet to get PKs
	result, err := h.executor.Execute(ctx, req.ObjectSet)
	if err != nil {
		apierror.WriteJSON(w, executeError(err))
		return
	}

	// US-381: rewrite the executor's PrimaryKeys for non-default branches.
	// The provider returns the authoritative PK set visible on the branch;
	// branch-only adds, branch-deletions, and branch-substitutions all
	// flow through the same hook so the rest of the load pipeline stays
	// branch-oblivious.
	if branch != DefaultBranch {
		scoped, scopeErr := h.branchScopes.ScopeObjectSet(ctx, branch, ontologyAPIName, result.ObjectType, result.PrimaryKeys)
		if scopeErr != nil {
			apierror.WriteJSON(w, branchScopeError(branch, scopeErr))
			return
		}
		result.PrimaryKeys = scoped
	}

	// Foundry V2 orderBy: reorder the FULL PK set before the pagination
	// slice below, so the ordering is global and stable across pages. The
	// sort is pushed down to Bleve (see sortPrimaryKeys); ObjectSets larger
	// than orderByMaxKeys get an explicit 422 rather than a silent unsorted
	// response.
	if len(sortOrder) > 0 && len(result.PrimaryKeys) > 1 {
		if len(result.PrimaryKeys) > orderByMaxKeys {
			apierror.WriteJSON(w, orderByTooLarge(len(result.PrimaryKeys)))
			return
		}
		sorted, sortErr := h.sortPrimaryKeys(ctx, result.ObjectType, result.PrimaryKeys, sortOrder)
		if sortErr != nil {
			apierror.WriteJSON(w, apierror.NewInternal("OrderByFailed", map[string]string{"error": sortErr.Error()}))
			return
		}
		result.PrimaryKeys = sorted
	}

	// Apply pagination
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 100
	}
	if pageSize > 1000 {
		pageSize = 1000
	}

	offset := 0
	if req.PageToken != "" {
		cursor, err := pagination.DecodeCursor(req.PageToken)
		if err == nil {
			offset = cursor.Offset
		}
	}

	totalCount := len(result.PrimaryKeys)

	// Slice for current page
	start := offset
	if start > totalCount {
		start = totalCount
	}
	end := start + pageSize
	if end > totalCount {
		end = totalCount
	}
	pagePKs := result.PrimaryKeys[start:end]

	// Load full objects from Bleve
	data := make([]*oss.WireObject, 0, len(pagePKs))

	// Determine which fields to request
	fields := []string{"*"}
	if len(req.Select) > 0 {
		fields = req.Select
	}

	// Mandatory-marking subset filtering needs each object's _markings set. A
	// caller-supplied select list may omit it; append it (so the subset check
	// can't fail open) and remember to strip it from the response afterward.
	// The default ["*"] path already carries _markings, so no addition needed.
	markingFieldAdded := false
	if h.markingFilter != nil && len(req.Select) > 0 && h.markingFilter.MarkingsEnabled(ctx, result.ObjectType) {
		hasMarkings := false
		for _, f := range fields {
			if f == markingField {
				hasMarkings = true
				break
			}
		}
		if !hasMarkings {
			fields = append(append([]string{}, fields...), markingField)
			markingFieldAdded = true
		}
	}

	for _, pk := range pagePKs {
		searchReq := bleve.NewSearchRequest(bleve.NewDocIDQuery([]string{pk}))
		searchReq.Fields = fields
		searchReq.Size = 1

		res, err := h.indexMgr.Search(scopedIndexKey(ctx, h.indexMgr, result.ObjectType), searchReq)
		if err != nil || len(res.Hits) == 0 {
			continue
		}

		props := res.Hits[0].Fields
		if len(req.Select) > 0 {
			filtered := make(map[string]interface{})
			for _, f := range req.Select {
				if v, ok := props[f]; ok {
					filtered[f] = v
				}
			}
			// Preserve _markings through select-filtering when it was added
			// solely for the subset check; stripped after FilterByMarkings.
			if markingFieldAdded {
				if v, ok := props[markingField]; ok {
					filtered[markingField] = v
				}
			}
			props = filtered
		}

		if derived, ok := result.DerivedValues[pk]; ok {
			if props == nil {
				props = make(map[string]interface{}, len(derived))
			}
			for k, v := range derived {
				props[k] = v
			}
		}

		// US-210: surface per-edge properties produced by a searchAround
		// step. Values are injected under "__edge" to avoid colliding with
		// object properties. Absent when the traversal wasn't searchAround
		// or no edge carried properties.
		if edge, ok := result.EdgeProperties[pk]; ok && len(edge) > 0 {
			if props == nil {
				props = make(map[string]interface{}, 1)
			}
			props["__edge"] = edge
		}

		data = append(data, oss.FormatObject(result.ObjectType, pk, props))
	}

	// Mandatory-marking subset filter: drop rows whose markings are not a
	// subset of the caller's (multi-valued AND semantics) before any
	// column-level visibility pass. No-op when no provider is wired or the
	// ObjectType is not markings-enabled.
	if h.markingFilter != nil {
		data = h.markingFilter.FilterByMarkings(ctx, result.ObjectType, data)
		if markingFieldAdded {
			for _, obj := range data {
				if obj != nil && obj.Properties != nil {
					delete(obj.Properties, markingField)
				}
			}
		}
	}

	// US-048: drop property fields the caller is not permitted to see. No-op
	// when no PROPERTY-scope policy is attached to result.ObjectType.
	data, err = h.applyPropertyVisibility(ctx, result.ObjectType, data)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("PropertyFilterFailed", map[string]string{"error": err.Error()}))
		return
	}

	accuracy := "EXACT"
	if result.Truncated {
		accuracy = "APPROXIMATE"
	}
	resp := &LoadObjectSetResponse{
		Data:               data,
		TotalCount:         strconv.Itoa(totalCount),
		TotalCountAccuracy: accuracy,
	}

	// Set next page token
	if end < totalCount {
		nextCursor := &pagination.Cursor{Offset: end}
		resp.NextPageToken = nextCursor.Encode()
	}

	if h.dataAccessAuditor != nil {
		h.dataAccessAuditor.RecordLoadObjectSet(ctx, ontologyAPIName, result.ObjectType, map[string]any{
			"count":      len(data),
			"totalCount": totalCount,
			"truncated":  result.Truncated,
		})
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// BranchHeader is the request header that overrides the default
// branch for read paths (PRD-V2 Gap-T4, round 39). Mirrors
// oms.BranchHeader; duplicated here to avoid a cross-package import
// from pkg/oss/objectset → pkg/oms.
const BranchHeader = "X-Weave-Branch"

// branchFromRequest is the round-39 sibling of oms.ResolveBranch
// FromRequest. Returns the raw, untrimmed branch input from either
// ?branch= query parameter (precedent — wins when both are set) or
// the X-Weave-Branch header (fallback). Returns empty string when
// neither is present so the caller's resolveBranch logic can keep
// its "empty → DefaultBranch" short-circuit.
func branchFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	if q := r.URL.Query().Get("branch"); q != "" {
		return q
	}
	return r.Header.Get(BranchHeader)
}

// resolveBranch normalises the branch input (US-381 ?branch= query
// + round-39 X-Weave-Branch header). An empty or whitespace-only
// value resolves to DefaultBranch ("main") so callers that omit
// both keep their pre-US-381 behavior. A non-empty value with
// leading/trailing whitespace is rejected as InvalidBranch rather
// than silently trimmed — branch identifiers are user-visible
// labels and a stray space almost always indicates a client bug.
// Length is capped at 128 chars to keep audit log lines bounded;
// matches the same defensive bound the OMS branch model enforces.
func resolveBranch(raw string) (string, *apierror.APIError) {
	if raw == "" {
		return DefaultBranch, nil
	}
	if strings.TrimSpace(raw) != raw {
		return "", apierror.NewInvalidParameter("InvalidBranch", map[string]string{
			"branch": raw,
			"reason": "branch must not contain leading or trailing whitespace",
		})
	}
	if len(raw) > 128 {
		return "", apierror.NewInvalidParameter("InvalidBranch", map[string]string{
			"branch": raw,
			"reason": "branch identifier exceeds 128 characters",
		})
	}
	return raw, nil
}

// branchScopeError maps a BranchScopeProvider error to a typed APIError.
// ErrBranchNotFound surfaces as a clean BranchNotFound 400; every other
// error becomes BranchScopeFailed so configuration mistakes stay visible
// without exposing the specific 404 code to SDK callers.
func branchScopeError(branch string, err error) *apierror.APIError {
	if errors.Is(err, ErrBranchNotFound) {
		return apierror.NewInvalidParameter("BranchNotFound", map[string]string{
			"branch": branch,
			"reason": "no ontology branch with this name",
		})
	}
	return apierror.NewInternal("BranchScopeFailed", map[string]string{
		"branch": branch,
		"error":  err.Error(),
	})
}

// resolveAsOf normalises the ?asOf= query parameter into the timestamp
// the history-snapshot scan should target. The parameter accepts two wire
// formats:
//
//   - RFC3339 timestamp ("2026-01-15T00:00:00Z") — the US-223 default.
//   - "tx-<id>" reference into dataset_transactions — US-379 lookup that
//     resolves to the matching CommittedAt instant via the wired
//     TransactionResolver. A missing resolver surfaces as
//     TransactionLookupUnavailable; an unknown tx_id surfaces as
//     TransactionNotFound; any other resolver error becomes
//     TimeTravelFailed.
//
// Errors return a non-nil *apierror.APIError so the caller can write the
// envelope without parsing branches itself.
func (h *Handler) resolveAsOf(ctx context.Context, asOfRaw string) (time.Time, *apierror.APIError) {
	if strings.HasPrefix(asOfRaw, "tx-") {
		if h.txResolver == nil {
			return time.Time{}, apierror.NewInvalidParameter("TransactionLookupUnavailable", map[string]string{
				"asOf":   asOfRaw,
				"reason": "transaction resolver is not configured on this server",
			})
		}
		ts, err := h.txResolver.ResolveTransaction(ctx, asOfRaw)
		if err != nil {
			if errors.Is(err, ErrTransactionNotFound) {
				return time.Time{}, apierror.NewInvalidParameter("TransactionNotFound", map[string]string{
					"txId":   asOfRaw,
					"reason": "no dataset transaction with this id",
				})
			}
			return time.Time{}, apierror.NewInternal("TimeTravelFailed", map[string]string{
				"asOf":  asOfRaw,
				"error": err.Error(),
			})
		}
		return ts, nil
	}
	asOf, err := time.Parse(time.RFC3339, asOfRaw)
	if err != nil {
		return time.Time{}, apierror.NewInvalidParameter("InvalidAsOf", map[string]string{
			"asOf":   asOfRaw,
			"reason": "asOf must be an RFC3339 timestamp (e.g. 2026-01-01T00:00:00Z) or a transaction id (tx-<uuid>)",
		})
	}
	return asOf, nil
}

// loadObjectsAsOf serves the US-223 time-travel branch of LoadObjects. It
// resolves the ObjectSet to a single base ObjectType, asks the wired
// HistorySnapshotProvider for every PK whose [valid_from, valid_to)
// interval covers asOf, then applies select / pagination exactly like the
// live path. Errors before any data is written so the response stays a
// regular JSON envelope.
func (h *Handler) loadObjectsAsOf(w http.ResponseWriter, r *http.Request, ctx context.Context, ontologyAPIName string, req *LoadObjectSetRequest, asOf time.Time) {
	if h.historySnapshots == nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("TimeTravelUnavailable", map[string]string{
			"reason": "history snapshot provider is not configured on this server",
		}))
		return
	}
	if req.ObjectSet.Type != "base" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("TimeTravelUnsupportedObjectSet", map[string]string{
			"objectSetType": req.ObjectSet.Type,
			"reason":        "asOf time-travel currently only supports base ObjectSet definitions",
		}))
		return
	}
	if req.ObjectSet.ObjectType == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingObjectType", map[string]string{
			"reason": "base ObjectSet requires objectType for asOf time-travel",
		}))
		return
	}

	snapshots, err := h.historySnapshots.SnapshotObjectsAt(ctx, ontologyAPIName, req.ObjectSet.ObjectType, asOf)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("TimeTravelFailed", map[string]string{
			"asOf":  asOf.Format(time.RFC3339),
			"error": err.Error(),
		}))
		return
	}

	// US-381: when the request also carries `?branch=`, post-filter the
	// snapshot list through the wired BranchScopeProvider so branch
	// overlays remain visible even on time-travel reads. The provider
	// receives the snapshot PKs as the live set; branch deletions /
	// substitutions are honored by intersecting against the returned
	// authoritative set. Branch-only adds that the snapshot path can't
	// produce (the provider would emit PKs not present in snapshots) are
	// silently dropped here — the caller only sees rows the history scan
	// already materialised. The default-branch path skips this hook.
	branch := BranchScopeFromContext(ctx)
	if branch != DefaultBranch && h.branchScopes != nil {
		livePKs := make([]string, len(snapshots))
		for i, snap := range snapshots {
			livePKs[i] = snap.PrimaryKey
		}
		scoped, scopeErr := h.branchScopes.ScopeObjectSet(ctx, branch, ontologyAPIName, req.ObjectSet.ObjectType, livePKs)
		if scopeErr != nil {
			apierror.WriteJSON(w, branchScopeError(branch, scopeErr))
			return
		}
		allowed := make(map[string]struct{}, len(scoped))
		for _, pk := range scoped {
			allowed[pk] = struct{}{}
		}
		filtered := snapshots[:0]
		for _, snap := range snapshots {
			if _, ok := allowed[snap.PrimaryKey]; ok {
				filtered = append(filtered, snap)
			}
		}
		snapshots = filtered
	}

	// Sort PKs ASC for stable pagination. The live path inherits Bleve's
	// internal order; the asOf path has no equivalent so deterministic-by-PK
	// is the safest default.
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].PrimaryKey < snapshots[j].PrimaryKey
	})

	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 100
	}
	if pageSize > 1000 {
		pageSize = 1000
	}
	offset := 0
	if req.PageToken != "" {
		if cursor, err := pagination.DecodeCursor(req.PageToken); err == nil {
			offset = cursor.Offset
		}
	}
	totalCount := len(snapshots)
	start := offset
	if start > totalCount {
		start = totalCount
	}
	end := start + pageSize
	if end > totalCount {
		end = totalCount
	}
	pageSnaps := snapshots[start:end]

	// Mandatory-marking subset filtering must also gate the time-travel read
	// path, else a caller could bypass markings via ?asOf=<now>. Mirror the
	// live LoadObjects handling: preserve _markings through select-filtering
	// (snapshot Properties already carry it) so the subset check sees it, then
	// strip it from the response.
	asOfMarkingFieldAdded := h.markingFilter != nil && len(req.Select) > 0 &&
		h.markingFilter.MarkingsEnabled(ctx, req.ObjectSet.ObjectType)
	if asOfMarkingFieldAdded {
		for _, f := range req.Select {
			if f == markingField {
				asOfMarkingFieldAdded = false
				break
			}
		}
	}

	data := make([]*oss.WireObject, 0, len(pageSnaps))
	for _, snap := range pageSnaps {
		props := snap.Properties
		if len(req.Select) > 0 {
			filtered := make(map[string]interface{}, len(req.Select))
			for _, f := range req.Select {
				if v, ok := props[f]; ok {
					filtered[f] = v
				}
			}
			if asOfMarkingFieldAdded {
				if v, ok := props[markingField]; ok {
					filtered[markingField] = v
				}
			}
			props = filtered
		}
		data = append(data, oss.FormatObject(req.ObjectSet.ObjectType, snap.PrimaryKey, props))
	}

	if h.markingFilter != nil {
		data = h.markingFilter.FilterByMarkings(ctx, req.ObjectSet.ObjectType, data)
		if asOfMarkingFieldAdded {
			for _, obj := range data {
				if obj != nil && obj.Properties != nil {
					delete(obj.Properties, markingField)
				}
			}
		}
	}

	data, err = h.applyPropertyVisibility(ctx, req.ObjectSet.ObjectType, data)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("PropertyFilterFailed", map[string]string{"error": err.Error()}))
		return
	}

	resp := &LoadObjectSetResponse{
		Data:               data,
		TotalCount:         strconv.Itoa(totalCount),
		TotalCountAccuracy: "EXACT",
	}
	if end < totalCount {
		nextCursor := &pagination.Cursor{Offset: end}
		resp.NextPageToken = nextCursor.Encode()
	}

	if h.dataAccessAuditor != nil {
		h.dataAccessAuditor.RecordLoadObjectSet(ctx, ontologyAPIName, req.ObjectSet.ObjectType, map[string]any{
			"count":      len(data),
			"totalCount": totalCount,
			"asOf":       asOf.Format(time.RFC3339),
		})
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// AggregateObjectSetRequest is the request for objectSet aggregation.
type AggregateObjectSetRequest struct {
	ObjectSet       *Definition                      `json:"objectSet"`
	Aggregation     []aggregation.AggregationSpec    `json:"aggregation"`
	GroupBy         []aggregation.GroupBySpec        `json:"groupBy,omitempty"`
	SubAggregations []aggregation.SubAggregationSpec `json:"subAggregations,omitempty"`
	Having          []aggregation.HavingClause       `json:"having,omitempty"`
	Cube            bool                             `json:"cube,omitempty"`
	Rollup          bool                             `json:"rollup,omitempty"`
	// ExcludedItems is an optional list of primary keys to exclude from
	// the resolved ObjectSet before aggregation runs (US-382). Forwarded
	// verbatim to the underlying aggregation engine so a single field
	// drives both the Bleve-facet path and the derived-field in-memory
	// path.
	ExcludedItems []string `json:"excludedItems,omitempty"`
}

// Aggregate handles POST /api/v2/ontologies/{ont}/objectSets/aggregate.
func (h *Handler) Aggregate(w http.ResponseWriter, r *http.Request) {
	var req AggregateObjectSetRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{"error": err.Error()}))
		return
	}

	if req.ObjectSet == nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingObjectSet", nil))
		return
	}

	ctx := WithOntologyScope(r.Context(), chi.URLParam(r, "ontologyApiName"))

	// Execute the ObjectSet to determine the object type and PKs.
	result, err := h.executor.Execute(ctx, req.ObjectSet)
	if err != nil {
		apierror.WriteJSON(w, executeError(err))
		return
	}

	// When the ObjectSet produced withProperties-derived values AND at least
	// one metric targets a derived field, route through the in-memory path
	// that reads values straight from Result.DerivedValues. The Bleve-facet
	// engine would otherwise return nil for any derived metric because the
	// field is not present in the base index.
	if aggregationNeedsDerivedPath(req.Aggregation, result.DerivedValues) {
		aggResult, err := h.aggregateWithDerived(ctx, result, &req)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("AggregationFailed", map[string]string{"error": err.Error()}))
			return
		}
		httputil.WriteJSON(w, http.StatusOK, aggResult)
		return
	}

	idx := h.indexMgr.GetIndex(scopedIndexKey(ctx, h.indexMgr, result.ObjectType))
	if idx == nil {
		apierror.WriteJSON(w, apierror.NewNotFound("IndexNotFound", map[string]string{"objectType": result.ObjectType}))
		return
	}

	// Build a base query scoped to the ObjectSet's primary keys.
	var baseQuery query.Query
	if len(result.PrimaryKeys) > 0 {
		baseQuery = bleve.NewDocIDQuery(result.PrimaryKeys)
	} else {
		baseQuery = bleve.NewMatchAllQuery()
	}

	aggReq := &aggregation.AggregationRequest{
		ObjectType:      result.ObjectType,
		Aggregations:    req.Aggregation,
		GroupBy:         req.GroupBy,
		SubAggregations: req.SubAggregations,
		Having:          req.Having,
		Cube:            req.Cube,
		Rollup:          req.Rollup,
		ExcludedItems:   req.ExcludedItems,
	}

	aggResult, err := h.aggEngine.AggregateWithQuery(idx, baseQuery, aggReq)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("AggregationFailed", map[string]string{"error": err.Error()}))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, aggResult)
}

// CreateTemporary handles POST /api/v2/ontologies/{ont}/objectSets/createTemporary.
func (h *Handler) CreateTemporary(w http.ResponseWriter, r *http.Request) {
	var req CreateTemporaryRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{"error": err.Error()}))
		return
	}

	if req.ObjectSet == nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingObjectSet", nil))
		return
	}

	if err := req.ObjectSet.Validate(); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidObjectSet", map[string]string{"error": err.Error()}))
		return
	}

	id := h.store.Put(req.ObjectSet)
	httputil.WriteJSON(w, http.StatusOK, &CreateTemporaryResponse{
		ObjectSetRID: id,
	})
}
