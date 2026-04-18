package actions

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/liyang/weave/pkg/apierror"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v5"
)

// parameterSchemaResource is the in-memory URL under which action-type JSON
// Schemas are registered with the jsonschema compiler. The value is arbitrary
// but must be stable across calls within a single Compile — draft-07 allows
// absolute URIs to be used as identifiers.
const parameterSchemaResource = "mem://action-parameter-schema.json"

// ParameterSchemaViolation represents a single field-level JSON Schema
// violation surfaced on the wire. Callers can walk the slice to produce a
// structured 422 payload; the legacy single-field Parameters map on
// *apierror.APIError carries only the first violation.
type ParameterSchemaViolation struct {
	Field   string `json:"field"`
	Keyword string `json:"keyword,omitempty"`
	Reason  string `json:"reason"`
}

// ParameterSchemaError is the typed error returned by ValidateParameterSchema
// when the declared schema rejects the request body. It implements Error()
// with a human message AND exposes the full list of per-field violations for
// structured handling. The legacy executor path collapses it to a wrapped
// *apierror.APIError; callers that need the full list can use errors.As.
type ParameterSchemaError struct {
	Violations []ParameterSchemaViolation
}

// Error implements the error interface. Emits a compact "<n> violation(s): ..."
// summary so logs and legacy callers still get a useful single-line message.
func (e *ParameterSchemaError) Error() string {
	if len(e.Violations) == 0 {
		return "parameter schema: violation"
	}
	first := e.Violations[0]
	if len(e.Violations) == 1 {
		return fmt.Sprintf("parameter schema: %s: %s", first.Field, first.Reason)
	}
	return fmt.Sprintf("parameter schema: %s: %s (and %d more)", first.Field, first.Reason, len(e.Violations)-1)
}

// APIError materialises the first violation into a 422 WEAVE_VALIDATION_SCHEMA
// payload. The handler path uses this via errors.As so every schema failure
// emits the same status code + parameter shape. The `violations` key carries
// the full JSON-encoded list for clients that want every field at once.
func (e *ParameterSchemaError) APIError() *apierror.APIError {
	if e == nil || len(e.Violations) == 0 {
		return apierror.NewValidationSchema("ParameterSchemaViolation",
			map[string]string{"reason": "unknown schema violation"})
	}
	first := e.Violations[0]
	params := map[string]string{
		"field":  first.Field,
		"reason": first.Reason,
	}
	if first.Keyword != "" {
		params["keyword"] = first.Keyword
	}
	if raw, err := json.Marshal(e.Violations); err == nil {
		params["violations"] = string(raw)
	}
	return apierror.NewValidationSchema("ParameterSchemaViolation", params)
}

// ParameterSchemaValidator caches a compiled *jsonschema.Schema keyed by the
// schema document's byte content. The Prepare path invokes Validate once per
// action call, so caching the compile output keeps the hot path cheap —
// ActionType.ParameterSchema is an effectively-immutable wire blob per row.
type ParameterSchemaValidator struct {
	mu    sync.RWMutex
	cache map[string]*jsonschema.Schema
}

// NewParameterSchemaValidator returns a fresh validator with an empty cache.
// Tests can construct multiple instances without leaking state; production
// code keeps one on the Executor.
func NewParameterSchemaValidator() *ParameterSchemaValidator {
	return &ParameterSchemaValidator{cache: map[string]*jsonschema.Schema{}}
}

// Validate evaluates params against schema and returns a
// *ParameterSchemaError wrapped in a single-line error if any field fails.
// Empty/nil/"null" schemas are no-ops so callers can hand untyped legacy rows
// through without a pre-check. Compile errors (malformed Draft-07 document on
// the stored row) surface as a generic error — the handler collapses to the
// legacy ActionFailed 400 rather than misclassifying them as user errors.
func (v *ParameterSchemaValidator) Validate(schema json.RawMessage, params map[string]interface{}) error {
	if !hasParameterSchema(schema) {
		return nil
	}
	compiled, err := v.compile(schema)
	if err != nil {
		return fmt.Errorf("compile parameter schema: %w", err)
	}
	// santhosh-tekuri/jsonschema operates on any (already-decoded JSON). The
	// caller's params map is the natural instance — a nil map validates as
	// the empty object, matching the JSON Schema "{}" idiom.
	instance := paramsInstance(params)
	if err := compiled.Validate(instance); err != nil {
		return convertValidationError(err)
	}
	return nil
}

