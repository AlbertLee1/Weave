package apierror

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// US-009: full table-driven coverage of every APIError constructor.
//
// For each constructor the table asserts:
//   - HTTP StatusCode mapping
//   - ErrorCode wire-format string
//   - ErrorName, Parameters preservation
//   - ErrorInstanceID is a fresh UUID per call
//   - JSON body schema is exactly the four Palantir wire-format fields
//   - JSON Marshal → Unmarshal round-trip preserves every wire-format field
//   - WriteJSON emits the same status code, Content-Type, and body
type us009Case struct {
	name       string
	build      func(name string, params map[string]string) *APIError
	wantStatus int
	wantCode   string
}

func us009Table() []us009Case {
	return []us009Case{
		{"NotFound", NewNotFound, http.StatusNotFound, "NOT_FOUND"},
		{"BadRequest", NewBadRequest, http.StatusBadRequest, "BAD_REQUEST"},
		{"InvalidParameter", NewInvalidParameter, http.StatusBadRequest, "INVALID_ARGUMENT"},
		{"PermissionDenied", NewPermissionDenied, http.StatusForbidden, "PERMISSION_DENIED"},
		{"Unauthorized", NewUnauthorized, http.StatusUnauthorized, "UNAUTHORIZED"},
		{"Conflict", NewConflict, http.StatusConflict, "CONFLICT"},
		{"Internal", NewInternal, http.StatusInternalServerError, "INTERNAL"},
		{"TooManyRequests", NewTooManyRequests, http.StatusTooManyRequests, "RESOURCE_EXHAUSTED"},
		{"RequestTimeout", NewRequestTimeout, http.StatusRequestTimeout, "DEADLINE_EXCEEDED"},
		{"ValidationEnum", NewValidationEnum, http.StatusUnprocessableEntity, "WEAVE_VALIDATION_ENUM"},
		{"QueryTooLarge", NewQueryTooLarge, http.StatusUnprocessableEntity, "WEAVE_QUERY_TOO_LARGE"},
		{"FunctionNondeterministic", NewFunctionNondeterministic, http.StatusConflict, "WEAVE_FUNCTION_NONDETERMINISTIC"},
		{"FunctionCallCycle", NewFunctionCallCycle, http.StatusUnprocessableEntity, "WEAVE_FUNCTION_CALL_CYCLE"},
		{"FunctionRecursionDepthExceeded", NewFunctionRecursionDepthExceeded, http.StatusUnprocessableEntity, "WEAVE_FUNCTION_RECURSION_DEPTH_EXCEEDED"},
		{"ValidationSchema", NewValidationSchema, http.StatusUnprocessableEntity, "WEAVE_VALIDATION_SCHEMA"},
		{"PipelineBreakingChange", NewPipelineBreakingChange, http.StatusUnprocessableEntity, "WEAVE_PIPELINE_BREAKING_CHANGE"},
	}
}

