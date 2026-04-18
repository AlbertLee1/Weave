package oms_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// setupFunctionVersionRouter wires the version-aware Function routes a US-217
// caller exercises end-to-end.
func setupFunctionVersionRouter(repo oms.Repository) *chi.Mux {
	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/functions", handler.CreateFunction)
	r.Get("/api/v2/ontologies/{ontologyApiName}/functions", handler.ListFunctions)
	r.Get("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}", handler.GetFunctionV2)
	r.Get("/api/v2/ontologies/{ontologyApiName}/functions/{functionName}/versions", handler.ListFunctionVersions)
	r.Put("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}", handler.UpdateFunction)
	r.Delete("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}", handler.DeleteFunction)
	return r
}

func newVersionRepo(t *testing.T) *mockRepo {
	t.Helper()
	return &mockRepo{
		ontologies: []oms.Ontology{{
			RID:         "ri.ontology.main.ontology.o1",
			APIName:     "northwind",
			DisplayName: "Northwind",
		}},
	}
}

func postFunction(t *testing.T, router http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/functions",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestCreateFunction_DefaultsVersionTo100(t *testing.T) {
	repo := newVersionRepo(t)
	router := setupFunctionVersionRouter(repo)

	w := postFunction(t, router, `{"name":"hello","sourceCode":"return 1"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var fn oms.Function
	if err := json.Unmarshal(w.Body.Bytes(), &fn); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if fn.Version != oms.DefaultFunctionVersion {
		t.Errorf("expected default version %q, got %q", oms.DefaultFunctionVersion, fn.Version)
	}
}

func TestCreateFunction_AcceptsExplicitSemver(t *testing.T) {
	repo := newVersionRepo(t)
	router := setupFunctionVersionRouter(repo)

	w := postFunction(t, router, `{"name":"hello","sourceCode":"return 1","version":"2.3.4"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var fn oms.Function
	if err := json.Unmarshal(w.Body.Bytes(), &fn); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if fn.Version != "2.3.4" {
		t.Errorf("expected version=2.3.4, got %q", fn.Version)
	}
}

func TestCreateFunction_RejectsInvalidSemver(t *testing.T) {
	repo := newVersionRepo(t)
	router := setupFunctionVersionRouter(repo)

	w := postFunction(t, router, `{"name":"hello","sourceCode":"return 1","version":"v1"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateFunction_NewVersionDoesNotOverwriteOld(t *testing.T) {
	repo := newVersionRepo(t)
	router := setupFunctionVersionRouter(repo)

	if w := postFunction(t, router, `{"name":"hello","sourceCode":"return 1","version":"1.0.0"}`); w.Code != http.StatusCreated {
		t.Fatalf("seed v1 failed: %d %s", w.Code, w.Body.String())
	}
	if w := postFunction(t, router, `{"name":"hello","sourceCode":"return 2","version":"2.0.0"}`); w.Code != http.StatusCreated {
		t.Fatalf("post v2 failed: %d %s", w.Code, w.Body.String())
	}
	if len(repo.functions) != 2 {
		t.Fatalf("expected 2 rows after creating two versions, got %d", len(repo.functions))
	}
	// Distinct RIDs prove neither overwrote the other.
	if repo.functions[0].RID == repo.functions[1].RID {
		t.Errorf("expected distinct RIDs across versions, got duplicate %q", repo.functions[0].RID)
	}
}

func TestCreateFunction_DuplicateVersionReturns409(t *testing.T) {
	repo := newVersionRepo(t)
	router := setupFunctionVersionRouter(repo)

	if w := postFunction(t, router, `{"name":"hello","sourceCode":"return 1","version":"1.0.0"}`); w.Code != http.StatusCreated {
		t.Fatalf("seed failed: %d %s", w.Code, w.Body.String())
	}
	w := postFunction(t, router, `{"name":"hello","sourceCode":"return 1 again","version":"1.0.0"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 on duplicate version, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetFunctionByName_ReturnsLatestSemver(t *testing.T) {
	repo := newVersionRepo(t)
	repo.functions = []oms.Function{
		{RID: "ri.ontology.main.function.f1", OntologyRID: "ri.ontology.main.ontology.o1", Name: "hello", Version: "1.0.0", SourceCode: "v1"},
		{RID: "ri.ontology.main.function.f2", OntologyRID: "ri.ontology.main.ontology.o1", Name: "hello", Version: "10.0.0", SourceCode: "v10"},
		{RID: "ri.ontology.main.function.f3", OntologyRID: "ri.ontology.main.ontology.o1", Name: "hello", Version: "2.0.0", SourceCode: "v2"},
	}
	router := setupFunctionVersionRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/functions/hello", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var fn oms.Function
	if err := json.Unmarshal(w.Body.Bytes(), &fn); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if fn.Version != "10.0.0" {
		t.Errorf("expected latest=10.0.0, got %q", fn.Version)
	}
	if fn.SourceCode != "v10" {
		t.Errorf("expected sourceCode=v10, got %q", fn.SourceCode)
	}
}

func TestGetFunctionByNameAtVersion_ResolvesPin(t *testing.T) {
	repo := newVersionRepo(t)
	repo.functions = []oms.Function{
		{RID: "ri.ontology.main.function.f1", OntologyRID: "ri.ontology.main.ontology.o1", Name: "hello", Version: "1.0.0", SourceCode: "v1"},
		{RID: "ri.ontology.main.function.f2", OntologyRID: "ri.ontology.main.ontology.o1", Name: "hello", Version: "2.0.0", SourceCode: "v2"},
	}
	router := setupFunctionVersionRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/functions/hello@1.0.0", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var fn oms.Function
	_ = json.Unmarshal(w.Body.Bytes(), &fn)
	if fn.Version != "1.0.0" {
		t.Errorf("expected pinned version=1.0.0, got %q", fn.Version)
	}
	if fn.SourceCode != "v1" {
		t.Errorf("expected pinned source=v1, got %q", fn.SourceCode)
	}
}

func TestGetFunctionByNameAtVersion_UnknownVersionIs404(t *testing.T) {
	repo := newVersionRepo(t)
	repo.functions = []oms.Function{
		{RID: "ri.ontology.main.function.f1", OntologyRID: "ri.ontology.main.ontology.o1", Name: "hello", Version: "1.0.0", SourceCode: "v1"},
	}
	router := setupFunctionVersionRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/functions/hello@9.9.9", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown version, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListFunctions_SortsByVersionDescPerName(t *testing.T) {
	repo := newVersionRepo(t)
	repo.functions = []oms.Function{
		{RID: "f1", OntologyRID: "ri.ontology.main.ontology.o1", Name: "alpha", Version: "1.0.0"},
		{RID: "f2", OntologyRID: "ri.ontology.main.ontology.o1", Name: "alpha", Version: "10.0.0"},
		{RID: "f3", OntologyRID: "ri.ontology.main.ontology.o1", Name: "alpha", Version: "2.0.0"},
		{RID: "f4", OntologyRID: "ri.ontology.main.ontology.o1", Name: "beta", Version: "1.5.0"},
	}
	router := setupFunctionVersionRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/functions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Data []oms.Function `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := []string{"10.0.0", "2.0.0", "1.0.0", "1.5.0"}
	if len(resp.Data) != len(want) {
		t.Fatalf("expected %d rows, got %d", len(want), len(resp.Data))
	}
	for i, v := range want {
		if resp.Data[i].Version != v {
			t.Errorf("position %d: expected version %q, got %q (%s)",
				i, v, resp.Data[i].Version, resp.Data[i].Name)
		}
	}
}

func TestListFunctionVersions_ReturnsAllSortedDesc(t *testing.T) {
	repo := newVersionRepo(t)
	repo.functions = []oms.Function{
		{RID: "f1", OntologyRID: "ri.ontology.main.ontology.o1", Name: "hello", Version: "1.0.0"},
		{RID: "f2", OntologyRID: "ri.ontology.main.ontology.o1", Name: "hello", Version: "2.0.0"},
		{RID: "f3", OntologyRID: "ri.ontology.main.ontology.o1", Name: "hello", Version: "1.5.0"},
		{RID: "f4", OntologyRID: "ri.ontology.main.ontology.o1", Name: "other", Version: "1.0.0"},
	}
	router := setupFunctionVersionRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/functions/hello/versions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Name string         `json:"name"`
		Data []oms.Function `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Name != "hello" {
		t.Errorf("expected name=hello, got %q", resp.Name)
	}
	want := []string{"2.0.0", "1.5.0", "1.0.0"}
	if len(resp.Data) != len(want) {
		t.Fatalf("expected %d versions, got %d", len(want), len(resp.Data))
	}
	for i, v := range want {
		if resp.Data[i].Version != v {
			t.Errorf("position %d: expected %q, got %q", i, v, resp.Data[i].Version)
		}
		if resp.Data[i].Name != "hello" {
			t.Errorf("position %d: expected name=hello, got %q", i, resp.Data[i].Name)
		}
	}
}

func TestListFunctionVersions_UnknownNameIs404(t *testing.T) {
	repo := newVersionRepo(t)
	router := setupFunctionVersionRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind/functions/missing/versions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown function name, got %d", w.Code)
	}
}