// compile fetches (or builds) the cached *jsonschema.Schema for the given
// schema bytes. Callers should NOT hold the RLock across Validate calls —
// compile releases it before returning the pointer.
func (v *ParameterSchemaValidator) compile(schema json.RawMessage) (*jsonschema.Schema, error) {
	key := string(schema)

	v.mu.RLock()
	if hit, ok := v.cache[key]; ok {
		v.mu.RUnlock()
		return hit, nil
	}
	v.mu.RUnlock()

	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft7
	if err := compiler.AddResource(parameterSchemaResource, strings.NewReader(key)); err != nil {
		return nil, err
	}
	compiled, err := compiler.Compile(parameterSchemaResource)
	if err != nil {
		return nil, err
	}

	v.mu.Lock()
	v.cache[key] = compiled
	v.mu.Unlock()
	return compiled, nil
}

// hasParameterSchema reports whether the raw message carries a non-empty,
// non-null JSON Schema blob. Mirrors the len/null guard used elsewhere in
// the codebase for optional JSONB fields.
func hasParameterSchema(schema json.RawMessage) bool {
	if len(schema) == 0 {
		return false
	}
	trimmed := strings.TrimSpace(string(schema))
	return trimmed != "" && trimmed != "null"
}

// paramsInstance produces the JSON-shaped value handed to jsonschema.Validate.
// A nil params map is validated as the empty object `{}` so an ActionType
// with a schema but no required fields passes. Non-nil maps are handed
// through unchanged; jsonschema accepts any.
func paramsInstance(params map[string]interface{}) interface{} {
	if params == nil {
		return map[string]interface{}{}
	}
	return params
}

// convertValidationError flattens a nested *jsonschema.ValidationError tree
// into a ParameterSchemaError carrying one ParameterSchemaViolation per leaf.
// Leaves are identified by having no Causes — the root envelope is skipped.
func convertValidationError(err error) error {
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return err
	}
	violations := collectLeafViolations(ve)
	if len(violations) == 0 {
		// Shouldn't happen for a well-formed jsonschema error, but fall back
		// to the root message so callers never see an empty Violations slice.
		violations = []ParameterSchemaViolation{{
			Field:  instanceField(ve.InstanceLocation),
			Reason: ve.Message,
		}}
	}
	return &ParameterSchemaError{Violations: violations}
}

// collectLeafViolations walks the ValidationError tree and returns the
// deepest nodes — those with no Causes. santhosh-tekuri emits an envelope at
// the root ("doesn't validate with ...") and the actual per-keyword failures
// as leaves, so this walk yields the user-facing detail.
func collectLeafViolations(ve *jsonschema.ValidationError) []ParameterSchemaViolation {
	if ve == nil {
		return nil
	}
	if len(ve.Causes) == 0 {
		return []ParameterSchemaViolation{{
			Field:   instanceField(ve.InstanceLocation),
			Keyword: keywordName(ve.KeywordLocation),
			Reason:  ve.Message,
		}}
	}
	var out []ParameterSchemaViolation
	for _, cause := range ve.Causes {
		out = append(out, collectLeafViolations(cause)...)
	}
	return out
}

// instanceField turns the library's "/name" JSON Pointer into a
// dot-delimited field path ("name", "items.0.price", or "" for the root).
// Callers read it directly into the HTTP error's `field` parameter.
func instanceField(loc string) string {
	if loc == "" || loc == "/" {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(loc, "/"), "/")
	return strings.Join(parts, ".")
}

// keywordName extracts the trailing keyword name (the JSON Schema keyword
// that failed — e.g. "required", "pattern", "minimum") from a KeywordLocation
// like "/properties/name/required". Empty location yields "".
func keywordName(loc string) string {
	if loc == "" {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(loc, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}
