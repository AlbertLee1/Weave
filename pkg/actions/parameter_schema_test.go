package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/oms"
)

// ---------------------------------------------------------------------------
// ParameterSchemaValidator unit tests — US-245 Parameter Validation DSL.
//
// These tests exercise the validator in isolation; Prepare-level integration
// is covered by the TestPrepare_* suite below.
// ---------------------------------------------------------------------------

func TestParameterSchemaValidator_NilSchema_IsNoop(t *testing.T) {
	v := NewParameterSchemaValidator()
	if err := v.Validate(nil, map[string]interface{}{"x": 1}); err != nil {
		t.Fatalf("nil schema must be a no-op, got %v", err)
	}
	if err := v.Validate(json.RawMessage("null"), nil); err != nil {
		t.Fatalf("null schema must be a no-op, got %v", err)
	}
	if err := v.Validate(json.RawMessage(""), nil); err != nil {
		t.Fatalf("empty schema must be a no-op, got %v", err)
	}
}

func TestParameterSchemaValidator_RequiredField_Missing(t *testing.T) {
	v := NewParameterSchemaValidator()
	schema := json.RawMessage(`{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"type": "object",
		"properties": {"name": {"type": "string"}},
		"required": ["name"]
	}`)
	err := v.Validate(schema, map[string]interface{}{})
	if err == nil {
		t.Fatal("expected violation for missing required field")
	}
	var schemaErr *ParameterSchemaError
	if !errors.As(err, &schemaErr) {
		t.Fatalf("expected *ParameterSchemaError, got %T: %v", err, err)
	}
	if len(schemaErr.Violations) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(schemaErr.Violations), schemaErr.Violations)
	}
	if schemaErr.Violations[0].Keyword != "required" {
		t.Errorf("keyword: expected required, got %q", schemaErr.Violations[0].Keyword)
	}
}

func TestParameterSchemaValidator_Pattern_Violation(t *testing.T) {
	v := NewParameterSchemaValidator()
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {"email": {"type": "string", "pattern": "^[^@]+@[^@]+$"}}
	}`)
	err := v.Validate(schema, map[string]interface{}{"email": "not-an-email"})
	if err == nil {
		t.Fatal("expected violation for pattern mismatch")
	}
	var schemaErr *ParameterSchemaError
	if !errors.As(err, &schemaErr) {
		t.Fatalf("expected typed error, got %v", err)
	}
	violation := schemaErr.Violations[0]
	if violation.Field != "email" {
		t.Errorf("field: expected email, got %q", violation.Field)
	}
	if violation.Keyword != "pattern" {
		t.Errorf("keyword: expected pattern, got %q", violation.Keyword)
	}
}

func TestParameterSchemaValidator_Range_Violation(t *testing.T) {
	v := NewParameterSchemaValidator()
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {"age": {"type": "integer", "minimum": 0, "maximum": 150}}
	}`)
	err := v.Validate(schema, map[string]interface{}{"age": 200})
	if err == nil {
		t.Fatal("expected violation for out-of-range value")
	}
	var schemaErr *ParameterSchemaError
	if !errors.As(err, &schemaErr) {
		t.Fatalf("expected typed error, got %v", err)
	}
	if schemaErr.Violations[0].Field != "age" {
		t.Errorf("field: expected age, got %q", schemaErr.Violations[0].Field)
	}
	if schemaErr.Violations[0].Keyword != "maximum" {
		t.Errorf("keyword: expected maximum, got %q", schemaErr.Violations[0].Keyword)
	}
}

func TestParameterSchemaValidator_Dependency_Violation(t *testing.T) {
	v := NewParameterSchemaValidator()
	// Draft-07 "dependencies": if "creditCard" is present, "billingAddress"
	// must be too.
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"creditCard": {"type": "string"},
			"billingAddress": {"type": "string"}
		},
		"dependencies": {
			"creditCard": ["billingAddress"]
		}
	}`)
	err := v.Validate(schema, map[string]interface{}{"creditCard": "4111"})
	if err == nil {
		t.Fatal("expected dependency violation")
	}
	var schemaErr *ParameterSchemaError
	if !errors.As(err, &schemaErr) {
		t.Fatalf("expected typed error, got %v", err)
	}
	if len(schemaErr.Violations) == 0 {
		t.Fatal("expected at least one violation")
	}
}

func TestParameterSchemaValidator_MultipleViolations(t *testing.T) {
	v := NewParameterSchemaValidator()
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "minLength": 2},
			"age":  {"type": "integer", "minimum": 0}
		},
		"required": ["name", "age"]
	}`)
	err := v.Validate(schema, map[string]interface{}{"name": "a", "age": -1})
	if err == nil {
		t.Fatal("expected multiple violations")
	}
	var schemaErr *ParameterSchemaError
	if !errors.As(err, &schemaErr) {
		t.Fatalf("expected typed error, got %v", err)
	}
	if len(schemaErr.Violations) < 2 {
		t.Fatalf("expected >=2 violations, got %d: %+v", len(schemaErr.Violations), schemaErr.Violations)
	}
}

