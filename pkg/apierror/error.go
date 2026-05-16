package apierror

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
)

// APIError represents a Palantir-style API error response.
type APIError struct {
	ErrorCode       string            `json:"-"`
	ErrorName       string            `json:"-"`
	ErrorInstanceID string            `json:"-"`
	Parameters      map[string]string `json:"-"`
	StatusCode      int               `json:"-"`
}

// Error implements the error interface.
func (e *APIError) Error() string {
	return fmt.Sprintf("%s: %s (instance: %s)", e.ErrorCode, e.ErrorName, e.ErrorInstanceID)
}

// wireFormat is the Palantir wire-format JSON representation.
type wireFormat struct {
	ErrorCode       string            `json:"errorCode"`
	ErrorName       string            `json:"errorName"`
	ErrorInstanceID string            `json:"errorInstanceId"`
	Parameters      map[string]string `json:"parameters"`
}

// MarshalJSON outputs only the Palantir wire-format fields (excludes StatusCode).
func (e *APIError) MarshalJSON() ([]byte, error) {
	return json.Marshal(wireFormat{
		ErrorCode:       e.ErrorCode,
		ErrorName:       e.ErrorName,
		ErrorInstanceID: e.ErrorInstanceID,
		Parameters:      e.Parameters,
	})
}

func newAPIError(code, name string, params map[string]string, status int) *APIError {
	if params == nil {
		params = map[string]string{}
	}
	return &APIError{
		ErrorCode:       code,
		ErrorName:       name,
		ErrorInstanceID: uuid.New().String(),
		Parameters:      params,
		StatusCode:      status,
	}
}

// NewNotFound creates a NOT_FOUND API error (HTTP 404).
func NewNotFound(name string, params map[string]string) *APIError {
	return newAPIError("NOT_FOUND", name, params, http.StatusNotFound)
}

// NewBadRequest creates a BAD_REQUEST API error (HTTP 400).
func NewBadRequest(name string, params map[string]string) *APIError {
	return newAPIError("BAD_REQUEST", name, params, http.StatusBadRequest)
}

// NewInvalidParameter creates an INVALID_ARGUMENT API error (HTTP 400).
func NewInvalidParameter(name string, params map[string]string) *APIError {
	return newAPIError("INVALID_ARGUMENT", name, params, http.StatusBadRequest)
}

// NewPermissionDenied creates a PERMISSION_DENIED API error (HTTP 403).
func NewPermissionDenied(name string, params map[string]string) *APIError {
	return newAPIError("PERMISSION_DENIED", name, params, http.StatusForbidden)
}

// NewUnauthorized creates an UNAUTHORIZED API error (HTTP 401).
func NewUnauthorized(name string, params map[string]string) *APIError {
	return newAPIError("UNAUTHORIZED", name, params, http.StatusUnauthorized)
}

// NewConflict creates a CONFLICT API error (HTTP 409).
func NewConflict(name string, params map[string]string) *APIError {
	return newAPIError("CONFLICT", name, params, http.StatusConflict)
}

// NewGone creates a NOT_FOUND-shaped 410 Gone API error. Used by surfaces
// that distinguish "never existed" (404) from "existed but was deliberately
// revoked" (410) — e.g. VTX-013 share links after the owner deletes them.
func NewGone(name string, params map[string]string) *APIError {
	return newAPIError("NOT_FOUND", name, params, http.StatusGone)
}

// NewInternal creates an INTERNAL error (HTTP 500).
func NewInternal(name string, params map[string]string) *APIError {
	return newAPIError("INTERNAL", name, params, http.StatusInternalServerError)
}

// NewTooManyRequests creates a RESOURCE_EXHAUSTED API error (HTTP 429). Used
// by rate-limited surfaces such as the SSE subscribe endpoint when the
// per-user connection cap has been reached.
func NewTooManyRequests(name string, params map[string]string) *APIError {
	return newAPIError("RESOURCE_EXHAUSTED", name, params, http.StatusTooManyRequests)
}

// NewRequestTimeout creates a DEADLINE_EXCEEDED API error (HTTP 408).
// Used by long-running compute surfaces such as the Function execute
// endpoint (US-218) when the per-call CPU budget is exhausted.
func NewRequestTimeout(name string, params map[string]string) *APIError {
	return newAPIError("DEADLINE_EXCEEDED", name, params, http.StatusRequestTimeout)
}

// NewValidationEnum creates a WEAVE_VALIDATION_ENUM API error (HTTP 422).
// US-208: returned by the EditBatch validation path when a property value
// falls outside the ValueType's enum constraint. Callers should populate
// Parameters with at minimum `property`, `value`, and `allowedValues`.
func NewValidationEnum(name string, params map[string]string) *APIError {
	return newAPIError("WEAVE_VALIDATION_ENUM", name, params, http.StatusUnprocessableEntity)
}

