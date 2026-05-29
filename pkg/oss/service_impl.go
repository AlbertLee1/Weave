package oss

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/cellsec"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/links"
	"github.com/liyang/weave/pkg/masking"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss/pagination"
	"github.com/liyang/weave/pkg/oss/where"
	"github.com/liyang/weave/pkg/rls"
	"github.com/liyang/weave/pkg/security"
	"github.com/liyang/weave/pkg/tracing"
	"go.opentelemetry.io/otel/attribute"
)

// ServiceImpl implements the Service interface.
type ServiceImpl struct {
	omsRepo      oms.Repository
	indexMgr     *index.Manager
	linkResolver links.LinkResolver

	// policyFilter is the optional ABAC enforcement layer applied after every
	// read. When nil, all object reads bypass policy evaluation (back-compat
	// for tests and dev mode that haven't wired the filter yet).
	policyFilter *PolicyFilter

	// policyEngine is the optional query-time row-level policy compiler
	// (US-046). When attached every Load/Search path AND-combines the
	// per-user policy query into the Bleve request BEFORE the search runs,
	// so denied rows never materialize. A nil engine short-circuits to
	// bleve.NewMatchAllQuery() so existing callers are unaffected.
	policyEngine *security.Engine

	// rowPolicyEngine is the US-256 row_policies engine. When attached its
	// compiled clauses are AND-combined alongside policyEngine so both
	// security surfaces enforce at read time. A nil engine is treated as
	// "no additional filter" and existing callers are unaffected.
	rowPolicyEngine *rls.Engine

	// columnMaskEngine is the US-257 column_masks engine. When attached it
	// rewrites property values on WireObjects returned from every read path
	// for callers outside the mask's AppliesTo allow list. A nil engine is
	// a no-op so legacy callers that haven't wired masking keep their
	// current wire shape.
	columnMaskEngine *masking.Engine

	// cellMaskEngine is the US-258 cell_masks engine. When attached it
	// rewrites property values on a SPECIFIC (objectType, primaryKey) row
	// for callers outside the mask's AppliesTo allow list. Runs AFTER
	// applyColumnMasking so cell-specific rules can override or add to the
	// column-wide mask set for that row. A nil engine is a no-op.
	cellMaskEngine *cellsec.Engine

	// dataAccessAuditor is the US-264 data-access audit sink. Emits an
	// audit_events row (action = "data.access") on successful reads of
	// ObjectTypes whose AuditDataAccess flag is true. A nil auditor, or an
	// ObjectType that hasn't opted in, is a no-op so the audit write cost
	// is paid only for explicitly-governed types.
	dataAccessAuditor *DataAccessAuditor
}

// NewService creates a new OSS service.
func NewService(omsRepo oms.Repository, indexMgr *index.Manager, linkResolver links.LinkResolver) *ServiceImpl {
	return &ServiceImpl{
		omsRepo:      omsRepo,
		indexMgr:     indexMgr,
		linkResolver: linkResolver,
	}
}

// SetPolicyFilter installs the ABAC PolicyFilter that gates read responses.
// Call sites should attach the filter immediately after NewService during
// server boot. Passing nil disables filtering (used by older tests that
// don't seed any policies).
func (s *ServiceImpl) SetPolicyFilter(f *PolicyFilter) {
	s.policyFilter = f
}

// SetPolicyEngine attaches the query-time row-level policy engine. The
// engine is consulted inside each Load/Search path to compile the caller's
// policy clause, which is then AND-combined with the user-supplied where
// filter via bleve.NewConjunctionQuery. Pass nil to detach (tests / dev
// mode). Safe to call at any point — every read re-reads the field.
func (s *ServiceImpl) SetPolicyEngine(e *security.Engine) {
	s.policyEngine = e
}

// SetRowPolicyEngine attaches the US-256 row_policies engine. Its output
// is AND-combined with whatever the security.Engine emits (if any), keeping
// the two security surfaces independently authorable. Pass nil to detach.
func (s *ServiceImpl) SetRowPolicyEngine(e *rls.Engine) {
	s.rowPolicyEngine = e
}

// SetColumnMaskEngine attaches the US-257 column_masks engine. Compiled
// transforms are applied to every WireObject returned from the service's
// read paths BEFORE serialization, so masked property values reach the wire
// but never leak to the caller's network. Pass nil to detach.
func (s *ServiceImpl) SetColumnMaskEngine(e *masking.Engine) {
	s.columnMaskEngine = e
}

// SetCellMaskEngine attaches the US-258 cell_masks engine. Compiled
// transforms are applied to every WireObject returned from the service's
// read paths AFTER column masking, so per-row overrides can sharpen (or
// add to) the column-wide policy for a specific instance. Pass nil to
// detach.
func (s *ServiceImpl) SetCellMaskEngine(e *cellsec.Engine) {
	s.cellMaskEngine = e
}

// SetDataAccessAuditor attaches the US-264 data-access audit sink. When
// attached, every successful GetObject / ListObjects / SearchObjects /
// ListLinkedObjects / GetLinkedObject call emits an audit_events row for
// ObjectTypes whose AuditDataAccess flag is true. Passing nil detaches the
// auditor (back-compat for tests that don't wire an audit store).
func (s *ServiceImpl) SetDataAccessAuditor(a *DataAccessAuditor) {
	s.dataAccessAuditor = a
}

