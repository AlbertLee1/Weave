package oss

import (
	"context"
	"fmt"
	"strconv"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
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
}

// NewService creates a new OSS service.
func NewService(omsRepo oms.Repository, indexMgr *index.Manager, linkResolver links.LinkResolver) *ServiceImpl {
	return &ServiceImpl{
		omsRepo:      omsRepo,
		indexMgr:     indexMgr,
		linkResolver: linkResolver,
	}
}

// GetObject retrieves a single object by its primary key.
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
	return FormatObject(req.ObjectType, req.PrimaryKey, hit.Fields), nil
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

	// Set next page token if there are more results
	nextOffset := cursor.Offset + pageSize
	if nextOffset < int(result.Total) {
		nextCursor := &pagination.Cursor{Offset: nextOffset}
		page.NextPageToken = nextCursor.Encode()
	}

	return page, nil
}

// ListLinkedObjects lists objects linked to a source object through a link type.
func (s *ServiceImpl) ListLinkedObjects(ctx context.Context, req LinkedObjectsRequest) (*ObjectPage, error) {
	// Get the source object type to resolve the link
	ot, err := s.omsRepo.GetObjectTypeByAPIName(ctx, req.OntologyRID, req.ObjectType)
	if err != nil {
		return nil, err
	}

	// Resolve linked object primary keys
	targetPKs, err := s.linkResolver.ResolveLinkedObjectsByAPIName(ctx, ot.RID, req.LinkType, []string{req.PrimaryKey})
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

	// Get the link type to find the target object type
	outgoing, err := s.omsRepo.ListOutgoingLinkTypes(ctx, ot.RID)
	if err != nil {
		return nil, err
	}

	var targetOTAPIName string
	for _, lt := range outgoing {
		if lt.APIName == req.LinkType {
			// TargetObjectType is the RID; we need the API name
			targetOT, err := s.omsRepo.GetObjectType(ctx, lt.TargetObjectType)
			if err != nil {
				return nil, err
			}
			targetOTAPIName = targetOT.APIName
			break
		}
	}

	if targetOTAPIName == "" {
		return nil, fmt.Errorf("link type %q not found", req.LinkType)
	}

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

	// Build a disjunction query to find all target objects
	page := &ObjectPage{
		Data: make([]*WireObject, 0, len(paginatedPKs)),
	}
	page.TotalCount = strconv.Itoa(totalCount)

	for _, pk := range paginatedPKs {
		q := bleve.NewTermQuery(pk)
		q.SetField(targetOT.PrimaryKey)
		searchReq := bleve.NewSearchRequest(q)
		searchReq.Fields = []string{"*"}
		searchReq.Size = 1

		result, err := s.indexMgr.Search(targetOTAPIName, searchReq)
		if err != nil {
			return nil, err
		}

		if len(result.Hits) > 0 {
			hit := result.Hits[0]
			page.Data = append(page.Data, FormatObject(targetOTAPIName, pk, hit.Fields))
		}
	}

	// Set next page token if there are more results
	nextOffset := cursor.Offset + pageSize
	if nextOffset < totalCount {
		nextCursor := &pagination.Cursor{Offset: nextOffset}
		page.NextPageToken = nextCursor.Encode()
	}

	return page, nil
}
