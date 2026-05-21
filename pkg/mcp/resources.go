package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// uriScheme is the URI prefix every Weave-side MCP resource URI uses. The
// authority segment after `weave://` discriminates the resource family
// (currently `ontology` and `objectset`).
const uriScheme = "weave://"

// Resource is a single entry in the resources/list response.
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// ResourceContent is a single content block returned by resources/read.
// Text carries the JSON-encoded payload for the resource.
type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
}

// ObjectSetEntry is the catalog-facing view of a temporary ObjectSet stored
// by pkg/oss/objectset.Store. We pin Definition to `any` so the catalog stays
// import-free of the objectset package; the production adapter passes the
// real *objectset.Definition value through unchanged.
type ObjectSetEntry struct {
	ID         string
	Definition any
	CreatedAt  time.Time
}

// ObjectSetCatalog is the optional dependency Server needs to expose
// ObjectSet resources. The production wiring satisfies it via a thin adapter
// over *objectset.Store; tests plug an in-memory fake. When unset, ObjectSet
// resources are simply absent from resources/list and resources/read on a
// `weave://objectset/...` URI returns ErrObjectSetNotFound.
type ObjectSetCatalog interface {
	ListObjectSets() []ObjectSetEntry
	GetObjectSet(id string) (*ObjectSetEntry, error)
}

// ErrObjectSetNotFound is the sentinel an ObjectSetCatalog should return when
// no entry matches the requested id. Server.handleResourcesRead maps it to
// the same application-error code as a missing ontology so MCP clients see a
// uniform "resource not found" failure mode.
var ErrObjectSetNotFound = errors.New("object set not found")

// SetObjectSetCatalog wires the optional ObjectSetCatalog dependency. Safe to
// leave unset in degraded-mode test routers that have no objectset.Store.
func (s *Server) SetObjectSetCatalog(c ObjectSetCatalog) { s.objectSetCatalog = c }

// resourcesReadParams is the params shape for resources/read.
type resourcesReadParams struct {
	URI string `json:"uri"`
}

type resourcesListParams struct {
	Cursor   string `json:"cursor,omitempty"`
	PageSize int    `json:"pageSize,omitempty"`
}

type resourceListCursor struct {
	Version  int    `json:"v"`
	AfterURI string `json:"afterUri"`
	PageSize int    `json:"pageSize,omitempty"`
}

const maxResourcePageSize = 500

type resourcesSubscribeParams struct {
	URI string `json:"uri"`
}

// handleResourcesList implements MCP resources/list. It enumerates every
// ontology in the OMS repo, every ObjectType under each ontology (OSV2-307,
// so AI clients can resource://read a single type's schema instead of
// fetching the entire ontology), plus every entry in the optional
// ObjectSetCatalog. The list is sorted by URI for deterministic wire
// output — clients that cache the catalogue benefit from a stable ordering.
//
// Failure semantics: if any per-ontology ListObjectTypes call errors out
// we surface an InternalError rather than returning a partial catalogue.
// A partial result would silently hide types from the client, which is
// the worst failure mode for a discovery endpoint — better to fail loudly
// and let the client retry once the upstream is healthy again.
func (s *Server) handleResourcesList(ctx context.Context, req *Request) *Response {
	params, errResp := decodeResourcesListParams(req)
	if errResp != nil {
		return errResp
	}

	resources := []Resource{}

	if s.oms != nil {
		onts, err := s.oms.ListOntologies(ctx)
		if err != nil {
			return NewErrorResponse(req.ID, CodeInternalError,
				"list ontologies: "+err.Error(), nil)
		}
		for _, o := range onts {
			name := o.DisplayName
			if name == "" {
				name = o.APIName
			}
			resources = append(resources, Resource{
				URI:         uriScheme + "ontology/" + o.RID,
				Name:        name,
				Description: o.Description,
				MimeType:    "application/json",
			})

			// OSV2-307: emit one resource per ObjectType under the ontology
			// so clients can address them directly via
			// weave://objecttype/<ontologyApiName>/<objectTypeApiName>.
			ots, err := s.oms.ListObjectTypes(ctx, o.RID)
			if err != nil {
				return NewErrorResponse(req.ID, CodeInternalError,
					fmt.Sprintf("list object types for %s: %s", o.APIName, err.Error()), nil)
			}
			for _, ot := range ots {
				otName := ot.DisplayName
				if otName == "" {
					otName = ot.APIName
				}
				resources = append(resources, Resource{
					URI:         uriScheme + "objecttype/" + o.APIName + "/" + ot.APIName,
					Name:        otName,
					Description: ot.Description,
					MimeType:    "application/json",
				})
			}
		}
	}

	if s.objectSetCatalog != nil {
		for _, e := range s.objectSetCatalog.ListObjectSets() {
			resources = append(resources, Resource{
				URI:         uriScheme + "objectset/" + e.ID,
				Name:        "objectSet " + e.ID,
				Description: fmt.Sprintf("Temporary ObjectSet created at %s", e.CreatedAt.UTC().Format(time.RFC3339)),
				MimeType:    "application/json",
			})
		}
	}

	sort.Slice(resources, func(i, j int) bool { return resources[i].URI < resources[j].URI })

	page, nextCursor, err := pageResources(resources, params)
	if err != nil {
		return NewErrorResponse(req.ID, CodeInvalidParams, err.Error(), nil)
	}
	result := map[string]any{"resources": page}
	if nextCursor != "" {
		result["nextCursor"] = nextCursor
	}
	return NewSuccessResponse(req.ID, result)
}