// NewQueryTooLarge creates a WEAVE_QUERY_TOO_LARGE API error (HTTP 422).
// US-366: returned by the multi-hop searchAround executor when the deduped
// intermediate working set exceeds SearchAroundIntermediateCap. Callers
// populate Parameters with at minimum `cap`, and may include `hop`,
// `linkApiName`, and `intermediateSize` to help the user retune the query.
func NewQueryTooLarge(name string, params map[string]string) *APIError {
	return newAPIError("WEAVE_QUERY_TOO_LARGE", name, params, http.StatusUnprocessableEntity)
}

// NewFunctionNondeterministic creates a WEAVE_FUNCTION_NONDETERMINISTIC API
// error (HTTP 409). US-370: returned by POST /functions/{rid}/replay when the
// fresh execution's output hash diverges from the persisted historical hash.
// Callers populate Parameters with at minimum `executionId`, `originalHash`,
// `replayHash` so SDK consumers can surface a meaningful audit notice.
func NewFunctionNondeterministic(name string, params map[string]string) *APIError {
	return newAPIError("WEAVE_FUNCTION_NONDETERMINISTIC", name, params, http.StatusConflict)
}

// NewFunctionCallCycle creates a WEAVE_FUNCTION_CALL_CYCLE API error
// (HTTP 422). US-371: returned by the Function publish path when static
// analysis of weave.callFunction(...) targets surfaces a cycle in the
// resulting call graph (A→B→A, self-recursion, or any longer ring). Callers
// populate Parameters with at minimum `name` (the rejected function's
// name@version), `cycle` (the dotted cycle path), and may include the
// offending downstream `target` so SDK clients can guide the author back
// to the broken edge without a second round-trip.
func NewFunctionCallCycle(name string, params map[string]string) *APIError {
	return newAPIError("WEAVE_FUNCTION_CALL_CYCLE", name, params, http.StatusUnprocessableEntity)
}

// NewFunctionRecursionDepthExceeded creates a
// WEAVE_FUNCTION_RECURSION_DEPTH_EXCEEDED API error (HTTP 422). US-371: the
// runtime sentinel surfaced when weave.callFunction tries to grow the call
// stack past fncall.MaxDepth (8 frames). Callers populate Parameters with at
// minimum `depth` (the rejected nesting level), `limit` (the configured
// ceiling), and `ref` (the function ref the runtime refused to dispatch).
func NewFunctionRecursionDepthExceeded(name string, params map[string]string) *APIError {
	return newAPIError("WEAVE_FUNCTION_RECURSION_DEPTH_EXCEEDED", name, params, http.StatusUnprocessableEntity)
}

// NewValidationSchema creates a WEAVE_VALIDATION_SCHEMA API error (HTTP 422).
// US-245: returned by the ActionType parameter-validation DSL when a request
// violates the declared JSON Schema (Draft-07). Callers populate Parameters
// with at minimum `field` (instance path), `reason` (schema message) and
// optionally `keyword` (violated JSON Schema keyword).
func NewValidationSchema(name string, params map[string]string) *APIError {
	return newAPIError("WEAVE_VALIDATION_SCHEMA", name, params, http.StatusUnprocessableEntity)
}

// NewAutomationRuleCycle creates a WEAVE_AUTOMATION_RULE_CYCLE API error
// (HTTP 422). US-477: returned by the Automate rule register / update path
// when topological sort of the action→event→action graph surfaces a cycle
// (A→B→A, self-loop, or any longer ring). Callers populate Parameters with
// at minimum `cycle` (the dotted cycle path) and `ruleId` so SDK consumers
// can guide the author back to the broken edge.
func NewAutomationRuleCycle(name string, params map[string]string) *APIError {
	return newAPIError("WEAVE_AUTOMATION_RULE_CYCLE", name, params, http.StatusUnprocessableEntity)
}

// NewPipelineBreakingChange creates a WEAVE_PIPELINE_BREAKING_CHANGE API error
// (HTTP 422). US-378: returned when an APPEND-mode pipeline run detects a
// schema diff that drops or alters the type of a column the pipeline has
// previously committed against. Callers populate Parameters with at minimum
// `pipelineId`, `dropped` (comma-joined list of removed column names) and
// optionally `conflicts` (comma-joined "name:oldType→newType" pairs).
func NewPipelineBreakingChange(name string, params map[string]string) *APIError {
	return newAPIError("WEAVE_PIPELINE_BREAKING_CHANGE", name, params, http.StatusUnprocessableEntity)
}

// WriteJSON writes an APIError as a JSON HTTP response with the appropriate status code.
func WriteJSON(w http.ResponseWriter, err *APIError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(err.StatusCode)
	json.NewEncoder(w).Encode(err)
}
