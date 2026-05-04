package oms_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

func TestGenerateSDK_TypeScript(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test Ontology"},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ri.ontology.main.objectType.1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Employee", DisplayName: "Employee", PrimaryKey: "employeeId"},
		},
		properties: []oms.Property{
			{RID: "ri.ontology.main.property.1", ObjectTypeRID: "ri.ontology.main.objectType.1", APIName: "employeeId", BaseType: "string"},
			{RID: "ri.ontology.main.property.2", ObjectTypeRID: "ri.ontology.main.objectType.1", APIName: "name", BaseType: "string"},
		},
		linkTypes: []oms.LinkType{
			{RID: "ri.ontology.main.linkType.1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "manages"},
		},
		actionTypes: []oms.ActionType{
			{RID: "ri.ontology.main.actionType.1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "createEmployee", DisplayName: "Create Employee", Status: "ACTIVE",
				Parameters: json.RawMessage(`[{"id":"name","type":"string","required":true}]`)},
		},
		interfaces: []oms.Interface{
			{RID: "ri.ontology.main.interface.1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "HasName"},
		},
	}

	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/sdkgen", handler.GenerateSDK)

	req := httptest.NewRequest("POST", "/api/v2/ontologies/test/sdkgen?lang=ts", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if ct := w.Header().Get("Content-Type"); ct != "application/zip" {
		t.Errorf("expected Content-Type application/zip, got %q", ct)
	}

	// Verify it's a valid zip
	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatalf("invalid zip: %v", err)
	}

	// Check expected files
	expectedFiles := map[string]bool{
		"src/models.ts":  false,
		"src/client.ts":  false,
		"src/index.ts":   false,
		"package.json":   false,
		"tsconfig.json":  false,
	}
	for _, f := range zr.File {
		if _, ok := expectedFiles[f.Name]; ok {
			expectedFiles[f.Name] = true
		}
	}
	for name, found := range expectedFiles {
		if !found {
			t.Errorf("expected file %q in zip, not found", name)
		}
	}
}

func TestGenerateSDK_Python(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test"},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ot1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Employee"},
		},
	}

	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/sdkgen", handler.GenerateSDK)

	req := httptest.NewRequest("POST", "/api/v2/ontologies/test/sdkgen?lang=python", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatalf("invalid zip: %v", err)
	}

	found := false
	for _, f := range zr.File {
		if f.Name == "weave_sdk/models.py" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected weave_sdk/models.py in zip")
	}
}

func TestGenerateSDK_Go(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test"},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ot1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Employee"},
		},
	}

	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/sdkgen", handler.GenerateSDK)

	req := httptest.NewRequest("POST", "/api/v2/ontologies/test/sdkgen?lang=go", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatalf("invalid zip: %v", err)
	}

	found := false
	for _, f := range zr.File {
		// US-420 restructured the Go SDK output to per-ontology subpackage
		// `pkg/<ontology>/{objects,actions,functions,client}.go`. The "test"
		// ontology lowercases to package `test`.
		if f.Name == "pkg/test/objects.go" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected pkg/test/objects.go in zip")
	}
}

func TestGenerateSDK_UnsupportedLanguage(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
	}

	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/sdkgen", handler.GenerateSDK)

	req := httptest.NewRequest("POST", "/api/v2/ontologies/test/sdkgen?lang=java", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGenerateSDK_MissingLang(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
	}

	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/sdkgen", handler.GenerateSDK)

	req := httptest.NewRequest("POST", "/api/v2/ontologies/test/sdkgen", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGenerateSDK_XOntologyVersionHeader(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test", CurrentVersion: 7},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ot1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Employee"},
		},
	}

	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/sdkgen", handler.GenerateSDK)

	req := httptest.NewRequest("POST", "/api/v2/ontologies/test/sdkgen?lang=ts", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("X-Ontology-Version"); got != "7" {
		t.Errorf("expected X-Ontology-Version '7', got %q", got)
	}
}

func TestGenerateSDK_IncludesMetadataFile(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test", CurrentVersion: 5},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ot1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Employee"},
		},
	}

	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/sdkgen", handler.GenerateSDK)

	req := httptest.NewRequest("POST", "/api/v2/ontologies/test/sdkgen?lang=ts", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatalf("invalid zip: %v", err)
	}

	var metaFound, changelogFound bool
	for _, f := range zr.File {
		switch f.Name {
		case ".weave-sdk.json":
			metaFound = true
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("open metadata: %v", err)
			}
			raw, _ := io.ReadAll(rc)
			rc.Close()
			if !bytes.Contains(raw, []byte(`"ontologyVersion": 5`)) {
				t.Errorf("metadata did not include ontologyVersion 5: %s", raw)
			}
		case "CHANGELOG.md":
			changelogFound = true
		}
	}
	if !metaFound {
		t.Error("expected .weave-sdk.json in zip")
	}
	if !changelogFound {
		t.Error("expected CHANGELOG.md in zip")
	}
}

func TestGenerateSDK_OntologyNotFound(t *testing.T) {
	repo := &mockRepo{}

	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/sdkgen", handler.GenerateSDK)

	req := httptest.NewRequest("POST", "/api/v2/ontologies/nonexistent/sdkgen?lang=ts", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}