// compilePolicyQuery compiles the row-level security policy for ot into a
// Bleve query suitable for AND-combining into a read request. Both the
// security.Engine (US-046 rule-based ABAC) and the rls.Engine (US-256
// predicate-based row policies) are consulted; their output is joined with
// a ConjunctionQuery so BOTH surfaces enforce simultaneously. A nil return
// means "no extra filter" and callers use their base query unchanged. An
// engine that resolves to a match-all clause contributes nothing to avoid
// degenerate wrappers.
func (s *ServiceImpl) compilePolicyQuery(ctx context.Context, ot oms.ObjectType) (query.Query, error) {
	user := auth.UserFromContext(ctx)

	var abacQ query.Query
	if s.policyEngine != nil {
		q, err := s.policyEngine.Evaluate(ctx, user, ot)
		if err != nil {
			return nil, err
		}
		if _, ok := q.(*query.MatchAllQuery); !ok {
			abacQ = q
		}
	}

	var rlsQ query.Query
	if s.rowPolicyEngine != nil {
		q, err := s.rowPolicyEngine.Compile(ctx, user, ot.RID)
		if err != nil {
			return nil, err
		}
		rlsQ = q
	}

	switch {
	case abacQ == nil && rlsQ == nil:
		return nil, nil
	case abacQ != nil && rlsQ == nil:
		return abacQ, nil
	case abacQ == nil && rlsQ != nil:
		return rlsQ, nil
	default:
		return bleve.NewConjunctionQuery(abacQ, rlsQ), nil
	}
}

// mergePolicyQuery AND-combines a user-supplied Bleve query with the
// compiled policy query. Returns the userQ unchanged when policyQ is nil,
// otherwise wraps both in a ConjunctionQuery. Keeps the single "merge
// policy with user where" idiom in one place so every call site stays
// consistent.
func mergePolicyQuery(userQ, policyQ query.Query) query.Query {
	if policyQ == nil {
		return userQ
	}
	return bleve.NewConjunctionQuery(userQ, policyQ)
}

// applyPolicyFilter is the single chokepoint where every read method funnels
// its result list through PolicyFilter.FilterObjects. Returning the input
// unchanged when no filter is installed keeps existing tests green.
func (s *ServiceImpl) applyPolicyFilter(ctx context.Context, ontologyRID, objectTypeAPIName string, objs []*WireObject) ([]*WireObject, error) {
	if s.policyFilter == nil {
		return objs, nil
	}
	user := auth.UserFromContext(ctx)
	return s.policyFilter.FilterObjects(ctx, user, ontologyRID, objectTypeAPIName, objs)
}

// applyRowPolicyCEL enforces US-487 row-level CEL gates as a per-row
// post-filter. Skipped (returns input unchanged) when:
//
//   - the rowPolicyEngine is not wired (degraded mode),
//   - the input slice is empty,
//   - no CEL policy is registered for this ObjectType.
//
// Otherwise each candidate WireObject is run through
// Engine.EvaluateRowCEL with the caller's user binding and the object's
// Properties map. Any policy that rejects the row drops it from the
// output; any runtime error in policy evaluation also drops the row
// (fail-closed) so a broken CEL never silently leaks data.
//
// Call order: downstream of applyMarkingFilter (markings already
// scrubbed the obvious denies) and upstream of applyPropertyVisibility
// so column masks operate on the post-CEL survivors only.
func (s *ServiceImpl) applyRowPolicyCEL(ctx context.Context, ot *oms.ObjectType, objs []*WireObject) ([]*WireObject, error) {
	if s.rowPolicyEngine == nil || ot == nil || len(objs) == 0 {
		return objs, nil
	}
	if !s.rowPolicyEngine.HasCELForObjectType(ot.RID) {
		return objs, nil
	}
	user := auth.UserFromContext(ctx)
	out := make([]*WireObject, 0, len(objs))
	for _, obj := range objs {
		if obj == nil {
			continue
		}
		ok, err := s.rowPolicyEngine.EvaluateRowCEL(ctx, user, ot.RID, obj.Properties)
		if err != nil {
			// Fail-closed: skip the row but do not abort the whole page —
			// one broken policy / one missing field on one row should
			// not nuke the entire list response.
			continue
		}
		if !ok {
			continue
		}
		out = append(out, obj)
	}
	return out, nil
}

// applyMarkingFilter enforces US-052 Foundry-style mandatory access control
// (subset / AND semantics) as a post-Bleve verification pass. The policy
// engine's auto-marking clause (US-051) compiles to a should-terms
// BooleanQuery against `_markings`, which expresses "at least one overlap"
// — correct for single-valued marking fields but too loose for multi-valued
// ones where an object requires the full set of its labels. This pass
// runs auth.EvaluateMarkings per row so rows whose `_markings` slice is NOT
// a subset of the user's markings are dropped before handing the page back.
//
// Call order: downstream of applyPolicyFilter and upstream of
// applyPropertyVisibility, mirroring the established "drop denied rows first,
// then rewrite the survivors' columns" chain. Skipped when the policy engine
// is not attached, when the ObjectType is not opted in via
// SetMarkingsEnabled, or when the row list is empty.
func (s *ServiceImpl) applyMarkingFilter(ctx context.Context, ot *oms.ObjectType, objs []*WireObject) []*WireObject {
	if s.policyEngine == nil || ot == nil || len(objs) == 0 {
		return objs
	}
	if !s.policyEngine.MarkingsEnabled(ot.RID) {
		return objs
	}
	user := auth.UserFromContext(ctx)
	userMarkings := extractUserMarkings(user)
	out := make([]*WireObject, 0, len(objs))
	for _, o := range objs {
		if o == nil {
			continue
		}
		objMarkings := extractObjectMarkings(o)
		if !auth.EvaluateMarkings(userMarkings, objMarkings) {
			continue
		}
		out = append(out, o)
	}
	return out
}

// extractUserMarkings pulls the caller's marking set out of
// user.Attributes["markings"]. Kept local to service_impl.go so the OSS
// layer does not reach into pkg/security's unexported userMarkingsKey
// constant. The value mirrors `security.userMarkingsKey`; when that
// constant is exported in a future refactor this helper can delegate.
func extractUserMarkings(user *auth.User) []string {
	if user == nil || user.Attributes == nil {
		return nil
	}
	raw, ok := user.Attributes["markings"]
	if !ok {
		return nil
	}
	return coerceStringSlice(raw)
}

