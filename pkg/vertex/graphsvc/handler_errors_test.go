package graphsvc_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/vertex/graphsvc"
)

// VTX-122 boundary-path coverage for handler.go. The happy-path suite in
// handler_test.go already covers the 2xx flow; these tests target the
// error branches (nil repo, malformed JSON, missing fields, repo
// sentinels) so the package can reach the 80% floor without integration
// tests.

// nilHandler returns a chi router configured with a Handler whose repo
// and templates are both nil. Every route therefore exercises the
// `if h.repo == nil` / `if h.templates == nil` guards.
func nilHandler(t *testing.T) chi.Router {
	t.Helper()
	h := graphsvc.NewHandler(nil, nil)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r
}

func doRaw(t *testing.T, r chi.Router, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestGraphsHandler_Given_NilRepo_When_AnyRoute_Then_500RepoNotConfigured(t *testing.T) {
	r := nilHandler(t)
	cases := []struct {
		name   string
		method string
		path   string
		body   []byte
	}{
		{"create", http.MethodPost, "/api/vertex/v1/graphs", []byte(`{"ontologyRid":"o","name":"n"}`)},
		{"get", http.MethodGet, "/api/vertex/v1/graphs/ri.vertex.main.graph.x", nil},
		{"update", http.MethodPut, "/api/vertex/v1/graphs/ri.vertex.main.graph.x", []byte(`{"payload":{}}`)},
		{"patchLayout", http.MethodPatch, "/api/vertex/v1/graphs/ri.vertex.main.graph.x/layout", []byte(`{"positions":{}}`)},
		{"duplicate", http.MethodPost, "/api/vertex/v1/graphs/ri.vertex.main.graph.x/duplicate", nil},
		{"saveAsTemplate", http.MethodPost, "/api/vertex/v1/graphs/ri.vertex.main.graph.x/save-as-template", []byte(`{}`)},
		{"history", http.MethodGet, "/api/vertex/v1/graphs/ri.vertex.main.graph.x/history", nil},
		{"getVersion", http.MethodGet, "/api/vertex/v1/graphs/ri.vertex.main.graph.x/versions/1", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := doRaw(t, r, c.method, c.path, c.body)
			if w.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500; body: %s", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "RepoNotConfigured") {
				t.Errorf("body should mention RepoNotConfigured, got %s", w.Body.String())
			}
		})
	}
}

func TestGraphsHandler_Given_MalformedJSON_When_PostOrPutOrPatch_Then_400InvalidJSON(t *testing.T) {
	r, _, _ := newTestHandler(t)
	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"create", http.MethodPost, "/api/vertex/v1/graphs"},
		{"update", http.MethodPut, "/api/vertex/v1/graphs/ri.vertex.main.graph.x"},
		{"patchLayout", http.MethodPatch, "/api/vertex/v1/graphs/ri.vertex.main.graph.x/layout"},
		{"saveAsTemplate", http.MethodPost, "/api/vertex/v1/graphs/ri.vertex.main.graph.x/save-as-template"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := doRaw(t, r, c.method, c.path, []byte(`{not-json`))
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body: %s", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "InvalidJSON") {
				t.Errorf("body should mention InvalidJSON, got %s", w.Body.String())
			}
		})
	}
}

