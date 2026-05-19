package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/actions"
	"github.com/liyang/weave/pkg/attachment"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/cipher"
	"github.com/liyang/weave/pkg/developer"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/geotemporal"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/links"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/oss/aggregation"
	"github.com/liyang/weave/pkg/oss/objectset"
	"github.com/liyang/weave/pkg/subscriptions"
	"github.com/liyang/weave/pkg/timeseries"
	"github.com/liyang/weave/pkg/transactions"
	"gopkg.in/yaml.v3"
)

// canonicalSpecPath returns the on-disk path to the canonical OpenAPI spec
// file used by humans, the build, and the contract tests. The path is
// computed relative to this test file so the test runs correctly regardless
// of the caller's working directory.
func canonicalSpecPath(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// cmd/server -> repo root -> api/openapi.yaml
	return filepath.Join(wd, "..", "..", "api", "openapi.yaml")
}

// loadCanonicalSpec parses the on-disk YAML spec into a generic map. We use
// a generic map (rather than kin-openapi or similar) so the test has zero
// runtime dependencies beyond gopkg.in/yaml.v3.
func loadCanonicalSpec(t *testing.T) map[string]any {
	t.Helper()
	path := canonicalSpecPath(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return doc
}

// specOperationKey is "METHOD path" for direct comparison with chi routes.
type specOperationKey struct {
	Method string
	Path   string
}

// extractSpecOperations walks the parsed YAML and returns one specOperationKey
// per (method, path) operation declared under `paths`.
func extractSpecOperations(t *testing.T, doc map[string]any) map[specOperationKey]bool {
	t.Helper()
	out := map[specOperationKey]bool{}
	pathsRaw, ok := doc["paths"]
	if !ok {
		return out
	}
	paths, ok := pathsRaw.(map[string]any)
	if !ok {
		t.Fatalf("paths: expected map, got %T", pathsRaw)
	}
	verbs := map[string]string{
		"get":     "GET",
		"post":    "POST",
		"put":     "PUT",
		"delete":  "DELETE",
		"patch":   "PATCH",
		"head":    "HEAD",
		"options": "OPTIONS",
	}
	for path, item := range paths {
		op, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for verb, methodName := range verbs {
			if _, hasVerb := op[verb]; hasVerb {
				out[specOperationKey{Method: methodName, Path: path}] = true
			}
		}
	}
	return out
}

// extractChiRoutes walks the chi router tree and returns the set of
// (method, path) pairs registered on it. chi templates use the same
// {param} syntax as OpenAPI, so paths can be compared directly.
func extractChiRoutes(t *testing.T, r *chi.Mux) map[specOperationKey]bool {
	t.Helper()
	out := map[specOperationKey]bool{}
	walkErr := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		// Drop the trailing "/*" chi appends to wildcard routes; we never
		// document those because they are catch-alls for the SPA fallback.
		clean := strings.TrimSuffix(route, "/*")
		out[specOperationKey{Method: method, Path: clean}] = true
		return nil
	})
	if walkErr != nil {
		t.Fatalf("chi.Walk: %v", walkErr)
	}
	return out
}

// undocumentedRouteAllowList is the set of (method, path) pairs registered
// on the router that we intentionally do NOT document in the OpenAPI spec
// because they are infrastructure (the spec, the Swagger UI, redirect
// shims) or static-asset catch-alls.
var undocumentedRouteAllowList = map[specOperationKey]bool{
	{Method: "GET", Path: "/api/openapi.yaml"}: true,
	{Method: "GET", Path: "/swagger/"}:         true,
	{Method: "GET", Path: "/swagger"}:          true,
}

// orphanSpecPathAllowList is the set of (method, path) pairs declared in the
// OpenAPI spec that have no chi route in this server. This list is empty by
// design: every documented path SHOULD map to a registered route.
var orphanSpecPathAllowList = map[specOperationKey]bool{}

func TestBDD_VertexControlPanelOpenAPIContract(t *testing.T) {
	doc := loadCanonicalSpec(t)
	specOps := extractSpecOperations(t, doc)
	controlPanelOps := []specOperationKey{
		{Method: "GET", Path: "/api/vertex/v1/admin/control-panel"},
		{Method: "PUT", Path: "/api/vertex/v1/admin/control-panel"},
	}
	for _, op := range controlPanelOps {
		if undocumentedRouteAllowList[op] {
			t.Errorf("%s %s must be documented in OpenAPI, not allow-listed as undocumented", op.Method, op.Path)
		}
		if !specOps[op] {
			t.Errorf("api/openapi.yaml must document %s %s", op.Method, op.Path)
		}
	}

	schemas := openAPISchemas(t, doc)
	config := openAPIProperties(t, schemas, "VertexControlPanelConfig")
	update := openAPIProperties(t, schemas, "VertexControlPanelUpdateRequest")
	expectedDefaults := map[string]string{
		"defaultWindowDays":       "30",
		"pollingIntervalSec":      "5",
		"searchAroundMaxNodes":    "200",
		"searchAroundMaxDepth":    "3",
		"missingDataWarningHours": "24",
	}
	for field, wantDefault := range expectedDefaults {
		prop, ok := config[field].(map[string]any)
		if !ok {
			t.Errorf("VertexControlPanelConfig must expose %s", field)
			continue
		}
		if got := fmt.Sprint(prop["default"]); got != wantDefault {
			t.Errorf("VertexControlPanelConfig.%s default = %s, want %s", field, got, wantDefault)
		}
		if _, ok := update[field]; !ok {
			t.Errorf("VertexControlPanelUpdateRequest must expose sparse update field %s", field)
		}
	}
}

