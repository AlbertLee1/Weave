package graphsvc

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/liyang/weave/pkg/oms"
)

// payloadSchemaResource is the in-memory URL the JSON Schema compiler uses to
// register schema/graph.schema.json — arbitrary but stable across calls so the
// compiler can resolve $ref entries (#/definitions/layer, etc.).
const payloadSchemaResource = "mem://vertex-graph.schema.json"

//go:embed schema/graph.schema.json
var payloadSchemaBytes []byte

// PayloadValidationError is returned by PayloadValidator.Validate when a
// SystemGraph payload fails either structural (JSON Schema) or semantic
// (objectType / linkType reference) checks.
//
// The handler maps Status directly to the HTTP response, so callers must
// distinguish 400 (PayloadCodeSchema) from 422 (PayloadCodeUnknown*) by code.
type PayloadValidationError struct {
	Code   string // PayloadCodeSchema | PayloadCodeUnknownObjectType | PayloadCodeUnknownLinkType
	Field  string // dot-delimited instance path, e.g. "positions.n1.x"
	Reason string // human-readable explanation surfaced in the wire response
	Status int    // HTTP status code (400 or 422)
}

// Error implements the error interface so callers can errors.As against the
// concrete type.
func (e *PayloadValidationError) Error() string {
	if e.Field == "" {
		return e.Reason
	}
	return e.Field + ": " + e.Reason
}

// PayloadCode* are the discriminator values on PayloadValidationError.Code.
const (
	PayloadCodeSchema            = "schema"
	PayloadCodeUnknownObjectType = "unknownObjectType"
	PayloadCodeUnknownLinkType   = "unknownLinkType"
)

// ReferenceLookup is the slice of pkg/oms.Repository PayloadValidator needs to
// verify that objectType / linkType RIDs in a payload resolve to actual rows.
// Production wires *oms.PGRepository; tests pass a fake.
type ReferenceLookup interface {
	GetObjectType(ctx context.Context, rid string) (*oms.ObjectType, error)
	GetLinkType(ctx context.Context, rid string) (*oms.LinkType, error)
}

// PayloadValidator validates Vertex SystemGraph payloads against the embedded
// graph.schema.json (structural — 400 on failure) and against an OMS
// ReferenceLookup (semantic — 422 on dangling RID). The compiled schema is
// cached on construction so per-request cost is one Validate + a handful of
// Get* lookups.
type PayloadValidator struct {
	schema *jsonschema.Schema
	refs   ReferenceLookup
}

// NewPayloadValidator compiles the embedded graph schema and returns a
// validator. Passing a nil refs is supported — used by degraded-mode boots
// without an OMS pool — and disables 422 reference checks while leaving the
// schema (400) path active.
func NewPayloadValidator(refs ReferenceLookup) (*PayloadValidator, error) {
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft7
	if err := compiler.AddResource(payloadSchemaResource, strings.NewReader(string(payloadSchemaBytes))); err != nil {
		return nil, fmt.Errorf("vertex graph schema: add resource: %w", err)
	}
	schema, err := compiler.Compile(payloadSchemaResource)
	if err != nil {
		return nil, fmt.Errorf("vertex graph schema: compile: %w", err)
	}
	return &PayloadValidator{schema: schema, refs: refs}, nil
}

// Validate runs structural (JSON Schema) then semantic (reference) checks.
//
// An empty / nil payload is a no-op: the handler-level "missing payload" guards
// (POST: payload is optional, PUT: rejected upstream as MissingPayload) own
// that case so this validator stays focused on shape + references.
func (v *PayloadValidator) Validate(ctx context.Context, payload json.RawMessage) error {
	if len(payload) == 0 {
		return nil
	}
	var instance any
	if err := json.Unmarshal(payload, &instance); err != nil {
		return &PayloadValidationError{
			Code:   PayloadCodeSchema,
			Reason: "payload is not valid JSON: " + err.Error(),
			Status: http.StatusBadRequest,
		}
	}
	if err := v.schema.Validate(instance); err != nil {
		return schemaToPayloadError(err)
	}
	if v.refs == nil {
		return nil
	}
	return v.validateReferences(ctx, payload)
}

// schemaToPayloadError flattens a santhosh-tekuri ValidationError tree into a
// single user-facing PayloadValidationError. The leaf with the most specific
// instance path wins; if the failure is a missing required property at the
// root we surface "<name> field required" verbatim so the BDD expectation
// holds. Status is always 400 — semantic 422s come from validateReferences.
func schemaToPayloadError(err error) *PayloadValidationError {
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return &PayloadValidationError{
			Code:   PayloadCodeSchema,
			Reason: err.Error(),
			Status: http.StatusBadRequest,
		}
	}
	leaf := pickSchemaLeaf(ve)
	field := schemaInstanceField(leaf.InstanceLocation)
	reason := leaf.Message
	if name, ok := requiredFieldName(leaf); ok {
		// Make the missing-required message human-friendly and stable so
		// callers can grep on it ("layers field required").
		reason = name + " field required"
		if field == "" {
			field = name
		}
	}
	return &PayloadValidationError{
		Code:   PayloadCodeSchema,
		Field:  field,
		Reason: reason,
		Status: http.StatusBadRequest,
	}
}