func TestUS009_ConstructorMapping(t *testing.T) {
	for _, tc := range us009Table() {
		t.Run(tc.name, func(t *testing.T) {
			params := map[string]string{
				"resource": "Employee",
				"detail":   "table-driven probe",
			}
			err := tc.build(tc.name+"Name", params)

			if err == nil {
				t.Fatalf("constructor returned nil")
			}
			if err.StatusCode != tc.wantStatus {
				t.Errorf("StatusCode: got %d, want %d", err.StatusCode, tc.wantStatus)
			}
			if err.ErrorCode != tc.wantCode {
				t.Errorf("ErrorCode: got %q, want %q", err.ErrorCode, tc.wantCode)
			}
			if err.ErrorName != tc.name+"Name" {
				t.Errorf("ErrorName: got %q, want %q", err.ErrorName, tc.name+"Name")
			}
			if got := err.Parameters["resource"]; got != "Employee" {
				t.Errorf("Parameters[resource]: got %q, want Employee", got)
			}
			if got := err.Parameters["detail"]; got != "table-driven probe" {
				t.Errorf("Parameters[detail]: got %q, want table-driven probe", got)
			}
			if !uuidRegex.MatchString(err.ErrorInstanceID) {
				t.Errorf("ErrorInstanceID is not a UUID: %q", err.ErrorInstanceID)
			}

			data, jerr := json.Marshal(err)
			if jerr != nil {
				t.Fatalf("json.Marshal: %v", jerr)
			}
			var wire map[string]any
			if jerr := json.Unmarshal(data, &wire); jerr != nil {
				t.Fatalf("json.Unmarshal: %v", jerr)
			}
			wantKeys := map[string]bool{
				"errorCode":       true,
				"errorName":       true,
				"errorInstanceId": true,
				"parameters":      true,
			}
			if len(wire) != len(wantKeys) {
				t.Errorf("wire JSON has %d keys, want %d (%v)", len(wire), len(wantKeys), wire)
			}
			for k := range wire {
				if !wantKeys[k] {
					t.Errorf("unexpected wire key %q", k)
				}
			}
			if wire["errorCode"] != tc.wantCode {
				t.Errorf("wire errorCode: got %v, want %q", wire["errorCode"], tc.wantCode)
			}
			if wire["errorName"] != tc.name+"Name" {
				t.Errorf("wire errorName: got %v, want %q", wire["errorName"], tc.name+"Name")
			}
			if wire["errorInstanceId"] != err.ErrorInstanceID {
				t.Errorf("wire errorInstanceId: got %v, want %q", wire["errorInstanceId"], err.ErrorInstanceID)
			}
			wireParams, ok := wire["parameters"].(map[string]any)
			if !ok {
				t.Fatalf("wire parameters is not an object: %v", wire["parameters"])
			}
			if wireParams["resource"] != "Employee" {
				t.Errorf("wire parameters[resource]: got %v", wireParams["resource"])
			}
			if wireParams["detail"] != "table-driven probe" {
				t.Errorf("wire parameters[detail]: got %v", wireParams["detail"])
			}

			rec := httptest.NewRecorder()
			WriteJSON(rec, err)
			if rec.Code != tc.wantStatus {
				t.Errorf("WriteJSON status: got %d, want %d", rec.Code, tc.wantStatus)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("WriteJSON Content-Type: got %q, want application/json", ct)
			}
			gotBody := bytes.TrimSpace(rec.Body.Bytes())
			wantBody := bytes.TrimSpace(data)
			if !bytes.Equal(gotBody, wantBody) {
				t.Errorf("WriteJSON body mismatch:\n  got:  %s\n  want: %s", gotBody, wantBody)
			}
		})
	}
}

func TestUS009_JSONRoundTripPreservesWireFields(t *testing.T) {
	for _, tc := range us009Table() {
		t.Run(tc.name, func(t *testing.T) {
			original := tc.build(tc.name+"Probe", map[string]string{
				"alpha": "one",
				"beta":  "two",
			})

			data, err := json.Marshal(original)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			var decoded wireFormat
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal into wireFormat: %v", err)
			}
			if decoded.ErrorCode != original.ErrorCode {
				t.Errorf("ErrorCode: got %q, want %q", decoded.ErrorCode, original.ErrorCode)
			}
			if decoded.ErrorName != original.ErrorName {
				t.Errorf("ErrorName: got %q, want %q", decoded.ErrorName, original.ErrorName)
			}
			if decoded.ErrorInstanceID != original.ErrorInstanceID {
				t.Errorf("ErrorInstanceID: got %q, want %q", decoded.ErrorInstanceID, original.ErrorInstanceID)
			}
			if len(decoded.Parameters) != len(original.Parameters) {
				t.Errorf("Parameters len: got %d, want %d", len(decoded.Parameters), len(original.Parameters))
			}
			for k, v := range original.Parameters {
				if decoded.Parameters[k] != v {
					t.Errorf("Parameters[%q]: got %q, want %q", k, decoded.Parameters[k], v)
				}
			}
		})
	}
}