func TestBDD_VertexShareLinksAndWidgetOpenAPIContract(t *testing.T) {
	doc := loadCanonicalSpec(t)
	specOps := extractSpecOperations(t, doc)
	expectedOps := []struct {
		op          specOperationKey
		operationID string
	}{
		{
			op:          specOperationKey{Method: "POST", Path: "/api/vertex/v1/graphs/{rid}/share-links"},
			operationID: "createVertexShareLink",
		},
		{
			op:          specOperationKey{Method: "DELETE", Path: "/api/vertex/v1/share-links/{token}"},
			operationID: "revokeVertexShareLink",
		},
		{
			op:          specOperationKey{Method: "GET", Path: "/api/vertex/v1/share-links/{token}/graph"},
			operationID: "getVertexGraphViaShareLink",
		},
		{
			op:          specOperationKey{Method: "GET", Path: "/api/vertex/v1/graphs/{rid}/widget"},
			operationID: "getVertexGraphWidget",
		},
		{
			op:          specOperationKey{Method: "POST", Path: "/api/vertex/v1/graphs/{rid}/widget/save"},
			operationID: "saveVertexGraphWidget",
		},
	}

	allPresent := true
	for _, want := range expectedOps {
		if undocumentedRouteAllowList[want.op] {
			t.Errorf("%s %s must be documented in OpenAPI, not allow-listed as undocumented", want.op.Method, want.op.Path)
		}
		if !specOps[want.op] {
			t.Errorf("api/openapi.yaml must document %s %s", want.op.Method, want.op.Path)
			allPresent = false
		}
	}
	if !allPresent {
		return
	}

	for _, want := range expectedOps {
		operation := openAPIPathOperation(t, doc, want.op.Path, want.op.Method)
		if got, _ := operation["operationId"].(string); got != want.operationID {
			t.Errorf("%s %s operationId = %q, want %q", want.op.Method, want.op.Path, got, want.operationID)
		}
	}

	shareCreate := openAPIPathOperation(t, doc, "/api/vertex/v1/graphs/{rid}/share-links", "POST")
	if got := openAPIJSONResponseSchemaRef(t, shareCreate, "201"); got != "#/components/schemas/VertexShareLinkCreateResponse" {
		t.Errorf("share-link create 201 schema = %q, want VertexShareLinkCreateResponse", got)
	}
	revoke := openAPIPathOperation(t, doc, "/api/vertex/v1/share-links/{token}", "DELETE")
	if !openAPIResponseStatusExists(t, revoke, "204") {
		t.Errorf("share-link revoke must document 204 No Content")
	}
	shareGraph := openAPIPathOperation(t, doc, "/api/vertex/v1/share-links/{token}/graph", "GET")
	if got := openAPIJSONResponseSchemaRef(t, shareGraph, "200"); got != "#/components/schemas/VertexGraph" {
		t.Errorf("share-link graph 200 schema = %q, want VertexGraph", got)
	}
	widgetGet := openAPIPathOperation(t, doc, "/api/vertex/v1/graphs/{rid}/widget", "GET")
	if got := openAPIJSONResponseSchemaRef(t, widgetGet, "200"); got != "#/components/schemas/VertexGraph" {
		t.Errorf("widget GET 200 schema = %q, want VertexGraph", got)
	}
	widgetSave := openAPIPathOperation(t, doc, "/api/vertex/v1/graphs/{rid}/widget/save", "POST")
	if got := openAPIRequestBodySchemaRef(t, widgetSave); got != "#/components/schemas/VertexWidgetSaveRequest" {
		t.Errorf("widget save request schema = %q, want VertexWidgetSaveRequest", got)
	}
	if got := openAPIJSONResponseSchemaRef(t, widgetSave, "200"); got != "#/components/schemas/VertexGraph" {
		t.Errorf("widget save 200 schema = %q, want VertexGraph", got)
	}

	schemas := openAPISchemas(t, doc)
	graph := openAPIProperties(t, schemas, "VertexGraph")
	for _, field := range []string{"rid", "ontologyRid", "name", "version", "versioned", "payload", "createdBy", "createdAt", "updatedAt"} {
		if _, ok := graph[field]; !ok {
			t.Errorf("VertexGraph must expose %s", field)
		}
	}
	shareResponse := openAPIProperties(t, schemas, "VertexShareLinkCreateResponse")
	for _, field := range []string{"token", "graphRid", "createdBy", "createdAt"} {
		if _, ok := shareResponse[field]; !ok {
			t.Errorf("VertexShareLinkCreateResponse must expose %s", field)
		}
	}
	widgetRequest := openAPIProperties(t, schemas, "VertexWidgetSaveRequest")
	for _, field := range []string{"payload", "overrideGraphRid"} {
		if _, ok := widgetRequest[field]; !ok {
			t.Errorf("VertexWidgetSaveRequest must expose %s", field)
		}
	}
}