// pickSchemaLeaf walks the ValidationError tree and returns the most specific
// leaf — i.e. the deepest node with no Causes. Falls back to the root when
// the tree is shallow (single-keyword failure).
func pickSchemaLeaf(ve *jsonschema.ValidationError) *jsonschema.ValidationError {
	if len(ve.Causes) == 0 {
		return ve
	}
	best := ve
	for _, c := range ve.Causes {
		cand := pickSchemaLeaf(c)
		if len(cand.InstanceLocation) >= len(best.InstanceLocation) {
			best = cand
		}
	}
	return best
}

// requiredFieldName extracts the property name from a "required" violation
// message (e.g. `missing properties: "layers"`). Returns ok=false for any
// other keyword so callers fall back to the raw schema message.
func requiredFieldName(ve *jsonschema.ValidationError) (string, bool) {
	if !strings.HasSuffix(ve.KeywordLocation, "/required") {
		return "", false
	}
	// santhosh-tekuri formats the message as: missing properties: 'foo', 'bar'
	// — single-quote delimited. We surface the first one in the reason.
	const marker = "missing properties: "
	idx := strings.Index(ve.Message, marker)
	if idx < 0 {
		return "", false
	}
	rest := ve.Message[idx+len(marker):]
	rest = strings.TrimSpace(rest)
	rest = strings.TrimPrefix(rest, "'")
	end := strings.IndexAny(rest, "',")
	if end < 0 {
		return strings.TrimSuffix(rest, "'"), true
	}
	return rest[:end], true
}

// schemaInstanceField turns a JSON Pointer ("/positions/n1/x") into the
// dot-delimited path the handler exposes on the wire ("positions.n1.x").
func schemaInstanceField(loc string) string {
	if loc == "" || loc == "/" {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(loc, "/"), "/")
	return strings.Join(parts, ".")
}

// minimalPayload is the subset of fields validateReferences needs after schema
// validation has guaranteed the shape is sound. We re-decode rather than carry
// the schema's any-typed instance through so this stays explicit about which
// fields drive the 422 path.
type minimalPayload struct {
	Layers []struct {
		ID            string `json:"id"`
		ObjectTypeRID string `json:"objectTypeRid"`
	} `json:"layers"`
	Edges []struct {
		ID          string `json:"id"`
		LinkTypeRID string `json:"linkTypeRid"`
	} `json:"edges"`
}

// validateReferences walks every layer.objectTypeRid + edge.linkTypeRid and
// confirms the row exists in OMS. Empty RIDs are skipped (handler-level checks
// own the "RID required" semantics; this is purely a not-found mapper).
func (v *PayloadValidator) validateReferences(ctx context.Context, payload json.RawMessage) error {
	var p minimalPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		// Schema already validated shape; if decode fails here something
		// upstream changed the schema without updating this struct. Return
		// a 400 rather than 500 so the user sees a helpful message.
		return &PayloadValidationError{
			Code:   PayloadCodeSchema,
			Reason: "payload references decode failed: " + err.Error(),
			Status: http.StatusBadRequest,
		}
	}
	seenObjectType := map[string]struct{}{}
	for i, l := range p.Layers {
		if l.ObjectTypeRID == "" {
			continue
		}
		if _, dup := seenObjectType[l.ObjectTypeRID]; dup {
			continue
		}
		seenObjectType[l.ObjectTypeRID] = struct{}{}
		if _, err := v.refs.GetObjectType(ctx, l.ObjectTypeRID); err != nil {
			if errors.Is(err, oms.ErrNotFound) {
				return &PayloadValidationError{
					Code:   PayloadCodeUnknownObjectType,
					Field:  fmt.Sprintf("layers.%d.objectTypeRid", i),
					Reason: fmt.Sprintf("objectType not found: %s", l.ObjectTypeRID),
					Status: http.StatusUnprocessableEntity,
				}
			}
			return fmt.Errorf("lookup objectType %q: %w", l.ObjectTypeRID, err)
		}
	}
	seenLinkType := map[string]struct{}{}
	for i, e := range p.Edges {
		if e.LinkTypeRID == "" {
			continue
		}
		if _, dup := seenLinkType[e.LinkTypeRID]; dup {
			continue
		}
		seenLinkType[e.LinkTypeRID] = struct{}{}
		if _, err := v.refs.GetLinkType(ctx, e.LinkTypeRID); err != nil {
			if errors.Is(err, oms.ErrNotFound) {
				return &PayloadValidationError{
					Code:   PayloadCodeUnknownLinkType,
					Field:  fmt.Sprintf("edges.%d.linkTypeRid", i),
					Reason: fmt.Sprintf("linkType not found: %s", e.LinkTypeRID),
					Status: http.StatusUnprocessableEntity,
				}
			}
			return fmt.Errorf("lookup linkType %q: %w", e.LinkTypeRID, err)
		}
	}
	return nil
}