func decodeResourcesListParams(req *Request) (resourcesListParams, *Response) {
	var p resourcesListParams
	if len(req.Params) == 0 {
		return p, nil
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return p, NewErrorResponse(req.ID, CodeInvalidParams, "decode params: "+err.Error(), nil)
	}
	if p.PageSize < 0 {
		return p, NewErrorResponse(req.ID, CodeInvalidParams, "pageSize must be non-negative", nil)
	}
	if p.PageSize > maxResourcePageSize {
		return p, NewErrorResponse(req.ID, CodeInvalidParams,
			fmt.Sprintf("pageSize must be <= %d", maxResourcePageSize), nil)
	}
	if p.Cursor != "" {
		c, err := decodeResourceListCursor(p.Cursor)
		if err != nil {
			return p, NewErrorResponse(req.ID, CodeInvalidParams, err.Error(), nil)
		}
		if p.PageSize == 0 {
			p.PageSize = c.PageSize
		}
		p.Cursor = c.AfterURI
	}
	return p, nil
}

func pageResources(resources []Resource, params resourcesListParams) ([]Resource, string, error) {
	start := 0
	if params.Cursor != "" {
		start = sort.Search(len(resources), func(i int) bool {
			return resources[i].URI > params.Cursor
		})
	}
	remaining := len(resources) - start
	if params.PageSize == 0 || params.PageSize >= remaining {
		return resources[start:], "", nil
	}
	end := start + params.PageSize
	nextCursor, err := encodeResourceListCursor(resources[end-1].URI, params.PageSize)
	if err != nil {
		return nil, "", err
	}
	return resources[start:end], nextCursor, nil
}

