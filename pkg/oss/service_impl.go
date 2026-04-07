package oss

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/links"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss/pagination"
	"github.com/liyang/weave/pkg/oss/where"
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

// GetObject retrieves a single object by its primary key.
//
// ABAC: when a PolicyFilter is installed, the freshly-loaded object is run
// through it. If the user can't see the object, the method returns
// ErrNotFound (not ErrForbidden) so the policy itself does not leak the
// object's existence. Allowed objects may have property values redacted.
func (s *ServiceImpl) GetObject(ctx context.Context, req GetObjectRequest) (*WireObject, error) {
	if _, err := s.omsRepo.GetObjectTypeByAPIName(ctx, req.OntologyRID, req.ObjectType); err != nil {
		return nil, err
	}

	// Look up by document ID (indexed with PK as doc ID)
	q := bleve.NewDocIDQuery([]string{req.PrimaryKey})
	searchReq := bleve.NewSearchRequest(q)
	searchReq.Fields = []string{"*"}
	searchReq.Size = 1

	result, err := s.indexMgr.Search(req.ObjectType, searchReq)
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
	if len(filtered) == 0 {
		// Policy denied: hide existence with ErrNotFound rather than 403.
		return nil, oms.ErrNotFound
	}
	return filtered[0], nil
}

// ListObjects lists objects of a given type with pagination.
func (s *ServiceImpl) ListObjects(ctx context.Context, req ListObjectsRequest) (*ObjectPage, error) {
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

	searchReq := bleve.NewSearchRequest(bleve.NewMatchAllQuery())
	searchReq.Fields = []string{"*"}
	searchReq.Size = pageSize
	searchReq.From = cursor.Offset

	// Apply ordering if specified.
	if req.OrderBy != "" {
		searchReq.SortBy(parseOrderBy(req.OrderBy))
	}

	result, err := s.indexMgr.Search(req.ObjectType, searchReq)
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
	page.Data = filtered

	// Set next page token if there are more results
	nextOffset := cursor.Offset + pageSize
	if nextOffset < int(result.Total) {
		nextCursor := &pagination.Cursor{Offset: nextOffset}
		page.NextPageToken = nextCursor.Encode()
	}

	return page, nil
}

// SearchObjects searches objects using a where clause with pagination.
func (s *ServiceImpl) SearchObjects(ctx context.Context, req SearchObjectsRequest) (*ObjectPage, error) {
	ot, err := s.omsRepo.GetObjectTypeByAPIName(ctx, req.OntologyRID, req.ObjectType)
	if err != nil {
		return nil, err
	}

	var bleveQuery query.Query
	if req.Where != nil {
		bleveQuery, err = where.ConvertToBleveQuery(req.Where)
		if err != nil {
			return nil, err
		}
	} else {
		bleveQuery = bleve.NewMatchAllQuery()
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

	searchReq := bleve.NewSearchRequest(bleveQuery)
	searchReq.Fields = []string{"*"}
	searchReq.Size = pageSize
	searchReq.From = cursor.Offset

	// Apply ordering if specified.
	if req.OrderBy != "" {
		searchReq.SortBy(parseOrderBy(req.OrderBy))
	}

	result, err := s.indexMgr.Search(req.ObjectType, searchReq)
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
	page.Data = filtered

	// Set next page token if there are more results
	nextOffset := cursor.Offset + pageSize
	if nextOffset < int(result.Total) {
		nextCursor := &pagination.Cursor{Offset: nextOffset}
		page.NextPageToken = nextCursor.Encode()
	}

	return page, nil
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

	// Resolve linked primary keys via the direction-aware resolver.
	targetPKs, err := s.linkResolver.ResolveLinked(ctx, matchedLT.RID, []string{req.PrimaryKey}, dir)
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

	batchResult, err := s.indexMgr.Search(targetOTAPIName, batchReq)
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
	page.Data = filtered

	// Set next page token if there are more results
	nextOffset := cursor.Offset + pageSize
	if nextOffset < totalCount {
		nextCursor := &pagination.Cursor{Offset: nextOffset}
		page.NextPageToken = nextCursor.Encode()
	}

	return page, nil
}