// extractObjectMarkings pulls a marking slice out of a WireObject by
// reading the reserved `_markings` property key. Bleve preserves
// multi-valued keyword fields either as []interface{} or []string
// depending on how the doc was indexed, so coerceStringSlice normalises
// both shapes plus the scalar-string case (legacy single-value docs).
func extractObjectMarkings(o *WireObject) []string {
	if o == nil || o.Properties == nil {
		return nil
	}
	raw, ok := o.Properties[security.MarkingField]
	if !ok {
		return nil
	}
	return coerceStringSlice(raw)
}

// coerceStringSlice is a shared helper used by extractUserMarkings and
// extractObjectMarkings so both paths accept the same input shapes.
func coerceStringSlice(raw any) []string {
	switch v := raw.(type) {
	case nil:
		return nil
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	default:
		return nil
	}
}

// applyColumnMasking enforces US-257 column-level masking by rewriting
// property values on each WireObject according to the caller-specific mask
// transforms compiled by masking.Engine. A nil engine, nil object-type or
// empty input slice short-circuits to a no-op. The engine bypasses admins
// and callers inside the mask's AppliesTo allow list, so the returned
// transform map is empty in those cases and the input slice flows through
// unchanged.
//
// Call order: after applyMarkingFilter so denied rows are dropped first,
// and after applyPropertyVisibility so property-level visibility decisions
// are made before value rewriting runs against the surviving properties.
func (s *ServiceImpl) applyColumnMasking(ctx context.Context, ot *oms.ObjectType, objs []*WireObject) []*WireObject {
	if s.columnMaskEngine == nil || ot == nil || len(objs) == 0 {
		return objs
	}
	user := auth.UserFromContext(ctx)
	transforms, err := s.columnMaskEngine.Compile(ctx, user, ot.RID)
	if err != nil || len(transforms) == 0 {
		return objs
	}
	for _, o := range objs {
		if o == nil || len(o.Properties) == 0 {
			continue
		}
		masking.ApplyTransforms(o.Properties, transforms)
	}
	return objs
}

// applyCellMasking enforces US-258 / US-376 cell-level security by rewriting
// property values on a per-(objectType, primary key) basis. Runs AFTER
// applyColumnMasking so row-specific cell rules can further restrict (or
// add transforms not declared at the column level) for a single instance.
// A nil engine, nil object-type or empty input slice short-circuits to a
// no-op.
//
// US-376: the row's properties are passed through to CompileForRow so any
// CEL Expression masks can evaluate against the (user, row) binding before
// the row hits the wire. Returned strategies are applied via
// masking.ApplyStrategyTransforms which understands the new NULL strategy
// alongside the legacy hash/redact/partial trio.
func (s *ServiceImpl) applyCellMasking(ctx context.Context, ot *oms.ObjectType, objs []*WireObject) []*WireObject {
	if s.cellMaskEngine == nil || ot == nil || len(objs) == 0 {
		return objs
	}
	user := auth.UserFromContext(ctx)
	for _, o := range objs {
		if o == nil || len(o.Properties) == 0 {
			continue
		}
		pk := stringifyPrimaryKey(o.PrimaryKey)
		if pk == "" {
			continue
		}
		transforms, err := s.cellMaskEngine.CompileForRow(ctx, user, ot.RID, pk, o.Properties)
		if err != nil || len(transforms) == 0 {
			continue
		}
		masking.ApplyStrategyTransforms(o.Properties, transforms)
	}
	return objs
}

// stringifyPrimaryKey renders a WireObject primary key as the canonical
// string key used by pkg/cellsec's index. Matches how FormatObject stores
// the value (string in practice, but WireObject.PrimaryKey is typed as
// interface{} for future composite keys). Returns "" when the value is
// nil — such rows bypass cell masking (nothing to target).
func stringifyPrimaryKey(pk interface{}) string {
	if pk == nil {
		return ""
	}
	if s, ok := pk.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", pk)
}

// applyPropertyVisibility enforces US-048 column-level visibility by running
// the row-level policy engine's AllowedProperties hook against each returned
// WireObject. The engine short-circuits to nil when no PROPERTY-scope
// policies are attached to the object type, in which case this pass is a
// no-op and the input slice is returned unchanged. When an allow list is
// returned the helper filters every object in place via
// WireObject.FilterProperties so fields outside the allow list are OMITTED
// (not nulled) from the serialized response.
//
// Call order: downstream of applyPolicyFilter so denied rows are dropped
// before the per-object column pass runs. The property filter only rewrites
// property maps — row count is preserved.
func (s *ServiceImpl) applyPropertyVisibility(ctx context.Context, ot *oms.ObjectType, objs []*WireObject) []*WireObject {
	if s.policyEngine == nil || ot == nil || len(objs) == 0 {
		return objs
	}
	user := auth.UserFromContext(ctx)
	allowed := s.policyEngine.AllowedProperties(ctx, user, *ot)
	if allowed == nil {
		return objs
	}
	out := make([]*WireObject, len(objs))
	for i, o := range objs {
		out[i] = o.FilterProperties(allowed)
	}
	return out
}

