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

// WriteJSON writes an APIError as a JSON HTTP response with the appropriate status code.
func WriteJSON(w http.ResponseWriter, err *APIError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(err.StatusCode)
	json.NewEncoder(w).Encode(err)
}
