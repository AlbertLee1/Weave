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