func TestBDD_VertexGraphCRUDOpenAPIContract(t *testing.T) {
	doc := loadCanonicalSpec(t)
	specOps := extractSpecOperations(t, doc)
	expectedOps := []struct {
		op          specOperationKey
		operationID string
		status      string
	}{
		{
			op:          specOperationKey{Method: "POST", Path: "/api/vertex/v1/graphs"},
			operationID: "createVertexGraph",
			status:      "201",
		},
		{
			op:          specOperationKey{Method: "GET", Path: "/api/vertex/v1/graphs/{rid}"},
			operationID: "getVertexGraph",
			status:      "200",
		},
		{
			op:          specOperationKey{Method: "PUT", Path: "/api/vertex/v1/graphs/{rid}"},
			operationID: "updateVertexGraph",
			status:      "200",
		},
	}

	allPresent := true
	for _, want := range expectedOps {
		if undocumentedRouteAllowList[want.op] {
			t.Errorf("%s %s must be documented in OpenAPI, not allow-listed as undocumented", want.op.Method, want.op.Path)
		}
		if !specOps[want.op] {
			t.Errorf("api/openapi.yaml must document %s %s", want.op.Method, want.op.Path)
			allPresent = false
		}
	}
	if !allPresent {
		return
	}

	for _, want := range expectedOps {
		operation := openAPIPathOperation(t, doc, want.op.Path, want.op.Method)
		if got, _ := operation["operationId"].(string); got != want.operationID {
			t.Errorf("%s %s operationId = %q, want %q", want.op.Method, want.op.Path, got, want.operationID)
		}
		if got := openAPIJSONResponseSchemaRef(t, operation, want.status); got != "#/components/schemas/VertexGraph" {
			t.Errorf("%s %s %s schema = %q, want VertexGraph", want.op.Method, want.op.Path, want.status, got)
		}
	}

	create := openAPIPathOperation(t, doc, "/api/vertex/v1/graphs", "POST")
	if got := openAPIRequestBodySchemaRef(t, create); got != "#/components/schemas/VertexGraphCreateRequest" {
		t.Errorf("graph create request schema = %q, want VertexGraphCreateRequest", got)
	}
	update := openAPIPathOperation(t, doc, "/api/vertex/v1/graphs/{rid}", "PUT")
	if got := openAPIRequestBodySchemaRef(t, update); got != "#/components/schemas/VertexGraphUpdateRequest" {
		t.Errorf("graph update request schema = %q, want VertexGraphUpdateRequest", got)
	}

	schemas := openAPISchemas(t, doc)
	createProps := openAPIProperties(t, schemas, "VertexGraphCreateRequest")
	for _, field := range []string{"ontologyRid", "name", "versioned", "payload", "createdBy"} {
		if _, ok := createProps[field]; !ok {
			t.Errorf("VertexGraphCreateRequest must expose %s", field)
		}
	}
	createRequired := openAPIRequiredSet(t, schemas, "VertexGraphCreateRequest")
	for _, field := range []string{"ontologyRid", "name"} {
		if !createRequired[field] {
			t.Errorf("VertexGraphCreateRequest must require %s", field)
		}
	}

	updateProps := openAPIProperties(t, schemas, "VertexGraphUpdateRequest")
	if _, ok := updateProps["payload"]; !ok {
		t.Errorf("VertexGraphUpdateRequest must expose payload")
	}
	if !openAPIRequiredSet(t, schemas, "VertexGraphUpdateRequest")["payload"] {
		t.Errorf("VertexGraphUpdateRequest must require payload")
	}
}