func TestParameterSchemaValidator_HappyPath(t *testing.T) {
	v := NewParameterSchemaValidator()
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "minLength": 1},
			"age":  {"type": "integer", "minimum": 0, "maximum": 150}
		},
		"required": ["name", "age"]
	}`)
	if err := v.Validate(schema, map[string]interface{}{
		"name": "Alice",
		"age":  30,
	}); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestParameterSchemaValidator_CompileCache_ReusesCompilation(t *testing.T) {
	v := NewParameterSchemaValidator()
	schema := json.RawMessage(`{"type": "object"}`)
	for i := 0; i < 10; i++ {
		if err := v.Validate(schema, map[string]interface{}{}); err != nil {
			t.Fatalf("iter %d: unexpected error %v", i, err)
		}
	}
	// Ensure a cache entry was recorded for this schema.
	v.mu.RLock()
	if _, ok := v.cache[string(schema)]; !ok {
		v.mu.RUnlock()
		t.Fatal("expected cache hit for repeated schema")
	}
	v.mu.RUnlock()
}

func TestParameterSchemaValidator_MalformedSchema_CompileError(t *testing.T) {
	v := NewParameterSchemaValidator()
	// Not valid JSON at all.
	err := v.Validate(json.RawMessage(`{not-json`), nil)
	if err == nil {
		t.Fatal("expected compile error on malformed schema")
	}
	if !strings.Contains(err.Error(), "compile parameter schema") {
		t.Errorf("expected compile wrapper, got %v", err)
	}
	// Typed error should NOT be returned for compile failures — the handler
	// falls back to ActionFailed 400.
	var schemaErr *ParameterSchemaError
	if errors.As(err, &schemaErr) {
		t.Fatalf("compile error should not surface as typed ParameterSchemaError, got %v", schemaErr)
	}
}

func TestParameterSchemaError_APIError_Structure(t *testing.T) {
	err := &ParameterSchemaError{
		Violations: []ParameterSchemaViolation{
			{Field: "email", Keyword: "pattern", Reason: "pattern mismatch"},
			{Field: "age", Keyword: "minimum", Reason: "must be >= 0"},
		},
	}
	apiErr := err.APIError()
	if apiErr.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status: expected 422, got %d", apiErr.StatusCode)
	}
	if apiErr.ErrorCode != "WEAVE_VALIDATION_SCHEMA" {
		t.Errorf("errorCode: expected WEAVE_VALIDATION_SCHEMA, got %q", apiErr.ErrorCode)
	}
	if apiErr.ErrorName != "ParameterSchemaViolation" {
		t.Errorf("errorName: expected ParameterSchemaViolation, got %q", apiErr.ErrorName)
	}
	if apiErr.Parameters["field"] != "email" {
		t.Errorf("field: expected email (first violation), got %q", apiErr.Parameters["field"])
	}
	if apiErr.Parameters["keyword"] != "pattern" {
		t.Errorf("keyword: expected pattern, got %q", apiErr.Parameters["keyword"])
	}
	// Structured list is surfaced on the wire so SDKs can read every violation.
	if apiErr.Parameters["violations"] == "" {
		t.Error("expected violations JSON to be populated")
	}
	var round []ParameterSchemaViolation
	if err := json.Unmarshal([]byte(apiErr.Parameters["violations"]), &round); err != nil {
		t.Fatalf("violations JSON invalid: %v", err)
	}
	if len(round) != 2 {
		t.Errorf("expected 2 violations after round-trip, got %d", len(round))
	}
}

func TestHasParameterSchema(t *testing.T) {
	cases := []struct {
		name string
		raw  json.RawMessage
		want bool
	}{
		{"nil", nil, false},
		{"empty", json.RawMessage(""), false},
		{"whitespace", json.RawMessage("   "), false},
		{"null-literal", json.RawMessage("null"), false},
		{"null-with-whitespace", json.RawMessage("  null  "), false},
		{"empty-object", json.RawMessage("{}"), true},
		{"real-schema", json.RawMessage(`{"type":"object"}`), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasParameterSchema(tc.raw); got != tc.want {
				t.Errorf("hasParameterSchema(%q) = %v, want %v", string(tc.raw), got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Prepare integration — schema violation surfaces as *apierror.APIError with
// 422 status code so the handler can render structured field-level detail.
// ---------------------------------------------------------------------------

func newSchemaActionType(apiName string, params []ParameterDef, rules []Rule, schema json.RawMessage) oms.ActionType {
	at := newTestActionType(apiName, params, rules)
	at.ParameterSchema = schema
	return at
}

func TestPrepare_ParameterSchema_RequiredViolation_EmitsTyped422(t *testing.T) {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "minLength": 2}
		},
		"required": ["name"]
	}`)
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newSchemaActionType("createUser",
				[]ParameterDef{{ID: "name", Type: "string"}},
				[]Rule{
					{Type: "createObject", ObjectType: "User",
						PropertyBindings: map[string]PropertyBinding{
							"name": {Type: "parameter", Value: "name"},
						}},
				},
				schema),
		},
	}
	exec := NewExecutor(repo, &fakePublisher{})
	_, err := exec.Prepare(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "createUser",
		Parameters: map[string]interface{}{},
	})
	if err == nil {
		t.Fatal("expected schema violation to fail Prepare")
	}
	var apiErr *apierror.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected Prepare to surface *apierror.APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status: expected 422, got %d", apiErr.StatusCode)
	}
	if apiErr.ErrorCode != "WEAVE_VALIDATION_SCHEMA" {
		t.Errorf("errorCode: expected WEAVE_VALIDATION_SCHEMA, got %q", apiErr.ErrorCode)
	}
	if apiErr.Parameters["keyword"] != "required" {
		t.Errorf("keyword: expected required, got %q", apiErr.Parameters["keyword"])
	}
}