// GetObject retrieves a single object by its primary key.
//
// ABAC: when a PolicyFilter is installed, the freshly-loaded object is run
// through it. If the user can't see the object, the method returns
// ErrNotFound (not ErrForbidden) so the policy itself does not leak the
// object's existence. Allowed objects may have property values redacted.
func (s *ServiceImpl) GetObject(ctx context.Context, req GetObjectRequest) (*WireObject, error) {
	ctx, span := tracing.StartSpan(ctx, "oss.GetObject",
		attribute.String("ontology.rid", req.OntologyRID),
		attribute.String("object_type.api_name", req.ObjectType),
		attribute.String("object.primary_key", req.PrimaryKey),
	)
	defer span.End()

	ot, err := s.omsRepo.GetObjectTypeByAPIName(ctx, req.OntologyRID, req.ObjectType)
	if err != nil {
		return nil, err
	}

	// Look up by document ID (indexed with PK as doc ID). US-046 merges the
	// row-level policy query into the request so denied rows never hit the
	// decoder; the mergePolicyQuery helper keeps the wrapping in one place.
	idQ := bleve.NewDocIDQuery([]string{req.PrimaryKey})
	policyQ, err := s.compilePolicyQuery(ctx, *ot)
	if err != nil {
		return nil, err
	}
	searchReq := bleve.NewSearchRequest(mergePolicyQuery(idQ, policyQ))
	searchReq.Fields = []string{"*"}
	searchReq.Size = 1

	result, err := s.indexMgr.Search(scopedBleveKey(s.indexMgr, req.OntologyRID, req.ObjectType), searchReq)
	if err != nil {
		return nil, err
	}

	if result.Total == 0 {
		return nil, oms.ErrNotFound
	}

	hit := result.Hits[0]
	obj := FormatObject(req.ObjectType, req.PrimaryKey, hit.Fields)

	filtered, err := s.applyPolicyFilter(ctx, req.OntologyRID, req.ObjectType, []*WireObject{obj})
	if err != nil {
		return nil, err
	}
	// US-052: drop rows whose markings are not a subset of the caller's.
	// Applied AFTER applyPolicyFilter so the PolicyFilter pass still owns
	// pre-marking denies, and BEFORE applyPropertyVisibility so column
	// masking operates on the post-marking survivors only.
	filtered = s.applyMarkingFilter(ctx, ot, filtered)
	if len(filtered) == 0 {
		// Policy denied: hide existence with ErrNotFound rather than 403.
		return nil, oms.ErrNotFound
	}
	filtered, err = s.applyRowPolicyCEL(ctx, ot, filtered)
	if err != nil {
		return nil, err
	}
	if len(filtered) == 0 {
		return nil, oms.ErrNotFound
	}
	filtered = s.applyPropertyVisibility(ctx, ot, filtered)
	filtered = s.applyColumnMasking(ctx, ot, filtered)
	filtered = s.applyCellMasking(ctx, ot, filtered)
	s.dataAccessAuditor.Record(ctx, ot, "getObject", map[string]any{
		"objectType": req.ObjectType,
		"primaryKey": req.PrimaryKey,
	})
	return filtered[0], nil
}

// ListObjects lists objects of a given type with pagination.
func (s *ServiceImpl) ListObjects(ctx context.Context, req ListObjectsRequest) (*ObjectPage, error) {
	ctx, span := tracing.StartSpan(ctx, "oss.ListObjects",
		attribute.String("ontology.rid", req.OntologyRID),
		attribute.String("object_type.api_name", req.ObjectType),
	)
	defer span.End()

	ot, err := s.omsRepo.GetObjectTypeByAPIName(ctx, req.OntologyRID, req.ObjectType)
	if err != nil {
		return nil, err
	}

	cursor, err := pagination.DecodeCursor(req.PageToken)
	if err != nil {
		return nil, err
	}

	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = pagination.DefaultPageSize
	}
	if pageSize > pagination.MaxPageSize {
		pageSize = pagination.MaxPageSize
	}

	// US-046: merge the row-level policy query into the match-all base so
	// denied rows are filtered at query time. A nil policyQ leaves the
	// match-all untouched (back-compat for tests that don't wire an engine).
	policyQ, err := s.compilePolicyQuery(ctx, *ot)
	if err != nil {
		return nil, err
	}
	searchReq := bleve.NewSearchRequest(mergePolicyQuery(bleve.NewMatchAllQuery(), policyQ))
	searchReq.Fields = []string{"*"}
	searchReq.Size = pageSize
	searchReq.From = cursor.Offset

	// Apply ordering if specified.
	if req.OrderBy != "" {
		searchReq.SortBy(parseOrderBy(req.OrderBy))
	}

	result, err := s.indexMgr.Search(scopedBleveKey(s.indexMgr, req.OntologyRID, req.ObjectType), searchReq)
	if err != nil {
		return nil, err
	}

	page := &ObjectPage{
		Data: make([]*WireObject, 0, len(result.Hits)),
	}
	page.TotalCount = strconv.Itoa(int(result.Total))

	for _, hit := range result.Hits {
		pk := ""
		if v, ok := hit.Fields[ot.PrimaryKey]; ok {
			pk = fmt.Sprintf("%v", v)
		}
		page.Data = append(page.Data, FormatObject(req.ObjectType, pk, hit.Fields))
	}

	// ABAC: drop denied rows and redact masked properties.
	filtered, err := s.applyPolicyFilter(ctx, req.OntologyRID, req.ObjectType, page.Data)
	if err != nil {
		return nil, err
	}
	filtered = s.applyMarkingFilter(ctx, ot, filtered)
	filtered, err = s.applyRowPolicyCEL(ctx, ot, filtered)
	if err != nil {
		return nil, err
	}
	filtered = s.applyPropertyVisibility(ctx, ot, filtered)
	filtered = s.applyColumnMasking(ctx, ot, filtered)
	page.Data = s.applyCellMasking(ctx, ot, filtered)

	// Set next page token if there are more results
	nextOffset := cursor.Offset + pageSize
	if nextOffset < int(result.Total) {
		nextCursor := &pagination.Cursor{Offset: nextOffset}
		page.NextPageToken = nextCursor.Encode()
	}

	s.dataAccessAuditor.Record(ctx, ot, "listObjects", map[string]any{
		"objectType": req.ObjectType,
		"count":      len(page.Data),
	})

	return page, nil
}