func TestBDD_VertexGraphHistoryTemplateOpenAPIContract(t *testing.T) {
	doc := loadCanonicalSpec(t)
	specOps := extractSpecOperations(t, doc)
	expectedOps := []struct {
		op          specOperationKey
		operationID string
		status      string
		responseRef string
	}{
		{
			op:          specOperationKey{Method: "POST", Path: "/api/vertex/v1/graphs/{rid}/duplicate"},
			operationID: "duplicateVertexGraph",
			status:      "201",
			responseRef: "#/components/schemas/VertexGraph",
		},
		{
			op:          specOperationKey{Method: "POST", Path: "/api/vertex/v1/graphs/{rid}/save-as-template"},
			operationID: "saveVertexGraphAsTemplate",
			status:      "201",
			responseRef: "#/components/schemas/VertexGraphTemplateSaveResponse",
		},
		{
			op:          specOperationKey{Method: "GET", Path: "/api/vertex/v1/graphs/{rid}/history"},
			operationID: "listVertexGraphHistory",
			status:      "200",
			responseRef: "#/components/schemas/VertexGraphHistoryResponse",
		},
		{
			op:          specOperationKey{Method: "GET", Path: "/api/vertex/v1/graphs/{rid}/versions/{version}"},
			operationID: "getVertexGraphVersion",
			status:      "200",
			responseRef: "#/components/schemas/VertexGraph",
		},
		{
			op:          specOperationKey{Method: "POST", Path: "/api/vertex/v1/templates/{rid}/instantiate"},
			operationID: "instantiateVertexGraphTemplate",
			status:      "200",
			responseRef: "#/components/schemas/VertexGraphTemplateInstantiateResponse",
		},
	}

	allPresent := true
	for _, want := range expectedOps {
		if undocumentedRouteAllowList[want.op] {
			t.Errorf("%s %s must be documented in OpenAPI, not allow-listed as undocumented", want.op.Method, want.op.Path)
		}
		if !specOps[want.op] {
			t.Errorf("api/openapi.yaml must document %s %s", want.op.Method, want.op.Path)
			allPresent = false
		}
	}
	if !allPresent {
		return
	}

	for _, want := range expectedOps {
		operation := openAPIPathOperation(t, doc, want.op.Path, want.op.Method)
		if got, _ := operation["operationId"].(string); got != want.operationID {
			t.Errorf("%s %s operationId = %q, want %q", want.op.Method, want.op.Path, got, want.operationID)
		}
		if got := openAPIJSONResponseSchemaRef(t, operation, want.status); got != want.responseRef {
			t.Errorf("%s %s %s schema = %q, want %s", want.op.Method, want.op.Path, want.status, got, want.responseRef)
		}
	}

	saveTemplate := openAPIPathOperation(t, doc, "/api/vertex/v1/graphs/{rid}/save-as-template", "POST")
	if got := openAPIRequestBodySchemaRef(t, saveTemplate); got != "#/components/schemas/VertexGraphTemplateSaveRequest" {
		t.Errorf("graph save-as-template request schema = %q, want VertexGraphTemplateSaveRequest", got)
	}
	instantiate := openAPIPathOperation(t, doc, "/api/vertex/v1/templates/{rid}/instantiate", "POST")
	if got := openAPIRequestBodySchemaRef(t, instantiate); got != "#/components/schemas/VertexGraphTemplateInstantiateRequest" {
		t.Errorf("template instantiate request schema = %q, want VertexGraphTemplateInstantiateRequest", got)
	}

	schemas := openAPISchemas(t, doc)
	saveReqProps := openAPIProperties(t, schemas, "VertexGraphTemplateSaveRequest")
	for _, field := range []string{"name", "parameterizedFields"} {
		if _, ok := saveReqProps[field]; !ok {
			t.Errorf("VertexGraphTemplateSaveRequest must expose %s", field)
		}
	}
	saveRespProps := openAPIProperties(t, schemas, "VertexGraphTemplateSaveResponse")
	for _, field := range []string{"rid", "sourceGraphRid", "name", "parameterizedFields"} {
		if _, ok := saveRespProps[field]; !ok {
			t.Errorf("VertexGraphTemplateSaveResponse must expose %s", field)
		}
	}
	historyRespProps := openAPIProperties(t, schemas, "VertexGraphHistoryResponse")
	for _, field := range []string{"rid", "versions"} {
		if _, ok := historyRespProps[field]; !ok {
			t.Errorf("VertexGraphHistoryResponse must expose %s", field)
		}
	}
	historyEntryProps := openAPIProperties(t, schemas, "VertexGraphHistoryEntry")
	for _, field := range []string{"version", "createdAt"} {
		if _, ok := historyEntryProps[field]; !ok {
			t.Errorf("VertexGraphHistoryEntry must expose %s", field)
		}
	}
	instantiateReqProps := openAPIProperties(t, schemas, "VertexGraphTemplateInstantiateRequest")
	if _, ok := instantiateReqProps["parameters"]; !ok {
		t.Errorf("VertexGraphTemplateInstantiateRequest must expose parameters")
	}
	instantiateRespProps := openAPIProperties(t, schemas, "VertexGraphTemplateInstantiateResponse")
	for _, field := range []string{"sourceTemplateRid", "sourceGraphRid", "name", "payload"} {
		if _, ok := instantiateRespProps[field]; !ok {
			t.Errorf("VertexGraphTemplateInstantiateResponse must expose %s", field)
		}
	}
}