func TestGraphsHandler_Given_BlankName_When_Create_Then_400MissingName(t *testing.T) {
	r, _, _ := newTestHandler(t)
	w := doRaw(t, r, http.MethodPost, "/api/vertex/v1/graphs",
		[]byte(`{"ontologyRid":"ri.ontology.main.ontology.vtx","name":"   "}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "MissingName") {
		t.Errorf("body should mention MissingName, got %s", w.Body.String())
	}
}

func TestGraphsHandler_Given_EmptyPayload_When_Update_Then_400MissingPayload(t *testing.T) {
	r, _, _ := newTestHandler(t)
	w := doRaw(t, r, http.MethodPut, "/api/vertex/v1/graphs/ri.vertex.main.graph.x", []byte(`{}`))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "MissingPayload") {
		t.Errorf("body should mention MissingPayload, got %s", w.Body.String())
	}
}

func TestGraphsHandler_Given_EmptyPositions_When_PatchLayout_Then_400MissingPositions(t *testing.T) {
	r, _, _ := newTestHandler(t)
	w := doRaw(t, r, http.MethodPatch, "/api/vertex/v1/graphs/ri.vertex.main.graph.x/layout", []byte(`{}`))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "MissingPositions") {
		t.Errorf("body should mention MissingPositions, got %s", w.Body.String())
	}
}

func TestGraphsHandler_Given_UnknownRid_When_MutatingRoute_Then_404(t *testing.T) {
	r, _, _ := newTestHandler(t)
	missing := "/api/vertex/v1/graphs/ri.vertex.main.graph.00000000-0000-0000-0000-000000000000"
	cases := []struct {
		name   string
		method string
		path   string
		body   []byte
	}{
		{"update", http.MethodPut, missing, []byte(`{"payload":{"x":1}}`)},
		{"patchLayout", http.MethodPatch, missing + "/layout", []byte(`{"positions":{"a":1}}`)},
		{"duplicate", http.MethodPost, missing + "/duplicate", nil},
		{"saveAsTemplate", http.MethodPost, missing + "/save-as-template", []byte(`{}`)},
		{"history", http.MethodGet, missing + "/history", nil},
		{"getVersion", http.MethodGet, missing + "/versions/1", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := doRaw(t, r, c.method, c.path, c.body)
			if w.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404; body: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestGraphsHandler_Given_InvalidVersion_When_GetVersion_Then_400(t *testing.T) {
	r, _, _ := newTestHandler(t)
	w := doRaw(t, r, http.MethodGet, "/api/vertex/v1/graphs/ri.vertex.main.graph.x/versions/abc", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "InvalidVersion") {
		t.Errorf("body should mention InvalidVersion, got %s", w.Body.String())
	}
	// version 0 is also rejected (must be >= 1).
	w0 := doRaw(t, r, http.MethodGet, "/api/vertex/v1/graphs/ri.vertex.main.graph.x/versions/0", nil)
	if w0.Code != http.StatusBadRequest {
		t.Errorf("version 0: status = %d, want 400", w0.Code)
	}
}

func TestGraphsHandler_Given_BlankTemplateName_When_SaveAsTemplate_Then_DefaultsToSourceNameSuffix(t *testing.T) {
	r, _, templates := newTestHandler(t)
	createResp := doRaw(t, r, http.MethodPost, "/api/vertex/v1/graphs",
		[]byte(`{"ontologyRid":"ri.ontology.main.ontology.vtx","name":"Sample","payload":{"layers":[]}}`))
	var created map[string]any
	_ = json.Unmarshal(createResp.Body.Bytes(), &created)
	rid := created["rid"].(string)

	w := doRaw(t, r, http.MethodPost, "/api/vertex/v1/graphs/"+rid+"/save-as-template",
		[]byte(`{"name":"  "}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	gotName, _ := resp["name"].(string)
	wantSuffix := "Sample (template)"
	if gotName != wantSuffix {
		t.Errorf("template name = %q, want %q", gotName, wantSuffix)
	}
	if templates.Count() != 1 {
		t.Errorf("template count = %d, want 1", templates.Count())
	}
}

// Handler wired with a repo but nil templates: save-as-template must
// still 500 (the templates dep is required).
func TestGraphsHandler_Given_NilTemplates_When_SaveAsTemplate_Then_500(t *testing.T) {
	repo := graphsvc.NewMemRepo()
	h := graphsvc.NewHandler(repo, nil)
	r := chi.NewRouter()
	h.RegisterRoutes(r)

	createResp := doRaw(t, r, http.MethodPost, "/api/vertex/v1/graphs",
		[]byte(`{"ontologyRid":"o","name":"n"}`))
	var created map[string]any
	_ = json.Unmarshal(createResp.Body.Bytes(), &created)
	rid := created["rid"].(string)

	w := doRaw(t, r, http.MethodPost, "/api/vertex/v1/graphs/"+rid+"/save-as-template", []byte(`{}`))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
}

// errorRepo lets the failing branches of writeRepoError + history's
// ListVersions error path be exercised without integration tests.
type errorRepo struct {
	graphsvc.Repo
	err error
}

func (e errorRepo) Create(_ context.Context, _, name, createdBy string, payload json.RawMessage, versioned bool) (*graphsvc.Graph, error) {
	_, _, _, _, _ = name, createdBy, payload, versioned, false
	return nil, e.err
}
func (e errorRepo) Get(_ context.Context, _ string) (*graphsvc.Graph, error) { return nil, e.err }
func (e errorRepo) Update(_ context.Context, _ string, _ json.RawMessage) (*graphsvc.Graph, error) {
	return nil, e.err
}
func (e errorRepo) UpdateLayout(_ context.Context, _ string, _ json.RawMessage) error {
	return e.err
}
func (e errorRepo) Duplicate(_ context.Context, _ string) (*graphsvc.Graph, error) {
	return nil, e.err
}
func (e errorRepo) GetVersion(_ context.Context, _ string, _ int) (*graphsvc.Graph, error) {
	return nil, e.err
}
func (e errorRepo) ListVersions(_ context.Context, _ string) ([]graphsvc.GraphVersion, error) {
	return nil, e.err
}

// listVersionsFailRepo: Get works, ListVersions fails. Exercises the
// `ListVersionsFailed` 500 branch in history().
type listVersionsFailRepo struct {
	*graphsvc.MemRepo
	failVersions bool
}

func (r *listVersionsFailRepo) ListVersions(ctx context.Context, ridStr string) ([]graphsvc.GraphVersion, error) {
	if r.failVersions {
		return nil, errors.New("disk full")
	}
	return r.MemRepo.ListVersions(ctx, ridStr)
}

func TestGraphsHandler_Given_RepoCreateError_When_POST_Then_500CreateGraphFailed(t *testing.T) {
	h := graphsvc.NewHandler(errorRepo{err: errors.New("boom")}, nil)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	w := doRaw(t, r, http.MethodPost, "/api/vertex/v1/graphs",
		[]byte(`{"ontologyRid":"o","name":"n"}`))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "CreateGraphFailed") {
		t.Errorf("body should mention CreateGraphFailed, got %s", w.Body.String())
	}
}

func TestGraphsHandler_Given_UnknownRepoError_When_Get_Then_500GraphRepoError(t *testing.T) {
	h := graphsvc.NewHandler(errorRepo{err: errors.New("unexpected db error")}, nil)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	w := doRaw(t, r, http.MethodGet, "/api/vertex/v1/graphs/ri.vertex.main.graph.x", nil)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "GraphRepoError") {
		t.Errorf("body should mention GraphRepoError, got %s", w.Body.String())
	}
}