// SearchObjects searches objects using a where clause with pagination.
func (s *ServiceImpl) SearchObjects(ctx context.Context, req SearchObjectsRequest) (*ObjectPage, error) {
	ctx, span := tracing.StartSpan(ctx, "oss.SearchObjects",
		attribute.String("ontology.rid", req.OntologyRID),
		attribute.String("object_type.api_name", req.ObjectType),
	)
	defer span.End()

	ot, err := s.omsRepo.GetObjectTypeByAPIName(ctx, req.OntologyRID, req.ObjectType)
	if err != nil {
		return nil, err
	}

	var bleveQuery query.Query
	if req.Where != nil {
		opts := &where.ConvertOptions{Fuzzy: req.Fuzzy}
		bleveQuery, err = where.ConvertToBleveQueryWithOpts(req.Where, opts)
		if err != nil {
			return nil, err
		}
	} else {
		bleveQuery = bleve.NewMatchAllQuery()
	}

	// US-046: AND-combine the row-level policy query into the caller's
	// where clause so denied rows never enter the Bleve result set.
	policyQ, err := s.compilePolicyQuery(ctx, *ot)
	if err != nil {
		return nil, err
	}
	bleveQuery = mergePolicyQuery(bleveQuery, policyQ)

	cursor, err := pagination.DecodeCursor(req.PageToken)
	if err != nil {
		return nil, err
	}

	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = pagination.DefaultPageSize
	}
	if pageSize > pagination.MaxPageSize {
		pageSize = pagination.MaxPageSize
	}

	searchReq := bleve.NewSearchRequest(bleveQuery)
	searchReq.Fields = []string{"*"}
	searchReq.Size = pageSize
	searchReq.From = cursor.Offset

	// US-235: attach a highlighter when the caller asked for match
	// snippets. Default style is "html" — which wraps matched terms with
	// <mark>...</mark>. Callers can restrict highlighting to specific
	// fields; an empty Fields slice falls back to Bleve's all-text-fields
	// default.
	if req.Highlight != nil {
		style := req.Highlight.Style
		if style == "" {
			style = "html"
		}
		hr := bleve.NewHighlightWithStyle(style)
		for _, f := range req.Highlight.Fields {
			if f = strings.TrimSpace(f); f != "" {
				hr.AddField(f)
			}
		}
		searchReq.Highlight = hr
	}

	// US-236: per-field term-count facets. Fields are de-duplicated so a
	// caller sending `?facets=owner,owner` sees a single bucket list.
	// Unknown / non-indexed fields silently yield empty buckets — Bleve's
	// facet executor tolerates them.
	facetFields := dedupeFacetFields(req.Facets)
	for _, f := range facetFields {
		searchReq.AddFacet(f, bleve.NewFacetRequest(f, defaultFacetSize))
	}

	// Apply ordering if specified.
	if req.OrderBy != "" {
		searchReq.SortBy(parseOrderBy(req.OrderBy))
	}

	// US-234: regex queries are bounded by RegexQueryTimeout to prevent
	// pathological patterns from monopolising the index reader. Bleve honours
	// context cancellation through its term iterator so the deadline applies
	// inside the FSA traversal, not just at the call site.
	hasRegex := where.HasRegexClause(req.Where)
	searchCtx := ctx
	if hasRegex {
		var cancel context.CancelFunc
		searchCtx, cancel = context.WithTimeout(ctx, where.RegexQueryTimeout)
		defer cancel()
	}

	result, err := s.indexMgr.SearchInContext(searchCtx, scopedBleveKey(s.indexMgr, req.OntologyRID, req.ObjectType), searchReq)
	if err != nil {
		if hasRegex && errors.Is(err, context.DeadlineExceeded) {
			// Wrap with ErrInvalidWhereClause sentinel — Foundry's
			// contract treats "your regex was too slow" as a user-input
			// bound (HTTP 400) rather than a server failure. Round-36
			// sentinel routing in the handler keeps the 400 envelope.
			return nil, fmt.Errorf("%w: regex search exceeded %s timeout: %w",
				where.ErrInvalidWhereClause, where.RegexQueryTimeout, err)
		}
		return nil, err
	}

	page := &ObjectPage{
		Data: make([]*WireObject, 0, len(result.Hits)),
	}
	page.TotalCount = strconv.Itoa(int(result.Total))

	// US-236: materialize facet buckets. Every requested field is
	// registered — including ones with zero matching terms — so SDK
	// consumers see a stable key set. Map iteration order over the
	// underlying `map[string]*search.FacetResult` is nondeterministic;
	// bucket order inside each slice follows Bleve's facet ordering
	// (descending count).
	if len(facetFields) > 0 {
		facets := make(map[string][]FacetBucket, len(facetFields))
		for _, f := range facetFields {
			facets[f] = collectFacetBuckets(result, f)
		}
		page.Facets = facets
	}

	for _, hit := range result.Hits {
		pk := ""
		if v, ok := hit.Fields[ot.PrimaryKey]; ok {
			pk = fmt.Sprintf("%v", v)
		}
		obj := FormatObject(req.ObjectType, pk, hit.Fields)
		// US-235: attach per-field snippets the highlighter produced.
		// Fragments are already post-processed (e.g. `<mark>`-wrapped by
		// the "html" style). A hit with no fragments leaves Highlights
		// nil so the wire shape stays identical to an un-highlighted
		// response.
		if len(hit.Fragments) > 0 {
			hl := make(map[string][]string, len(hit.Fragments))
			for field, frags := range hit.Fragments {
				if len(frags) == 0 {
					continue
				}
				hl[field] = append([]string(nil), frags...)
			}
			if len(hl) > 0 {
				obj.Highlights = hl
			}
		}
		page.Data = append(page.Data, obj)
	}

	// ABAC: drop denied rows and redact masked properties.
	filtered, err := s.applyPolicyFilter(ctx, req.OntologyRID, req.ObjectType, page.Data)
	if err != nil {
		return nil, err
	}
	filtered = s.applyMarkingFilter(ctx, ot, filtered)
	filtered, err = s.applyRowPolicyCEL(ctx, ot, filtered)
	if err != nil {
		return nil, err
	}
	filtered = s.applyPropertyVisibility(ctx, ot, filtered)
	filtered = s.applyColumnMasking(ctx, ot, filtered)
	page.Data = s.applyCellMasking(ctx, ot, filtered)

	// Set next page token if there are more results
	nextOffset := cursor.Offset + pageSize
	if nextOffset < int(result.Total) {
		nextCursor := &pagination.Cursor{Offset: nextOffset}
		page.NextPageToken = nextCursor.Encode()
	}

	s.dataAccessAuditor.Record(ctx, ot, "searchObjects", map[string]any{
		"objectType": req.ObjectType,
		"count":      len(page.Data),
	})

	return page, nil
}