func TestBDD_VertexGraphLayoutDiffOpenAPIContract(t *testing.T) {
	doc := loadCanonicalSpec(t)
	specOps := extractSpecOperations(t, doc)
	expectedOps := []struct {
		op          specOperationKey
		operationID string
		status      string
		responseRef string
	}{
		{
			op:          specOperationKey{Method: "PATCH", Path: "/api/vertex/v1/graphs/{rid}/layout"},
			operationID: "patchVertexGraphLayout",
			status:      "200",
			responseRef: "#/components/schemas/VertexGraphLayoutPatchResponse",
		},
		{
			op:          specOperationKey{Method: "GET", Path: "/api/vertex/v1/graphs/{rid}/diff"},
			operationID: "diffVertexGraphVersions",
			status:      "200",
			responseRef: "#/components/schemas/VertexGraphDiffResponse",
		},
	}

	allPresent := true
	for _, want := range expectedOps {
		if undocumentedRouteAllowList[want.op] {
			t.Errorf("%s %s must be documented in OpenAPI, not allow-listed as undocumented", want.op.Method, want.op.Path)
		}
		if !specOps[want.op] {
			t.Errorf("api/openapi.yaml must document %s %s", want.op.Method, want.op.Path)
			allPresent = false
		}
	}
	if !allPresent {
		return
	}

	for _, want := range expectedOps {
		operation := openAPIPathOperation(t, doc, want.op.Path, want.op.Method)
		if got, _ := operation["operationId"].(string); got != want.operationID {
			t.Errorf("%s %s operationId = %q, want %q", want.op.Method, want.op.Path, got, want.operationID)
		}
		if got := openAPIJSONResponseSchemaRef(t, operation, want.status); got != want.responseRef {
			t.Errorf("%s %s %s schema = %q, want %s", want.op.Method, want.op.Path, want.status, got, want.responseRef)
		}
	}

	layout := openAPIPathOperation(t, doc, "/api/vertex/v1/graphs/{rid}/layout", "PATCH")
	if got := openAPIRequestBodySchemaRef(t, layout); got != "#/components/schemas/VertexGraphLayoutPatchRequest" {
		t.Errorf("graph layout patch request schema = %q, want VertexGraphLayoutPatchRequest", got)
	}
	diff := openAPIPathOperation(t, doc, "/api/vertex/v1/graphs/{rid}/diff", "GET")
	for _, name := range []string{"from", "to"} {
		param := openAPIParameter(t, diff, "query", name)
		if required, _ := param["required"].(bool); !required {
			t.Errorf("diff query parameter %s must be required", name)
		}
		schema, ok := param["schema"].(map[string]any)
		if !ok {
			t.Fatalf("diff query parameter %s schema: expected map, got %T", name, param["schema"])
		}
		if got, _ := schema["type"].(string); got != "integer" {
			t.Errorf("diff query parameter %s type = %q, want integer", name, got)
		}
		if got := fmt.Sprint(schema["minimum"]); got != "1" {
			t.Errorf("diff query parameter %s minimum = %s, want 1", name, got)
		}
	}

	schemas := openAPISchemas(t, doc)
	layoutReqProps := openAPIProperties(t, schemas, "VertexGraphLayoutPatchRequest")
	if _, ok := layoutReqProps["positions"]; !ok {
		t.Errorf("VertexGraphLayoutPatchRequest must expose positions")
	}
	if !openAPIRequiredSet(t, schemas, "VertexGraphLayoutPatchRequest")["positions"] {
		t.Errorf("VertexGraphLayoutPatchRequest must require positions")
	}
	layoutRespProps := openAPIProperties(t, schemas, "VertexGraphLayoutPatchResponse")
	if _, ok := layoutRespProps["rid"]; !ok {
		t.Errorf("VertexGraphLayoutPatchResponse must expose rid")
	}
	diffRespProps := openAPIProperties(t, schemas, "VertexGraphDiffResponse")
	for _, field := range []string{"rid", "from", "to", "ops"} {
		if _, ok := diffRespProps[field]; !ok {
			t.Errorf("VertexGraphDiffResponse must expose %s", field)
		}
	}
	diffOpProps := openAPIProperties(t, schemas, "VertexGraphDiffOp")
	for _, field := range []string{"op", "path", "value"} {
		if _, ok := diffOpProps[field]; !ok {
			t.Errorf("VertexGraphDiffOp must expose %s", field)
		}
	}
	diffOpRequired := openAPIRequiredSet(t, schemas, "VertexGraphDiffOp")
	for _, field := range []string{"op", "path"} {
		if !diffOpRequired[field] {
			t.Errorf("VertexGraphDiffOp must require %s", field)
		}
	}
}