func TestPrepare_ParameterSchema_PatternViolation_EmitsTyped422(t *testing.T) {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {"email": {"type": "string", "pattern": "^[^@]+@[^@]+$"}}
	}`)
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newSchemaActionType("setEmail",
				[]ParameterDef{{ID: "email", Type: "string"}},
				[]Rule{
					{Type: "createObject", ObjectType: "User",
						PropertyBindings: map[string]PropertyBinding{
							"email": {Type: "parameter", Value: "email"},
						}},
				},
				schema),
		},
	}
	exec := NewExecutor(repo, &fakePublisher{})
	_, err := exec.Prepare(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "setEmail",
		Parameters: map[string]interface{}{"email": "bogus"},
	})
	if err == nil {
		t.Fatal("expected schema violation to fail Prepare")
	}
	var apiErr *apierror.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *apierror.APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status: expected 422, got %d", apiErr.StatusCode)
	}
	if apiErr.Parameters["field"] != "email" {
		t.Errorf("field: expected email, got %q", apiErr.Parameters["field"])
	}
}

func TestPrepare_ParameterSchema_HappyPath_StillPrepares(t *testing.T) {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "minLength": 1}
		},
		"required": ["name"]
	}`)
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newSchemaActionType("createUser",
				[]ParameterDef{{ID: "name", Type: "string", Required: true}},
				[]Rule{
					{Type: "createObject", ObjectType: "User",
						PropertyBindings: map[string]PropertyBinding{
							"name": {Type: "parameter", Value: "name"},
						}},
				},
				schema),
		},
	}
	exec := NewExecutor(repo, &fakePublisher{})
	prep, err := exec.Prepare(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "createUser",
		Parameters: map[string]interface{}{"name": "Alice"},
	})
	if err != nil {
		t.Fatalf("expected Prepare to succeed, got %v", err)
	}
	if len(prep.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(prep.Edits))
	}
}

func TestHandler_Apply_SchemaViolation_EmitsWire422(t *testing.T) {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"age": {"type": "integer", "minimum": 0, "maximum": 150}
		},
		"required": ["age"]
	}`)
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newSchemaActionType("setAge",
				[]ParameterDef{{ID: "age", Type: "integer"}},
				[]Rule{
					{Type: "createObject", ObjectType: "Person",
						PropertyBindings: map[string]PropertyBinding{
							"age": {Type: "parameter", Value: "age"},
						}},
				},
				schema),
		},
	}
	exec := NewExecutor(repo, &fakePublisher{})
	router := setupRouter(NewHandler(exec))

	body := mustJSON(map[string]interface{}{
		"parameters": map[string]interface{}{"age": 999},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/setAge/apply", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	var wire struct {
		ErrorCode  string            `json:"errorCode"`
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if wire.ErrorCode != "WEAVE_VALIDATION_SCHEMA" {
		t.Errorf("errorCode: expected WEAVE_VALIDATION_SCHEMA, got %q", wire.ErrorCode)
	}
	if wire.Parameters["field"] != "age" {
		t.Errorf("field: expected age, got %q", wire.Parameters["field"])
	}
	if wire.Parameters["keyword"] != "maximum" {
		t.Errorf("keyword: expected maximum, got %q", wire.Parameters["keyword"])
	}
	if wire.Parameters["violations"] == "" {
		t.Error("expected structured violations array on the wire")
	}
}

func TestPrepare_ParameterSchema_EmptySchema_Ignored(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newSchemaActionType("createUser",
				[]ParameterDef{{ID: "name", Type: "string", Required: true}},
				[]Rule{
					{Type: "createObject", ObjectType: "User",
						PropertyBindings: map[string]PropertyBinding{
							"name": {Type: "parameter", Value: "name"},
						}},
				},
				nil),
		},
	}
	exec := NewExecutor(repo, &fakePublisher{})
	prep, err := exec.Prepare(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "createUser",
		Parameters: map[string]interface{}{"name": "Alice"},
	})
	if err != nil {
		t.Fatalf("expected Prepare to succeed when no schema declared, got %v", err)
	}
	if len(prep.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(prep.Edits))
	}
}