// CountObjects returns the number of objects of a given type, optionally
// filtered by the supplied Where clause.
//
// Two paths:
//
//  1. Unfiltered fast path (req.Where == nil): use indexMgr.DocCount —
//     constant-time index read. Backward-compatible for SDK callers
//     that send no body.
//  2. Filtered path (req.Where != nil): run the same where → Bleve
//     query → mergePolicyQuery pipeline SearchObjects uses, but with
//     Size=0 so Bleve returns the total match count without paying
//     to materialize documents. This is the path Foundry's OSv2
//     count endpoint takes on every request, and it is the only
//     path that respects the row-level policy filter — never let a
//     filtered count short-circuit through DocCount, or a user with
//     a restricting policy will over-count rows they cannot see.
func (s *ServiceImpl) CountObjects(ctx context.Context, req CountObjectsRequest) (*CountObjectsResponse, error) {
	ctx, span := tracing.StartSpan(ctx, "oss.CountObjects",
		attribute.String("ontology.rid", req.OntologyRID),
		attribute.String("object_type.api_name", req.ObjectType),
		attribute.Bool("filter.where", req.Where != nil),
	)
	defer span.End()

	ot, err := s.omsRepo.GetObjectTypeByAPIName(ctx, req.OntologyRID, req.ObjectType)
	if err != nil {
		return nil, err
	}

	if req.Where == nil {
		count, err := s.indexMgr.DocCount(scopedBleveKey(s.indexMgr, req.OntologyRID, req.ObjectType))
		if err != nil {
			// Index not found for this object type — valid type but no data yet.
			return &CountObjectsResponse{Count: 0}, nil
		}
		return &CountObjectsResponse{Count: int(count)}, nil
	}

	bleveQuery, err := where.ConvertToBleveQueryWithOpts(req.Where, nil)
	if err != nil {
		return nil, err
	}
	policyQ, err := s.compilePolicyQuery(ctx, *ot)
	if err != nil {
		return nil, err
	}
	bleveQuery = mergePolicyQuery(bleveQuery, policyQ)

	idx := s.indexMgr.GetIndex(scopedBleveKey(s.indexMgr, req.OntologyRID, req.ObjectType))
	if idx == nil {
		// No index yet — a filtered count over zero rows is zero,
		// regardless of the predicate.
		return &CountObjectsResponse{Count: 0}, nil
	}

	searchReq := bleve.NewSearchRequest(bleveQuery)
	searchReq.Size = 0
	searchReq.From = 0
	res, err := idx.SearchInContext(ctx, searchReq)
	if err != nil {
		return nil, err
	}
	return &CountObjectsResponse{Count: int(res.Total)}, nil
}

// defaultFacetSize is the per-field bucket ceiling for US-236 faceted
// searches. Matches the aggregation engine's default for exact groupBy —
// large enough for realistic category/owner fanouts, small enough to keep
// single-request cost bounded.
const defaultFacetSize = 100

// dedupeFacetFields preserves first-seen order while dropping empty and
// repeat entries so callers passing `?facets=owner,owner,owner` only trigger
// one Bleve facet and see one bucket list.
func dedupeFacetFields(fields []string) []string {
	if len(fields) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(fields))
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	return out
}

// collectFacetBuckets lifts a Bleve facet result into the `[]FacetBucket`
// wire shape. Returns an empty (non-nil) slice when the field has no
// matching terms so the caller always sees a stable `[]` on the wire
// instead of a missing key. `result` may be nil during degraded-mode tests;
// in that case every field maps to an empty list.
func collectFacetBuckets(result *bleve.SearchResult, field string) []FacetBucket {
	if result == nil {
		return []FacetBucket{}
	}
	fr, ok := result.Facets[field]
	if !ok || fr == nil || fr.Terms == nil {
		return []FacetBucket{}
	}
	terms := fr.Terms.Terms()
	buckets := make([]FacetBucket, 0, len(terms))
	for _, t := range terms {
		buckets = append(buckets, FacetBucket{Value: t.Term, Count: t.Count})
	}
	return buckets
}

// parseOrderBy converts an orderBy string like "field:asc" or "field:desc" into
// a Bleve sort order slice. Bleve uses "-field" for descending, "field" for ascending.
// Multiple fields can be comma-separated: "field1:asc,field2:desc".
func parseOrderBy(orderBy string) []string {
	parts := strings.Split(orderBy, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// Split on ":" to separate field and direction.
		fieldDir := strings.SplitN(part, ":", 2)
		field := strings.TrimSpace(fieldDir[0])
		if field == "" {
			continue
		}
		if len(fieldDir) == 2 && strings.TrimSpace(fieldDir[1]) == "desc" {
			result = append(result, "-"+field)
		} else {
			result = append(result, field)
		}
	}
	return result
}

