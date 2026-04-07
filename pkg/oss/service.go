package oss

import (
	"context"

	"github.com/liyang/weave/pkg/oss/where"
)

// GetObjectRequest is the request for getting a single object by primary key.
type GetObjectRequest struct {
	OntologyRID string
	ObjectType  string // API name
	PrimaryKey  string
}

// ListObjectsRequest is the request for listing objects with pagination.
type ListObjectsRequest struct {
	OntologyRID string
	ObjectType  string
	PageSize    int
	PageToken   string
	OrderBy     string // field to sort by (optional)
}

// SearchObjectsRequest is the request for searching objects with a where clause.
type SearchObjectsRequest struct {
	OntologyRID string
	ObjectType  string
	Where       *where.WhereClause
	PageSize    int
	PageToken   string
	OrderBy     string
}

// LinkedObjectsRequest is the request for listing linked objects.
type LinkedObjectsRequest struct {
	OntologyRID string
	ObjectType  string
	PrimaryKey  string
	LinkType    string // link type API name
	// Direction selects traversal direction. "" (default) or "forward" walks
	// the link in its declared source -> target direction. "reverse" walks
	// target -> source, allowing incoming-link discovery.
	Direction string
	PageSize  int
	PageToken string
}

// CreateLinkRequest is the request for creating (or upserting) a single
// many-to-many link edge between two objects.
type CreateLinkRequest struct {
	OntologyRID     string
	LinkTypeAPIName string
	SourcePK        string
	TargetPK        string
	Properties      map[string]interface{}
}

// DeleteLinkRequest is the request for deleting a single many-to-many link
// edge between two objects. Idempotent at the repository layer.
type DeleteLinkRequest struct {
	OntologyRID     string
	LinkTypeAPIName string
	SourcePK        string
	TargetPK        string
}

// Service defines the Object Set Service interface.
type Service interface {
	GetObject(ctx context.Context, req GetObjectRequest) (*WireObject, error)
	ListObjects(ctx context.Context, req ListObjectsRequest) (*ObjectPage, error)
	SearchObjects(ctx context.Context, req SearchObjectsRequest) (*ObjectPage, error)
	ListLinkedObjects(ctx context.Context, req LinkedObjectsRequest) (*ObjectPage, error)
	CreateLink(ctx context.Context, req CreateLinkRequest) error
	DeleteLink(ctx context.Context, req DeleteLinkRequest) error
}