func TestGraphsHandler_Given_InvalidPositionsSentinel_When_PatchLayout_Then_400(t *testing.T) {
	h := graphsvc.NewHandler(errorRepo{err: graphsvc.ErrInvalidPositions}, nil)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	w := doRaw(t, r, http.MethodPatch, "/api/vertex/v1/graphs/ri.vertex.main.graph.x/layout",
		[]byte(`{"positions":{"n1":{"x":1}}}`))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "MissingPositions") {
		t.Errorf("body should mention MissingPositions, got %s", w.Body.String())
	}
}

func TestGraphsHandler_Given_ListVersionsError_When_History_Then_500ListVersionsFailed(t *testing.T) {
	mem := graphsvc.NewMemRepo()
	h := graphsvc.NewHandler(&listVersionsFailRepo{MemRepo: mem, failVersions: true}, nil)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	createResp := doRaw(t, r, http.MethodPost, "/api/vertex/v1/graphs",
		[]byte(`{"ontologyRid":"o","name":"n"}`))
	var created map[string]any
	_ = json.Unmarshal(createResp.Body.Bytes(), &created)
	rid := created["rid"].(string)

	w := doRaw(t, r, http.MethodGet, "/api/vertex/v1/graphs/"+rid+"/history", nil)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "ListVersionsFailed") {
		t.Errorf("body should mention ListVersionsFailed, got %s", w.Body.String())
	}
}