// ListLinkedObjects lists objects linked to a source object through a link type.
// When req.Direction is "reverse" the link is walked target -> source, which
// means the caller's req.ObjectType is the link's declared *target* and the
// returned objects are instances of the link's declared *source*.
func (s *ServiceImpl) ListLinkedObjects(ctx context.Context, req LinkedObjectsRequest) (*ObjectPage, error) {
	ctx, span := tracing.StartSpan(ctx, "oss.ListLinkedObjects",
		attribute.String("ontology.rid", req.OntologyRID),
		attribute.String("object_type.api_name", req.ObjectType),
		attribute.String("object.primary_key", req.PrimaryKey),
		attribute.String("link_type.api_name", req.LinkType),
		attribute.String("link.direction", req.Direction),
	)
	defer span.End()

	dir, err := links.ParseDirection(req.Direction)
	if err != nil {
		return nil, err
	}

	// Get the caller's own object type (source for forward, target for reverse).
	ot, err := s.omsRepo.GetObjectTypeByAPIName(ctx, req.OntologyRID, req.ObjectType)
	if err != nil {
		return nil, err
	}

	// Locate the LinkType definition. For forward, the caller's ObjectType is
	// the link's source, so we look through outgoing links. For reverse, the
	// caller's ObjectType is the link's target, so we look through incoming.
	var candidates []oms.LinkType
	if dir == links.DirectionReverse {
		candidates, err = s.omsRepo.ListIncomingLinkTypes(ctx, ot.RID)
	} else {
		candidates, err = s.omsRepo.ListOutgoingLinkTypes(ctx, ot.RID)
	}
	if err != nil {
		return nil, err
	}

	var matchedLT *oms.LinkType
	for i := range candidates {
		if candidates[i].APIName == req.LinkType {
			matchedLT = &candidates[i]
			break
		}
	}
	if matchedLT == nil {
		return nil, fmt.Errorf("link type %q not found for object type %q (direction=%s)", req.LinkType, req.ObjectType, dir)
	}

	// US-044: stamp the ontology scope on the context so the link resolver
	// (which routes through Bleve) hits the per-ontology index.
	scopedCtx := index.WithOntologyScope(ctx, req.OntologyRID)

	// Resolve linked primary keys via the direction-aware resolver.
	targetPKs, err := s.linkResolver.ResolveLinked(scopedCtx, matchedLT.RID, []string{req.PrimaryKey}, dir)
	if err != nil {
		return nil, err
	}

	if len(targetPKs) == 0 {
		page := &ObjectPage{
			Data: make([]*WireObject, 0),
		}
		page.TotalCount = "0"
		return page, nil
	}

	// The "other side" of the link from the caller's perspective:
	//   forward: caller is source -> look up target object type.
	//   reverse: caller is target -> look up source object type.
	otherRID := matchedLT.TargetObjectType
	if dir == links.DirectionReverse {
		otherRID = matchedLT.SourceObjectType
	}
	otherOT, err := s.omsRepo.GetObjectType(ctx, otherRID)
	if err != nil {
		return nil, err
	}
	targetOTAPIName := otherOT.APIName

	// Get the target object type to find its primary key field
	targetOT, err := s.omsRepo.GetObjectTypeByAPIName(ctx, req.OntologyRID, targetOTAPIName)
	if err != nil {
		return nil, err
	}

	// Apply pagination to target PKs
	cursor, err := pagination.DecodeCursor(req.PageToken)
	if err != nil {
		return nil, err
	}

	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = pagination.DefaultPageSize
	}
	if pageSize > pagination.MaxPageSize {
		pageSize = pagination.MaxPageSize
	}

	totalCount := len(targetPKs)

	// Paginate the target PKs
	start := cursor.Offset
	if start > len(targetPKs) {
		start = len(targetPKs)
	}
	end := start + pageSize
	if end > len(targetPKs) {
		end = len(targetPKs)
	}
	paginatedPKs := targetPKs[start:end]

	page := &ObjectPage{
		Data: make([]*WireObject, 0, len(paginatedPKs)),
	}
	page.TotalCount = strconv.Itoa(totalCount)

	if len(paginatedPKs) == 0 {
		return page, nil
	}

	// Batch-hydrate: single DocIDQuery instead of N per-PK TermQueries.
	// This was a documented N+1 performance bug (PERF_1). The DocIDQuery
	// matches all paginated PKs in one Bleve Search call.
	batchQ := bleve.NewDocIDQuery(paginatedPKs)
	batchReq := bleve.NewSearchRequest(batchQ)
	batchReq.Fields = []string{"*"}
	batchReq.Size = len(paginatedPKs)

	batchResult, err := s.indexMgr.Search(scopedBleveKey(s.indexMgr, req.OntologyRID, targetOTAPIName), batchReq)
	if err != nil {
		return nil, err
	}

	// Map hits by primary key so we can emit them in the original paginated
	// order (preserving link-resolver ordering).
	hitByPK := make(map[string]*search.DocumentMatch, len(batchResult.Hits))
	for _, h := range batchResult.Hits {
		pk := h.ID
		if v, ok := h.Fields[targetOT.PrimaryKey]; ok {
			pk = fmt.Sprintf("%v", v)
		}
		hitByPK[pk] = h
	}

	for _, pk := range paginatedPKs {
		hit, ok := hitByPK[pk]
		if !ok {
			// Target PK not found in the index — skip (missing doc, not an error).
			continue
		}
		page.Data = append(page.Data, FormatObject(targetOTAPIName, pk, hit.Fields))
	}

	// ABAC: enforce policies on the *target* object type, not the caller's
	// object type. The user must be able to see the linked rows themselves.
	filtered, err := s.applyPolicyFilter(ctx, req.OntologyRID, targetOTAPIName, page.Data)
	if err != nil {
		return nil, err
	}
	filtered = s.applyMarkingFilter(ctx, targetOT, filtered)
	filtered, err = s.applyRowPolicyCEL(ctx, targetOT, filtered)
	if err != nil {
		return nil, err
	}
	filtered = s.applyPropertyVisibility(ctx, targetOT, filtered)
	filtered = s.applyColumnMasking(ctx, targetOT, filtered)
	page.Data = s.applyCellMasking(ctx, targetOT, filtered)

	// Set next page token if there are more results
	nextOffset := cursor.Offset + pageSize
	if nextOffset < totalCount {
		nextCursor := &pagination.Cursor{Offset: nextOffset}
		page.NextPageToken = nextCursor.Encode()
	}

	s.dataAccessAuditor.Record(ctx, targetOT, "listLinkedObjects", map[string]any{
		"objectType":       req.ObjectType,
		"sourcePrimaryKey": req.PrimaryKey,
		"linkType":         req.LinkType,
		"direction":        dir.String(),
		"count":            len(page.Data),
	})

	return page, nil
}