func openAPISchemas(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	components, ok := doc["components"].(map[string]any)
	if !ok {
		t.Fatalf("components: expected map, got %T", doc["components"])
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		t.Fatalf("components.schemas: expected map, got %T", components["schemas"])
	}
	return schemas
}

func openAPIProperties(t *testing.T, schemas map[string]any, name string) map[string]any {
	t.Helper()
	schema, ok := schemas[name].(map[string]any)
	if !ok {
		t.Fatalf("components.schemas.%s: expected map, got %T", name, schemas[name])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("components.schemas.%s.properties: expected map, got %T", name, schema["properties"])
	}
	return props
}

func openAPIRequiredSet(t *testing.T, schemas map[string]any, name string) map[string]bool {
	t.Helper()
	schema, ok := schemas[name].(map[string]any)
	if !ok {
		t.Fatalf("components.schemas.%s: expected map, got %T", name, schemas[name])
	}
	requiredRaw, ok := schema["required"].([]any)
	if !ok {
		t.Fatalf("components.schemas.%s.required: expected list, got %T", name, schema["required"])
	}
	out := map[string]bool{}
	for _, item := range requiredRaw {
		field, ok := item.(string)
		if !ok {
			t.Fatalf("components.schemas.%s.required item: expected string, got %T", name, item)
		}
		out[field] = true
	}
	return out
}

func openAPIPathOperation(t *testing.T, doc map[string]any, path string, method string) map[string]any {
	t.Helper()
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatalf("paths: expected map, got %T", doc["paths"])
	}
	pathItem, ok := paths[path].(map[string]any)
	if !ok {
		t.Fatalf("paths.%s: expected map, got %T", path, paths[path])
	}
	operation, ok := pathItem[strings.ToLower(method)].(map[string]any)
	if !ok {
		t.Fatalf("paths.%s.%s: expected map, got %T", path, strings.ToLower(method), pathItem[strings.ToLower(method)])
	}
	return operation
}

func openAPIParameter(t *testing.T, operation map[string]any, in string, name string) map[string]any {
	t.Helper()
	parameters, ok := operation["parameters"].([]any)
	if !ok {
		t.Fatalf("operation.parameters: expected list, got %T", operation["parameters"])
	}
	for _, raw := range parameters {
		param, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("operation.parameters item: expected map, got %T", raw)
		}
		gotIn, _ := param["in"].(string)
		gotName, _ := param["name"].(string)
		if gotIn == in && gotName == name {
			return param
		}
	}
	t.Fatalf("operation.parameters missing %s parameter %q", in, name)
	return nil
}

func openAPIResponseStatusExists(t *testing.T, operation map[string]any, status string) bool {
	t.Helper()
	responses, ok := operation["responses"].(map[string]any)
	if !ok {
		t.Fatalf("operation.responses: expected map, got %T", operation["responses"])
	}
	_, ok = responses[status]
	return ok
}

func openAPIJSONResponseSchemaRef(t *testing.T, operation map[string]any, status string) string {
	t.Helper()
	responses, ok := operation["responses"].(map[string]any)
	if !ok {
		t.Fatalf("operation.responses: expected map, got %T", operation["responses"])
	}
	response, ok := responses[status].(map[string]any)
	if !ok {
		t.Fatalf("operation.responses.%s: expected map, got %T", status, responses[status])
	}
	content, ok := response["content"].(map[string]any)
	if !ok {
		t.Fatalf("operation.responses.%s.content: expected map, got %T", status, response["content"])
	}
	jsonContent, ok := content["application/json"].(map[string]any)
	if !ok {
		t.Fatalf("operation.responses.%s.content.application/json: expected map, got %T", status, content["application/json"])
	}
	schema, ok := jsonContent["schema"].(map[string]any)
	if !ok {
		t.Fatalf("operation.responses.%s.content.application/json.schema: expected map, got %T", status, jsonContent["schema"])
	}
	ref, _ := schema["$ref"].(string)
	return ref
}

func openAPIRequestBodySchemaRef(t *testing.T, operation map[string]any) string {
	t.Helper()
	requestBody, ok := operation["requestBody"].(map[string]any)
	if !ok {
		t.Fatalf("operation.requestBody: expected map, got %T", operation["requestBody"])
	}
	content, ok := requestBody["content"].(map[string]any)
	if !ok {
		t.Fatalf("operation.requestBody.content: expected map, got %T", requestBody["content"])
	}
	jsonContent, ok := content["application/json"].(map[string]any)
	if !ok {
		t.Fatalf("operation.requestBody.content.application/json: expected map, got %T", content["application/json"])
	}
	schema, ok := jsonContent["schema"].(map[string]any)
	if !ok {
		t.Fatalf("operation.requestBody.content.application/json.schema: expected map, got %T", jsonContent["schema"])
	}
	ref, _ := schema["$ref"].(string)
	return ref
}

