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
	Fuzzy       *where.FuzzyConfig
	// Highlight, when non-nil, instructs the service to attach Bleve
	// `<mark>`-wrapped snippets to each returned object under the
	// `_highlights` key. Fields are the specific property apiNames to
	// highlight; when nil / empty, Bleve defaults to every text field with
	// term vectors.
	Highlight *HighlightConfig
	// Facets, when non-empty, instructs the service to compute term-count
	// buckets per field via Bleve facet requests and attach them to the
	// returned ObjectPage under `facets: {field: [{value, count}]}`
	// (US-236). Fields that are missing or not indexed silently yield an
	// empty bucket list.
	Facets    []string
	PageSize  int
	PageToken string
	OrderBy   string
}

// HighlightConfig configures the per-search highlighter. Style is a Bleve
// highlighter name (e.g. "html", "ansi"). An empty Style defaults to "html"
// — which wraps matches with <mark>…</mark>, matching the US-235 contract.
type HighlightConfig struct {
	Style  string   `json:"style,omitempty"`
	Fields []string `json:"fields,omitempty"`
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

// GetLinkedObjectRequest is the request for getting a single linked object by its primary key.
type GetLinkedObjectRequest struct {
	OntologyRID            string
	ObjectType             string // source object type API name
	PrimaryKey             string // source object primary key
	LinkType               string // link type API name
	LinkedObjectPrimaryKey string // target linked object primary key
	Direction              string // traversal direction (optional, default "forward")
}

// CountObjectsRequest is the request for counting objects of a given type.
//
// Where is optional. When nil the implementation MAY use the fast
// indexMgr.DocCount path (no row-policy AND-combine, no Bleve search
// overhead). When non-nil the implementation MUST run the same
// where → Bleve query pipeline SearchObjects uses, including the
// row-level policy merge, so filtered counts can never over-report
// rows the caller is not authorised to see. PRD-V2 §4.1 OSv2-1:1.
type CountObjectsRequest struct {
	OntologyRID string
	ObjectType  string
	Where       *where.WhereClause
}

// CountObjectsResponse is the Foundry V2 response for the count endpoint.
type CountObjectsResponse struct {
	Count int `json:"count"`
}

// Service defines the Object Set Service interface.
type Service interface {
	GetObject(ctx context.Context, req GetObjectRequest) (*WireObject, error)
	ListObjects(ctx context.Context, req ListObjectsRequest) (*ObjectPage, error)
	SearchObjects(ctx context.Context, req SearchObjectsRequest) (*ObjectPage, error)
	ListLinkedObjects(ctx context.Context, req LinkedObjectsRequest) (*ObjectPage, error)
	GetLinkedObject(ctx context.Context, req GetLinkedObjectRequest) (*WireObject, error)
	CountObjects(ctx context.Context, req CountObjectsRequest) (*CountObjectsResponse, error)
}