func encodeResourceListCursor(afterURI string, pageSize int) (string, error) {
	body, err := json.Marshal(resourceListCursor{
		Version:  1,
		AfterURI: afterURI,
		PageSize: pageSize,
	})
	if err != nil {
		return "", fmt.Errorf("encode resource cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(body), nil
}

func decodeResourceListCursor(cursor string) (resourceListCursor, error) {
	body, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return resourceListCursor{}, fmt.Errorf("invalid resource cursor")
	}
	var c resourceListCursor
	if err := json.Unmarshal(body, &c); err != nil {
		return resourceListCursor{}, fmt.Errorf("invalid resource cursor")
	}
	if c.Version != 1 {
		return resourceListCursor{}, fmt.Errorf("invalid resource cursor version")
	}
	if !strings.HasPrefix(c.AfterURI, uriScheme) {
		return resourceListCursor{}, fmt.Errorf("invalid resource cursor boundary")
	}
	if c.PageSize < 0 {
		return resourceListCursor{}, fmt.Errorf("invalid resource cursor page size")
	}
	if c.PageSize > maxResourcePageSize {
		return resourceListCursor{}, fmt.Errorf("invalid resource cursor page size")
	}
	return c, nil
}

// handleResourcesRead implements MCP resources/read. The URI scheme is
// `weave://<kind>/<id>`; supported kinds are ontology (returns the OMS
// schema bundle) and objectset (returns the stored Definition). Any other
// scheme or kind returns CodeInvalidParams.
func (s *Server) handleResourcesRead(ctx context.Context, req *Request) *Response {
	var p resourcesReadParams
	if len(req.Params) == 0 {
		return NewErrorResponse(req.ID, CodeInvalidParams, "params required", nil)
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return NewErrorResponse(req.ID, CodeInvalidParams, "decode params: "+err.Error(), nil)
	}
	if p.URI == "" {
		return NewErrorResponse(req.ID, CodeInvalidParams, "uri is required", nil)
	}

	kind, id, err := parseResourceURI(p.URI)
	if err != nil {
		return NewErrorResponse(req.ID, CodeInvalidParams, err.Error(), nil)
	}

	switch kind {
	case "ontology":
		text, err := s.readOntology(ctx, id)
		if err != nil {
			return NewErrorResponse(req.ID, CodeToolError, err.Error(), nil)
		}
		return NewSuccessResponse(req.ID, map[string]any{
			"contents": []ResourceContent{{
				URI: p.URI, MimeType: "application/json", Text: text,
			}},
		})
	case "objectset":
		text, err := s.readObjectSet(id)
		if err != nil {
			return NewErrorResponse(req.ID, CodeToolError, err.Error(), nil)
		}
		return NewSuccessResponse(req.ID, map[string]any{
			"contents": []ResourceContent{{
				URI: p.URI, MimeType: "application/json", Text: text,
			}},
		})
	case "objecttype":
		// id shape: <ontologyApiName>/<objectTypeApiName>. OSV2-307.
		ontAPI, otAPI, ok := strings.Cut(id, "/")
		if !ok || ontAPI == "" || otAPI == "" {
			return NewErrorResponse(req.ID, CodeInvalidParams,
				"objecttype uri must be weave://objecttype/<ontologyApiName>/<objectTypeApiName>", nil)
		}
		text, err := s.readObjectType(ctx, ontAPI, otAPI)
		if err != nil {
			return NewErrorResponse(req.ID, CodeInvalidParams, err.Error(), nil)
		}
		return NewSuccessResponse(req.ID, map[string]any{
			"contents": []ResourceContent{{
				URI: p.URI, MimeType: "application/json", Text: text,
			}},
		})
	default:
		return NewErrorResponse(req.ID, CodeInvalidParams,
			fmt.Sprintf("unsupported resource kind %q", kind), nil)
	}
}

// handleResourcesSubscribe implements MCP resources/subscribe. We validate
// the URI against the live catalog before recording it so clients cannot
// believe a typo or stale RID is being watched.
func (s *Server) handleResourcesSubscribe(ctx context.Context, req *Request) *Response {
	uri, errResp := decodeResourceSubscriptionParams(req)
	if errResp != nil {
		return errResp
	}
	if err := s.validateSubscribableResource(ctx, uri); err != nil {
		if errors.Is(err, errInvalidResourceURI) {
			return NewErrorResponse(req.ID, CodeInvalidParams, err.Error(), nil)
		}
		return NewErrorResponse(req.ID, CodeToolError, err.Error(), nil)
	}

	s.mu.Lock()
	if s.resourceSubscriptions == nil {
		s.resourceSubscriptions = map[string]struct{}{}
	}
	s.resourceSubscriptions[uri] = struct{}{}
	s.mu.Unlock()
	return NewSuccessResponse(req.ID, map[string]any{})
}

// handleResourcesUnsubscribe implements MCP resources/unsubscribe. It is
// intentionally idempotent: a client may unsubscribe after a resource has
// already changed or disappeared, and cleanup should still succeed.
func (s *Server) handleResourcesUnsubscribe(req *Request) *Response {
	uri, errResp := decodeResourceSubscriptionParams(req)
	if errResp != nil {
		return errResp
	}
	if _, _, err := parseResourceURI(uri); err != nil {
		return NewErrorResponse(req.ID, CodeInvalidParams, err.Error(), nil)
	}

	s.mu.Lock()
	delete(s.resourceSubscriptions, uri)
	s.mu.Unlock()
	return NewSuccessResponse(req.ID, map[string]any{})
}

func decodeResourceSubscriptionParams(req *Request) (string, *Response) {
	var p resourcesSubscribeParams
	if len(req.Params) == 0 {
		return "", NewErrorResponse(req.ID, CodeInvalidParams, "params required", nil)
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return "", NewErrorResponse(req.ID, CodeInvalidParams, "decode params: "+err.Error(), nil)
	}
	if p.URI == "" {
		return "", NewErrorResponse(req.ID, CodeInvalidParams, "uri is required", nil)
	}
	return p.URI, nil
}

var errInvalidResourceURI = errors.New("invalid resource uri")

func (s *Server) validateSubscribableResource(ctx context.Context, uri string) error {
	kind, id, err := parseResourceURI(uri)
	if err != nil {
		return fmt.Errorf("%w: %s", errInvalidResourceURI, err.Error())
	}

	switch kind {
	case "ontology":
		if s.oms == nil {
			return errors.New("oms repository not configured")
		}
		if _, err := s.oms.GetOntology(ctx, id); err != nil {
			return fmt.Errorf("resource %q not found: %w", uri, err)
		}
		return nil
	case "objectset":
		if s.objectSetCatalog == nil {
			return fmt.Errorf("resource %q not found: %w", uri, ErrObjectSetNotFound)
		}
		if _, err := s.objectSetCatalog.GetObjectSet(id); err != nil {
			return fmt.Errorf("resource %q not found: %w", uri, err)
		}
		return nil
	case "objecttype":
		ontAPI, otAPI, ok := strings.Cut(id, "/")
		if !ok || ontAPI == "" || otAPI == "" {
			return fmt.Errorf("%w: objecttype uri must be weave://objecttype/<ontologyApiName>/<objectTypeApiName>", errInvalidResourceURI)
		}
		if s.oms == nil {
			return errors.New("oms repository not configured")
		}
		ont, err := s.oms.GetOntology(ctx, ontAPI)
		if err != nil {
			return fmt.Errorf("resource %q not found: %w", uri, err)
		}
		if _, err := s.oms.GetObjectTypeByAPIName(ctx, ont.RID, otAPI); err != nil {
			return fmt.Errorf("resource %q not found: %w", uri, err)
		}
		return nil
	default:
		return fmt.Errorf("%w: unsupported resource kind %q", errInvalidResourceURI, kind)
	}
}

// readObjectType resolves <ontologyApiName>/<objectTypeApiName> via the OMS
// repository and returns the ObjectType row JSON-encoded. Unknown ontology
// and unknown ObjectType both produce errors with the substring 'object
// type' so callers can match on it deterministically (OSV2-307 acceptance).
func (s *Server) readObjectType(ctx context.Context, ontologyAPIName, objectTypeAPIName string) (string, error) {
	if s.oms == nil {
		return "", errors.New("oms repository not configured")
	}
	ont, err := s.oms.GetOntology(ctx, ontologyAPIName)
	if err != nil {
		return "", fmt.Errorf("get ontology %q for object type lookup: %w", ontologyAPIName, err)
	}
	ot, err := s.oms.GetObjectTypeByAPIName(ctx, ont.RID, objectTypeAPIName)
	if err != nil || ot == nil {
		return "", fmt.Errorf("object type %q not found under ontology %q", objectTypeAPIName, ontologyAPIName)
	}
	buf, err := json.MarshalIndent(ot, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal object type: %w", err)
	}
	return string(buf), nil
}

// parseResourceURI splits a `weave://kind/id` URI into its parts. The id may
// itself contain slashes (RIDs follow `ri.{service}.{realm}.{type}.{uuid}`
// without slashes today, but the tolerance avoids breakage if a future RID
// shape adds them).
func parseResourceURI(uri string) (kind, id string, err error) {
	if !strings.HasPrefix(uri, uriScheme) {
		return "", "", fmt.Errorf("uri must start with %s", uriScheme)
	}
	rest := strings.TrimPrefix(uri, uriScheme)
	idx := strings.IndexByte(rest, '/')
	if idx < 0 {
		return "", "", fmt.Errorf("uri %q missing resource id", uri)
	}
	kind = rest[:idx]
	id = rest[idx+1:]
	if kind == "" || id == "" {
		return "", "", fmt.Errorf("uri %q has empty kind or id", uri)
	}
	return kind, id, nil
}

// readOntology bundles the ontology row, its object types (with properties),
// outgoing link types, and action types into a single JSON document. The
// shape is aligned with what the MCP `weave_list_*` tools return so clients
// that already consume the tools can reuse the same decoders.
func (s *Server) readOntology(ctx context.Context, rid string) (string, error) {
	if s.oms == nil {
		return "", errors.New("oms repository not configured")
	}
	ont, err := s.oms.GetOntology(ctx, rid)
	if err != nil {
		return "", fmt.Errorf("get ontology %s: %w", rid, err)
	}
	objectTypes, err := s.oms.ListObjectTypes(ctx, rid)
	if err != nil {
		return "", fmt.Errorf("list object types: %w", err)
	}
	linkTypes, err := s.oms.ListLinkTypes(ctx, rid)
	if err != nil {
		return "", fmt.Errorf("list link types: %w", err)
	}
	actionTypes, err := s.oms.ListActionTypes(ctx, rid)
	if err != nil {
		return "", fmt.Errorf("list action types: %w", err)
	}

	out := map[string]any{
		"ontology":    ont,
		"objectTypes": objectTypes,
		"linkTypes":   linkTypes,
		"actionTypes": actionTypes,
	}
	buf, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal ontology schema: %w", err)
	}
	return string(buf), nil
}

// readObjectSet returns the stored Definition for a temporary ObjectSet.
// Reading the live materialised contents is left to the existing
// loadObjectSet HTTP route — callers that want rows can post the returned
// Definition there. This keeps the resource read deterministic and cheap.
func (s *Server) readObjectSet(id string) (string, error) {
	if s.objectSetCatalog == nil {
		return "", errors.New("object set catalog not configured")
	}
	entry, err := s.objectSetCatalog.GetObjectSet(id)
	if err != nil {
		return "", fmt.Errorf("get object set %s: %w", id, err)
	}
	out := map[string]any{
		"objectSetId": entry.ID,
		"createdAt":   entry.CreatedAt.UTC().Format(time.RFC3339),
		"definition":  entry.Definition,
	}
	buf, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal object set: %w", err)
	}
	return string(buf), nil
}