func newContractTestRouter(t *testing.T) *chi.Mux {
	t.Helper()
	repo := newFakeUserRepo()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err := auth.NewJWTSigner(priv, &priv.PublicKey, auth.JWTSignerOptions{
		Issuer:         "weave-test",
		Audience:       "weave-api",
		AccessTokenTTL: 15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	rs := auth.NewRefreshService(auth.NewMemoryRefreshStore(),
		auth.RefreshServiceOptions{AbsoluteTTL: 7 * 24 * time.Hour})

	// Wire OSS / Action / Aggregation / ObjectSet deps with stubs so the
	// full router registers every route. The handlers are never invoked by
	// chi.Walk, so panics on nil-call would never fire from contract tests.
	omsRepo := contractOmsRepo{}
	indexMgr := index.NewManager(t.TempDir())
	t.Cleanup(func() { _ = indexMgr.Close() })
	linkResolver := links.NewResolver(omsRepo, indexMgr)
	ossSvc := oss.NewService(omsRepo, indexMgr, linkResolver)
	aggEngine := aggregation.NewEngine()
	objSetStore := objectset.NewStore(time.Hour)
	objSetExecutor := objectset.NewExecutor(indexMgr, linkResolver, objSetStore)
	actionExecutor := actions.NewExecutor(omsRepo, nil)

	deps := &ServerDeps{
		OmsRepo:          omsRepo,
		UserRepo:         repo,
		RoleResolver:     auth.NewRoleResolver(repo, time.Minute),
		JWTSigner:        signer,
		RefreshService:   rs,
		IndexMgr:         indexMgr,
		LinkResolver:     linkResolver,
		OssSvc:           ossSvc,
		AggEngine:        aggEngine,
		ActionExecutor:   actionExecutor,
		ObjSetStore:      objSetStore,
		ObjSetExecutor:   objSetExecutor,
		AttachmentStore:  attachment.NewLocalStore(t.TempDir()),
		TimeSeriesStore:  timeseries.NewMemoryStore(),
		GeotemporalStore: geotemporal.NewMemoryStore(),
		CipherDecryptor:  mustContractCipherDecryptor(t),
		TransactionStore: transactions.NewMemoryStore(),
		FunnelBroadcast:  funnel.NewBroadcast(),
		FunnelPublisher:  stubIngestPublisher{},
		WebSocketHub:     subscriptions.NewHub(),
		ApplicationRepo:  stubApplicationRepo{},
		AuthCodeRepo:     stubAuthCodeRepo{},
		OAuthTokenRepo:   stubOAuthTokenRepo{},
	}
	// US-446: contract / pact tests assume /health(z)/ready returns 200 in the
	// degraded contract harness; MarkReady flips the lifecycle gate so the
	// readiness handler runs the dependency probes (which all skip in
	// degraded mode) instead of short-circuiting on starting.
	deps.ServerState.MarkReady()
	return NewFullRouter(deps)
}

func mustContractCipherDecryptor(t *testing.T) cipher.Decryptor {
	t.Helper()
	dec, err := cipher.NewAESGCMDecryptor("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("cipher.NewAESGCMDecryptor: %v", err)
	}
	return dec
}

// TestContract_AllRoutesDocumented verifies that every chi route registered
// on NewFullRouter is documented in api/openapi.yaml. This is the contract
// test that prevents spec drift: a developer who adds a new route MUST also
// add the corresponding entry to the spec, or this test fails.
func TestContract_AllRoutesDocumented(t *testing.T) {
	router := newContractTestRouter(t)
	chiRoutes := extractChiRoutes(t, router)
	specOps := extractSpecOperations(t, loadCanonicalSpec(t))

	var missing []specOperationKey
	for key := range chiRoutes {
		if undocumentedRouteAllowList[key] {
			continue
		}
		if !specOps[key] {
			missing = append(missing, key)
		}
	}
	if len(missing) == 0 {
		return
	}
	sort.Slice(missing, func(i, j int) bool {
		if missing[i].Path != missing[j].Path {
			return missing[i].Path < missing[j].Path
		}
		return missing[i].Method < missing[j].Method
	})
	var b strings.Builder
	b.WriteString("openapi.yaml is missing entries for the following chi routes:\n")
	for _, k := range missing {
		fmt.Fprintf(&b, "  %s %s\n", k.Method, k.Path)
	}
	b.WriteString("\nAdd a corresponding `paths` entry in api/openapi.yaml or extend ")
	b.WriteString("undocumentedRouteAllowList in cmd/server/contract_test.go.")
	t.Fatal(b.String())
}

// TestContract_NoOrphanedSpecPaths is the reverse check: every operation in
// api/openapi.yaml MUST map to a chi route currently registered on the
// server. This catches deleted routes whose spec entries were left behind.
func TestContract_NoOrphanedSpecPaths(t *testing.T) {
	router := newContractTestRouter(t)
	chiRoutes := extractChiRoutes(t, router)
	specOps := extractSpecOperations(t, loadCanonicalSpec(t))

	var orphans []specOperationKey
	for key := range specOps {
		if orphanSpecPathAllowList[key] {
			continue
		}
		if !chiRoutes[key] {
			orphans = append(orphans, key)
		}
	}
	if len(orphans) == 0 {
		return
	}
	sort.Slice(orphans, func(i, j int) bool {
		if orphans[i].Path != orphans[j].Path {
			return orphans[i].Path < orphans[j].Path
		}
		return orphans[i].Method < orphans[j].Method
	})
	var b strings.Builder
	b.WriteString("openapi.yaml has paths with no matching chi route:\n")
	for _, k := range orphans {
		fmt.Fprintf(&b, "  %s %s\n", k.Method, k.Path)
	}
	b.WriteString("\nRemove the unused entry from api/openapi.yaml or register the route on NewFullRouter.")
	t.Fatal(b.String())
}

// TestContract_EmbeddedSpecMatchesCanonical guards against the embedded
// /api/openapi.yaml drifting from the on-disk source. The embedded copy
// (cmd/server/openapi_spec.yaml) is what the running server returns, so it
// MUST be byte-identical to the canonical api/openapi.yaml that the spec
// review and contract tests work against.
func TestContract_EmbeddedSpecMatchesCanonical(t *testing.T) {
	canonical, err := os.ReadFile(canonicalSpecPath(t))
	if err != nil {
		t.Fatalf("read canonical: %v", err)
	}
	if string(canonical) != string(openapiSpecYAML) {
		t.Fatalf("embedded openapi spec drifted from api/openapi.yaml; " +
			"copy api/openapi.yaml to cmd/server/openapi_spec.yaml and rerun")
	}
}

// contractOmsRepo is a minimal stub OMS repo so the full router wires every
// route. It satisfies oms.Repository through interface embedding (every
// method panics with nil-deref if invoked, but chi.Walk only inspects route
// metadata, never executes the handlers).
type contractOmsRepo struct{ oms.Repository }

// stubIngestPublisher satisfies oss.IngestPublisher for contract tests.
type stubIngestPublisher struct{}

func (stubIngestPublisher) Publish(batch *funnel.EditBatch) (uint64, error) { return 0, nil }

// stubApplicationRepo satisfies developer.ApplicationRepository for contract
// tests. chi.Walk only inspects route metadata, so every method is allowed
// to return a trivial error/empty value — the handlers never run here.
type stubApplicationRepo struct{}

func (stubApplicationRepo) Create(context.Context, *developer.Application) error { return nil }
func (stubApplicationRepo) GetByID(context.Context, string) (*developer.Application, error) {
	return nil, developer.ErrApplicationNotFound
}
func (stubApplicationRepo) GetByClientID(context.Context, string) (*developer.Application, error) {
	return nil, developer.ErrApplicationNotFound
}
func (stubApplicationRepo) ListByUser(context.Context, string) ([]*developer.Application, error) {
	return nil, nil
}
func (stubApplicationRepo) Delete(context.Context, string) error { return nil }

// stubAuthCodeRepo satisfies developer.AuthorizationCodeRepository for
// contract tests. The handlers never actually execute during chi.Walk so
// every method is allowed to return a trivial error/value.
type stubAuthCodeRepo struct{}

func (stubAuthCodeRepo) Create(context.Context, *developer.AuthorizationCode) error { return nil }
func (stubAuthCodeRepo) GetByCode(context.Context, string) (*developer.AuthorizationCode, error) {
	return nil, developer.ErrAuthorizationCodeNotFound
}
func (stubAuthCodeRepo) MarkConsumed(context.Context, string, time.Time) error { return nil }

// stubOAuthTokenRepo satisfies developer.OAuthTokenRepository for contract
// tests.
type stubOAuthTokenRepo struct{}

func (stubOAuthTokenRepo) Create(context.Context, *developer.OAuthToken) error { return nil }
func (stubOAuthTokenRepo) GetByPrefix(context.Context, string, string) ([]*developer.OAuthToken, error) {
	return nil, nil
}
func (stubOAuthTokenRepo) Revoke(context.Context, string, time.Time) error { return nil }