func TestUS009_RequestIDUniqueAcrossCalls(t *testing.T) {
	const n = 64
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		err := NewNotFound("Probe", nil)
		if !uuidRegex.MatchString(err.ErrorInstanceID) {
			t.Fatalf("non-UUID errorInstanceId: %q", err.ErrorInstanceID)
		}
		if _, dup := seen[err.ErrorInstanceID]; dup {
			t.Fatalf("duplicate errorInstanceId after %d calls: %q", i, err.ErrorInstanceID)
		}
		seen[err.ErrorInstanceID] = struct{}{}
	}
}

func TestUS009_RequestIDDistinctAcrossConstructors(t *testing.T) {
	table := us009Table()
	ids := make(map[string]string, len(table))
	for _, tc := range table {
		err := tc.build("Probe", nil)
		if other, dup := ids[err.ErrorInstanceID]; dup {
			t.Fatalf("constructor %s collided with %s on id %q", tc.name, other, err.ErrorInstanceID)
		}
		ids[err.ErrorInstanceID] = tc.name
	}
}

func TestUS009_NilParamsNormalizedToEmptyJSONObject(t *testing.T) {
	for _, tc := range us009Table() {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.build("Probe", nil)
			if err.Parameters == nil {
				t.Fatalf("Parameters not normalized away from nil")
			}
			if len(err.Parameters) != 0 {
				t.Errorf("Parameters length: got %d, want 0", len(err.Parameters))
			}
			data, jerr := json.Marshal(err)
			if jerr != nil {
				t.Fatalf("Marshal: %v", jerr)
			}
			if !bytes.Contains(data, []byte(`"parameters":{}`)) {
				t.Errorf("Marshal should emit empty parameters object, got %s", data)
			}
		})
	}
}

func TestUS009_ErrorMethod_FormatsCodeNameInstance(t *testing.T) {
	cases := []struct {
		name string
		err  *APIError
	}{
		{"NotFound", NewNotFound("ObjectMissing", nil)},
		{"Internal", NewInternal("BoomServer", nil)},
		{"Conflict", NewConflict("AlreadyApproved", nil)},
		{"FunctionCallCycle", NewFunctionCallCycle("CycleSeen", map[string]string{"cycle": "A->A"})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.err.Error()
			if !strings.Contains(got, tc.err.ErrorCode) {
				t.Errorf("Error() should embed ErrorCode %q, got %q", tc.err.ErrorCode, got)
			}
			if !strings.Contains(got, tc.err.ErrorName) {
				t.Errorf("Error() should embed ErrorName %q, got %q", tc.err.ErrorName, got)
			}
			if !strings.Contains(got, tc.err.ErrorInstanceID) {
				t.Errorf("Error() should embed ErrorInstanceID %q, got %q", tc.err.ErrorInstanceID, got)
			}
		})
	}
}

func TestUS009_WireFormat_HidesGoStateFields(t *testing.T) {
	err := NewInternal("HiddenState", map[string]string{"k": "v"})
	data, jerr := json.Marshal(err)
	if jerr != nil {
		t.Fatalf("Marshal: %v", jerr)
	}
	body := string(data)
	// Status code is internal to the Go struct; clients learn HTTP status from
	// the response, not from the JSON body. Likewise the Go field names with
	// capital first letters must not leak.
	forbidden := []string{
		`"StatusCode"`,
		`"ErrorCode"`,
		`"ErrorName"`,
		`"ErrorInstanceID"`,
		`"Parameters"`,
		`"500"`,
	}
	for _, needle := range forbidden {
		if strings.Contains(body, needle) {
			t.Errorf("wire JSON leaked %q: %s", needle, body)
		}
	}
}

func TestUS009_WriteJSON_BodyMatchesMarshalForEveryConstructor(t *testing.T) {
	for _, tc := range us009Table() {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.build("Probe", map[string]string{"x": "y"})
			rec := httptest.NewRecorder()
			WriteJSON(rec, err)

			data, jerr := json.Marshal(err)
			if jerr != nil {
				t.Fatalf("Marshal: %v", jerr)
			}
			got := bytes.TrimSpace(rec.Body.Bytes())
			want := bytes.TrimSpace(data)
			if !bytes.Equal(got, want) {
				t.Errorf("body mismatch for %s:\n  got:  %s\n  want: %s", tc.name, got, want)
			}
		})
	}
}
