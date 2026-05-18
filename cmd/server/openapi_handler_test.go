package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestOpenAPISpec_Served verifies that GET /api/openapi.yaml returns the
// embedded spec with a YAML content-type and a non-empty body.
func TestOpenAPISpec_Served(t *testing.T) {
	router := newContractTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/openapi.yaml", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "yaml") {
		t.Errorf("Content-Type: got %q, expected to contain 'yaml'", ct)
	}
	if rec.Body.Len() == 0 {
		t.Error("body is empty")
	}
}

// TestOpenAPISpec_IsValid parses the served YAML and asserts it is a
// well-formed OpenAPI 3.0.3 document with at least one operation.
func TestOpenAPISpec_IsValid(t *testing.T) {
	router := newContractTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/openapi.yaml", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var doc map[string]any
	if err := yaml.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("parse yaml: %v", err)
	}

	openapi, _ := doc["openapi"].(string)
	if !strings.HasPrefix(openapi, "3.0") {
		t.Errorf("openapi: got %q, want a 3.0.x version", openapi)
	}

	info, ok := doc["info"].(map[string]any)
	if !ok {
		t.Fatal("info: missing or wrong type")
	}
	if title, _ := info["title"].(string); title == "" {
		t.Error("info.title: missing")
	}
	if version, _ := info["version"].(string); version == "" {
		t.Error("info.version: missing")
	}

	paths, ok := doc["paths"].(map[string]any)
	if !ok || len(paths) == 0 {
		t.Fatal("paths: missing or empty")
	}
}

// TestOpenAPISpec_HasSecuritySchemes asserts that the spec defines the
// bearerAuth and apiKey securitySchemes that the API contracts depend on.
func TestOpenAPISpec_HasSecuritySchemes(t *testing.T) {
	doc := loadCanonicalSpec(t)
	components, _ := doc["components"].(map[string]any)
	if components == nil {
		t.Fatal("components: missing")
	}
	schemes, _ := components["securitySchemes"].(map[string]any)
	if schemes == nil {
		t.Fatal("components.securitySchemes: missing")
	}
	for _, name := range []string{"bearerAuth", "apiKey"} {
		if _, ok := schemes[name]; !ok {
			t.Errorf("components.securitySchemes.%s: missing", name)
		}
	}
}

// TestOpenAPISpec_DocumentsSagaReadEndpoints (SELF-412) pins the US-044
// saga read paths used by the Saga Jobs UI and SDK consumers. These routes
// are already registered on the chi router; this test prevents them from
// drifting out of the OpenAPI contract again.
func TestOpenAPISpec_DocumentsSagaReadEndpoints(t *testing.T) {
	doc := loadCanonicalSpec(t)
	paths := mustOpenAPIMap(t, doc, "paths")

	for _, tc := range []struct {
		path        string
		operationID string
		responseRef string
	}{
		{
			path:        "/api/v2/ontologies/{ontologyApiName}/actions/sagas",
			operationID: "listActionSagas",
			responseRef: "#/components/schemas/ListSagasResponse",
		},
		{
			path:        "/api/v2/ontologies/{ontologyApiName}/actions/sagas/{sagaId}",
			operationID: "getActionSaga",
			responseRef: "#/components/schemas/GetSagaResponse",
		},
	} {
		t.Run(tc.operationID, func(t *testing.T) {
			pathItem := mustOpenAPIMap(t, paths, tc.path)
			getOp := mustOpenAPIMap(t, pathItem, "get")
			if got, _ := getOp["operationId"].(string); got != tc.operationID {
				t.Fatalf("%s operationId = %q, want %q", tc.path, got, tc.operationID)
			}

			responses := mustOpenAPIMap(t, getOp, "responses")
			okResponse := mustOpenAPIMap(t, responses, "200")
			content := mustOpenAPIMap(t, okResponse, "content")
			jsonContent := mustOpenAPIMap(t, content, "application/json")
			schema := mustOpenAPIMap(t, jsonContent, "schema")
			if got, _ := schema["$ref"].(string); got != tc.responseRef {
				t.Fatalf("%s 200 response schema = %q, want %q", tc.path, got, tc.responseRef)
			}
		})
	}

	schemas := mustOpenAPIMap(t, mustOpenAPIMap(t, doc, "components"), "schemas")
	saga := mustOpenAPIMap(t, schemas, "Saga")
	sagaProps := mustOpenAPIMap(t, saga, "properties")
	sagaStatus := mustOpenAPIMap(t, sagaProps, "status")
	mustOpenAPIEnum(t, sagaStatus, []string{"RUNNING", "SUCCESS", "COMPENSATING", "COMPENSATED", "FAILED"})

	sagaStep := mustOpenAPIMap(t, schemas, "SagaStep")
	stepProps := mustOpenAPIMap(t, sagaStep, "properties")
	for _, field := range []string{"stepId", "sagaId", "stepIndex", "actionType", "status"} {
		if _, ok := stepProps[field]; !ok {
			t.Fatalf("SagaStep.properties.%s: missing", field)
		}
	}
	stepStatus := mustOpenAPIMap(t, stepProps, "status")
	mustOpenAPIEnum(t, stepStatus, []string{"PENDING", "APPLIED", "FAILED", "COMPENSATED", "COMPENSATION_FAILED"})

	listResponse := mustOpenAPIMap(t, schemas, "ListSagasResponse")
	listProps := mustOpenAPIMap(t, listResponse, "properties")
	data := mustOpenAPIMap(t, listProps, "data")
	dataItems := mustOpenAPIMap(t, data, "items")
	if got, _ := dataItems["$ref"].(string); got != "#/components/schemas/Saga" {
		t.Fatalf("ListSagasResponse.data.items = %q, want Saga ref", got)
	}

	getResponse := mustOpenAPIMap(t, schemas, "GetSagaResponse")
	getProps := mustOpenAPIMap(t, getResponse, "properties")
	sagaRef := mustOpenAPIMap(t, getProps, "saga")
	if got, _ := sagaRef["$ref"].(string); got != "#/components/schemas/Saga" {
		t.Fatalf("GetSagaResponse.saga = %q, want Saga ref", got)
	}
	steps := mustOpenAPIMap(t, getProps, "steps")
	stepItems := mustOpenAPIMap(t, steps, "items")
	if got, _ := stepItems["$ref"].(string); got != "#/components/schemas/SagaStep" {
		t.Fatalf("GetSagaResponse.steps.items = %q, want SagaStep ref", got)
	}
}

