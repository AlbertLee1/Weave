package apierror

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

var uuidRegex = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func TestNewNotFoundError(t *testing.T) {
	err := NewNotFound("ObjectNotFound", map[string]string{
		"objectType": "Employee",
		"primaryKey": "123",
	})

	if err.ErrorCode != "NOT_FOUND" {
		t.Errorf("expected errorCode NOT_FOUND, got %s", err.ErrorCode)
	}
	if err.ErrorName != "ObjectNotFound" {
		t.Errorf("expected errorName ObjectNotFound, got %s", err.ErrorName)
	}
	if !uuidRegex.MatchString(err.ErrorInstanceID) {
		t.Errorf("expected valid UUID for errorInstanceId, got %s", err.ErrorInstanceID)
	}
	if err.Parameters["objectType"] != "Employee" {
		t.Errorf("expected parameter objectType=Employee, got %s", err.Parameters["objectType"])
	}
	if err.Parameters["primaryKey"] != "123" {
		t.Errorf("expected parameter primaryKey=123, got %s", err.Parameters["primaryKey"])
	}
}

func TestNewInvalidParameterError(t *testing.T) {
	err := NewInvalidParameter("InvalidParameter:fieldName", map[string]string{
		"fieldName": "age",
		"reason":    "must be positive",
	})

	if err.ErrorCode != "INVALID_ARGUMENT" {
		t.Errorf("expected errorCode INVALID_ARGUMENT, got %s", err.ErrorCode)
	}
	if err.ErrorName != "InvalidParameter:fieldName" {
		t.Errorf("expected errorName to contain parameter info, got %s", err.ErrorName)
	}
}

func TestNewPermissionDeniedError(t *testing.T) {
	err := NewPermissionDenied("PermissionDenied", map[string]string{
		"resource": "Employee",
	})

	if err.ErrorCode != "PERMISSION_DENIED" {
		t.Errorf("expected errorCode PERMISSION_DENIED, got %s", err.ErrorCode)
	}
}

func TestNewConflictError(t *testing.T) {
	err := NewConflict("ObjectAlreadyExists", map[string]string{
		"objectType": "Employee",
	})

	if err.ErrorCode != "CONFLICT" {
		t.Errorf("expected errorCode CONFLICT, got %s", err.ErrorCode)
	}
}

func TestErrorJSON_Marshaling(t *testing.T) {
	apiErr := NewNotFound("ObjectNotFound", map[string]string{
		"objectType": "Employee",
		"primaryKey": "123",
	})

	data, err := json.Marshal(apiErr)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var wire map[string]interface{}
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Must contain exactly the four Palantir wire-format fields
	expectedKeys := map[string]bool{
		"errorCode":       true,
		"errorName":       true,
		"errorInstanceId": true,
		"parameters":      true,
	}
	for k := range wire {
		if !expectedKeys[k] {
			t.Errorf("unexpected key in JSON output: %s", k)
		}
	}
	for k := range expectedKeys {
		if _, ok := wire[k]; !ok {
			t.Errorf("missing expected key in JSON output: %s", k)
		}
	}

	if wire["errorCode"] != "NOT_FOUND" {
		t.Errorf("expected errorCode NOT_FOUND, got %v", wire["errorCode"])
	}
	if wire["errorName"] != "ObjectNotFound" {
		t.Errorf("expected errorName ObjectNotFound, got %v", wire["errorName"])
	}

	params, ok := wire["parameters"].(map[string]interface{})
	if !ok {
		t.Fatalf("parameters is not an object")
	}
	if params["objectType"] != "Employee" {
		t.Errorf("expected parameter objectType=Employee, got %v", params["objectType"])
	}
}

func TestErrorJSON_ContainsInstanceID(t *testing.T) {
	apiErr := NewNotFound("ObjectNotFound", nil)

	data, err := json.Marshal(apiErr)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var wire map[string]interface{}
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	instanceID, ok := wire["errorInstanceId"].(string)
	if !ok {
		t.Fatal("errorInstanceId is not a string")
	}
	if !uuidRegex.MatchString(instanceID) {
		t.Errorf("errorInstanceId is not a valid UUID: %s", instanceID)
	}
}

func TestWriteError_SetsHTTPStatus(t *testing.T) {
	tests := []struct {
		name       string
		err        *APIError
		wantStatus int
	}{
		{"NotFound", NewNotFound("NotFound", nil), http.StatusNotFound},
		{"InvalidArgument", NewInvalidParameter("InvalidParam", nil), http.StatusBadRequest},
		{"PermissionDenied", NewPermissionDenied("Denied", nil), http.StatusForbidden},
		{"Conflict", NewConflict("Conflict", nil), http.StatusConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			WriteJSON(w, tt.err)

			if w.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, w.Code)
			}
		})
	}
}

// US-371: the apierror constructors for runtime depth-exceeded and
// publish-time call-graph cycle must surface the named wire-format codes at
// HTTP 422.
func TestNewFunctionRecursionDepthExceeded_Code(t *testing.T) {
	err := NewFunctionRecursionDepthExceeded("FunctionRecursionDepthExceeded", map[string]string{
		"depth": "9",
		"limit": "8",
		"ref":   "helper",
	})
	if err.ErrorCode != "WEAVE_FUNCTION_RECURSION_DEPTH_EXCEEDED" {
		t.Fatalf("expected WEAVE_FUNCTION_RECURSION_DEPTH_EXCEEDED, got %q", err.ErrorCode)
	}
	if err.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", err.StatusCode)
	}
}

func TestNewFunctionCallCycle_Code(t *testing.T) {
	err := NewFunctionCallCycle("FunctionCallCycle", map[string]string{
		"name":  "A",
		"cycle": "A -> B -> A",
	})
	if err.ErrorCode != "WEAVE_FUNCTION_CALL_CYCLE" {
		t.Fatalf("expected WEAVE_FUNCTION_CALL_CYCLE, got %q", err.ErrorCode)
	}
	if err.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", err.StatusCode)
	}
}

// US-378: pipeline schema-evolution constructor surfaces the named
// wire-format code at HTTP 422.
func TestNewPipelineBreakingChange_Code(t *testing.T) {
	err := NewPipelineBreakingChange("PipelineBreakingChange", map[string]string{
		"pipelineId": "demo",
		"dropped":    "email",
	})
	if err.ErrorCode != "WEAVE_PIPELINE_BREAKING_CHANGE" {
		t.Fatalf("expected WEAVE_PIPELINE_BREAKING_CHANGE, got %q", err.ErrorCode)
	}
	if err.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", err.StatusCode)
	}
}

func TestWriteError_ContentType(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSON(w, NewNotFound("NotFound", nil))

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}
}
