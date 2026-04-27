package mcp

import (
	"context"
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

// handleResourcesList implements MCP resources/list. It enumerates every
// ontology in the OMS repo plus every entry in the optional ObjectSetCatalog.
// The list is sorted by URI for deterministic wire output — clients that
// cache the catalogue benefit from a stable ordering.
func (s *Server) handleResourcesList(ctx context.Context, req *Request) *Response {
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
	return NewSuccessResponse(req.ID, map[string]any{"resources": resources})
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
	default:
		return NewErrorResponse(req.ID, CodeInvalidParams,
			fmt.Sprintf("unsupported resource kind %q", kind), nil)
	}
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