// GetLinkedObject returns a single linked object identified by its primary key.
// It verifies the target PK is actually linked via the specified link type before
// returning it, returning ErrNotFound if the target is not linked.
func (s *ServiceImpl) GetLinkedObject(ctx context.Context, req GetLinkedObjectRequest) (*WireObject, error) {
	ctx, span := tracing.StartSpan(ctx, "oss.GetLinkedObject",
		attribute.String("ontology.rid", req.OntologyRID),
		attribute.String("object_type.api_name", req.ObjectType),
		attribute.String("object.primary_key", req.PrimaryKey),
		attribute.String("link_type.api_name", req.LinkType),
		attribute.String("linked_object.primary_key", req.LinkedObjectPrimaryKey),
		attribute.String("link.direction", req.Direction),
	)
	defer span.End()

	dir, err := links.ParseDirection(req.Direction)
	if err != nil {
		return nil, err
	}

	// Get the caller's object type.
	ot, err := s.omsRepo.GetObjectTypeByAPIName(ctx, req.OntologyRID, req.ObjectType)
	if err != nil {
		return nil, err
	}

	// Locate the LinkType definition.
	var candidates []oms.LinkType
	if dir == links.DirectionReverse {
		candidates, err = s.omsRepo.ListIncomingLinkTypes(ctx, ot.RID)
	} else {
		candidates, err = s.omsRepo.ListOutgoingLinkTypes(ctx, ot.RID)
	}
	if err != nil {
		return nil, err
	}

	var matchedLT *oms.LinkType
	for i := range candidates {
		if candidates[i].APIName == req.LinkType {
			matchedLT = &candidates[i]
			break
		}
	}
	if matchedLT == nil {
		return nil, fmt.Errorf("link type %q not found for object type %q (direction=%s)", req.LinkType, req.ObjectType, dir)
	}

	// US-044: stamp the ontology scope on the context so the link resolver
	// hits the per-ontology Bleve index.
	scopedCtx := index.WithOntologyScope(ctx, req.OntologyRID)

	// Resolve linked primary keys.
	targetPKs, err := s.linkResolver.ResolveLinked(scopedCtx, matchedLT.RID, []string{req.PrimaryKey}, dir)
	if err != nil {
		return nil, err
	}

	// Verify the requested linked PK is actually among the resolved targets.
	found := false
	for _, pk := range targetPKs {
		if pk == req.LinkedObjectPrimaryKey {
			found = true
			break
		}
	}
	if !found {
		return nil, oms.ErrNotFound
	}

	// Determine the target object type.
	otherRID := matchedLT.TargetObjectType
	if dir == links.DirectionReverse {
		otherRID = matchedLT.SourceObjectType
	}
	otherOT, err := s.omsRepo.GetObjectType(ctx, otherRID)
	if err != nil {
		return nil, err
	}

	// Hydrate the single object from Bleve.
	batchQ := bleve.NewDocIDQuery([]string{req.LinkedObjectPrimaryKey})
	batchReq := bleve.NewSearchRequest(batchQ)
	batchReq.Fields = []string{"*"}
	batchReq.Size = 1

	batchResult, err := s.indexMgr.Search(scopedBleveKey(s.indexMgr, req.OntologyRID, otherOT.APIName), batchReq)
	if err != nil {
		return nil, err
	}

	if len(batchResult.Hits) == 0 {
		return nil, oms.ErrNotFound
	}

	hit := batchResult.Hits[0]
	obj := FormatObject(otherOT.APIName, req.LinkedObjectPrimaryKey, hit.Fields)

	// US-048: apply column-level visibility against the target object type.
	filtered := s.applyPropertyVisibility(ctx, otherOT, []*WireObject{obj})
	// US-257: value-level masking after visibility filtering.
	filtered = s.applyColumnMasking(ctx, otherOT, filtered)
	// US-258: per-cell rules after the column-wide pass so row-specific
	// overrides can sharpen the wire view.
	filtered = s.applyCellMasking(ctx, otherOT, filtered)
	s.dataAccessAuditor.Record(ctx, otherOT, "getLinkedObject", map[string]any{
		"objectType":             req.ObjectType,
		"sourcePrimaryKey":       req.PrimaryKey,
		"linkType":               req.LinkType,
		"direction":              dir.String(),
		"linkedObjectPrimaryKey": req.LinkedObjectPrimaryKey,
	})
	return filtered[0], nil
}