func mustOpenAPIMap(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	raw, ok := parent[key]
	if !ok {
		t.Fatalf("%s: missing", key)
	}
	out, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("%s: expected map, got %T", key, raw)
	}
	return out
}

func mustOpenAPIEnum(t *testing.T, schema map[string]any, want []string) {
	t.Helper()
	raw, ok := schema["enum"]
	if !ok {
		t.Fatalf("enum: missing")
	}
	gotRaw, ok := raw.([]any)
	if !ok {
		t.Fatalf("enum: expected []any, got %T", raw)
	}
	got := map[string]bool{}
	for _, item := range gotRaw {
		value, ok := item.(string)
		if !ok {
			t.Fatalf("enum item: expected string, got %T", item)
		}
		got[value] = true
	}
	for _, value := range want {
		if !got[value] {
			t.Fatalf("enum missing %q in %v", value, gotRaw)
		}
	}
}

// TestSwaggerUI_Served verifies that GET /swagger/ returns an HTML page that
// references the embedded spec URL.
func TestSwaggerUI_Served(t *testing.T) {
	router := newContractTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/swagger/", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "html") {
		t.Errorf("Content-Type: got %q, expected to contain 'html'", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "swagger-ui") {
		t.Errorf("body should reference swagger-ui assets")
	}
	if !strings.Contains(body, "/api/openapi.yaml") {
		t.Errorf("body should load /api/openapi.yaml as the spec source")
	}
}

// TestSwaggerUI_RedirectsBareSwagger verifies that GET /swagger redirects to
// /swagger/ so users do not see a 404 if they drop the trailing slash.
func TestSwaggerUI_RedirectsBareSwagger(t *testing.T) {
	router := newContractTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/swagger", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status: got %d want 301", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/swagger/" {
		t.Errorf("Location: got %q want /swagger/", loc)
	}
}

// TestSwaggerUI_EnablesTryItOut (US-422) asserts that the bundled Swagger UI
// page wires tryItOutEnabled + persistAuthorization explicitly. These
// settings are the contract that "/swagger/ is interactive": tryItOutEnabled
// surfaces the Try-it-out button on every operation, persistAuthorization
// keeps the bearer token across reloads. They are guarded as defaults in
// swagger-ui-dist, but defaults can flip across major versions, so the page
// states them inline and this test pins them.
func TestSwaggerUI_EnablesTryItOut(t *testing.T) {
	router := newContractTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/swagger/", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"tryItOutEnabled: true",
		"persistAuthorization: true",
		"displayRequestDuration: true",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("swagger UI HTML missing %q — interactive mode is part of the US-422 contract", want)
		}
	}
}
