package oms_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// fakeFuncRepoStore is an in-memory FunctionRepoStore that mirrors the
// shape of pkg/funcrepo.Manager but stays free of go-git, so the handler
// tests can assert wire behaviour without spinning up a filesystem repo.
type fakeFuncRepoStore struct {
	mu          sync.Mutex
	commits     map[string][]oms.FunctionRepoCommit
	sources     map[string]map[string]string
	commitFn    func(rid string, in oms.FunctionRepoCommitInput) (oms.FunctionRepoCommit, error)
	logFn       func(rid string, limit int) ([]oms.FunctionRepoCommit, error)
	getCommitFn func(rid, hash string) (oms.FunctionRepoCommitWithSource, error)
}

func (f *fakeFuncRepoStore) Commit(ctx context.Context, rid string, in oms.FunctionRepoCommitInput) (oms.FunctionRepoCommit, error) {
	if f.commitFn != nil {
		return f.commitFn(rid, in)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.commits == nil {
		f.commits = make(map[string][]oms.FunctionRepoCommit)
	}
	commit := oms.FunctionRepoCommit{
		Hash:       fmt.Sprintf("hash-%s-%d", rid, len(f.commits[rid])),
		Message:    in.Message,
		Author:     in.Author,
		Email:      in.Email,
		AuthorDate: in.When,
	}
	if commit.Author == "" {
		commit.Author = "weave"
	}
	if commit.Email == "" {
		commit.Email = "weave@weave.local"
	}
	if commit.AuthorDate.IsZero() {
		commit.AuthorDate = time.Now()
	}
	f.commits[rid] = append([]oms.FunctionRepoCommit{commit}, f.commits[rid]...)
	if f.sources == nil {
		f.sources = make(map[string]map[string]string)
	}
	if f.sources[rid] == nil {
		f.sources[rid] = make(map[string]string)
	}
	f.sources[rid][commit.Hash] = in.SourceCode
	return commit, nil
}

func (f *fakeFuncRepoStore) GetCommit(ctx context.Context, rid, hash string) (oms.FunctionRepoCommitWithSource, error) {
	if f.getCommitFn != nil {
		return f.getCommitFn(rid, hash)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	commits := f.commits[rid]
	for _, c := range commits {
		if c.Hash == hash {
			source := ""
			if f.sources != nil {
				source = f.sources[rid][hash]
			}
			return oms.FunctionRepoCommitWithSource{
				FunctionRepoCommit: c,
				SourceCode:         source,
			}, nil
		}
	}
	return oms.FunctionRepoCommitWithSource{}, oms.ErrFunctionRepoCommitNotFound
}

func (f *fakeFuncRepoStore) Log(ctx context.Context, rid string, limit int) ([]oms.FunctionRepoCommit, error) {
	if f.logFn != nil {
		return f.logFn(rid, limit)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	commits := f.commits[rid]
	if limit > 0 && len(commits) > limit {
		commits = commits[:limit]
	}
	return commits, nil
}

func setupFunctionRepoRouter(repo oms.Repository, store oms.FunctionRepoStore) *chi.Mux {
	handler := oms.NewOMSHandler(repo)
	if store != nil {
		handler.SetFunctionRepoStore(store)
	}
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}/commits", handler.CreateFunctionRepoCommit)
	r.Get("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}/log", handler.ListFunctionRepoCommits)
	r.Get("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}/commits/{hash}", handler.GetFunctionRepoCommit)
	return r
}

func us415SeedRepo() *mockRepo {
	return &mockRepo{
		ontologies: []oms.Ontology{{
			RID:         "ri.ontology.main.ontology.o1",
			APIName:     "northwind",
			DisplayName: "Northwind",
		}},
		functions: []oms.Function{{
			RID:         "ri.ontology.main.function.f1",
			OntologyRID: "ri.ontology.main.ontology.o1",
			Name:        "hello",
			Version:     "1.0.0",
			SourceCode:  "function hello() {}",
		}},
	}
}

func TestCreateFunctionRepoCommit_RoundTrip(t *testing.T) {
	repo := us415SeedRepo()
	store := &fakeFuncRepoStore{}
	router := setupFunctionRepoRouter(repo, store)

	body := `{"message":"first","sourceCode":"function v2() {}","author":"alice","email":"alice@example.com"}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/functions/hello/commits",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%s", w.Code, w.Body.String())
	}
	var commit oms.FunctionRepoCommit
	if err := json.Unmarshal(w.Body.Bytes(), &commit); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if commit.Hash == "" || commit.Message != "first" || commit.Author != "alice" {
		t.Fatalf("unexpected commit: %+v", commit)
	}

	// Subsequent /log returns the new commit.
	logReq := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/northwind/functions/hello/log", nil)
	logResp := httptest.NewRecorder()
	router.ServeHTTP(logResp, logReq)
	if logResp.Code != http.StatusOK {
		t.Fatalf("log: want 200, got %d body=%s", logResp.Code, logResp.Body.String())
	}
	var logBody struct {
		Data []oms.FunctionRepoCommit `json:"data"`
	}
	if err := json.Unmarshal(logResp.Body.Bytes(), &logBody); err != nil {
		t.Fatalf("decode log: %v", err)
	}
	if len(logBody.Data) != 1 || logBody.Data[0].Hash != commit.Hash {
		t.Fatalf("log mismatch: %+v", logBody.Data)
	}
}

func TestCreateFunctionRepoCommit_PatchAlias(t *testing.T) {
	repo := us415SeedRepo()
	store := &fakeFuncRepoStore{}
	router := setupFunctionRepoRouter(repo, store)

	body := `{"message":"alias","patch":"function v3() {}"}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/functions/hello/commits",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("patch alias: want 201, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCreateFunctionRepoCommit_ResolvesByRID(t *testing.T) {
	repo := us415SeedRepo()
	store := &fakeFuncRepoStore{}
	var seenRID string
	store.commitFn = func(rid string, in oms.FunctionRepoCommitInput) (oms.FunctionRepoCommit, error) {
		seenRID = rid
		return oms.FunctionRepoCommit{Hash: "h", Message: in.Message}, nil
	}
	router := setupFunctionRepoRouter(repo, store)

	// URL uses the bare name; the handler should resolve to the canonical
	// RID before passing it to the store.
	body := `{"message":"by-name","sourceCode":"x"}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/functions/hello/commits",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: %d body=%s", w.Code, w.Body.String())
	}
	if seenRID != "ri.ontology.main.function.f1" {
		t.Fatalf("expected RID resolution, got %q", seenRID)
	}
}

func TestCreateFunctionRepoCommit_RejectsMissingBody(t *testing.T) {
	repo := us415SeedRepo()
	router := setupFunctionRepoRouter(repo, &fakeFuncRepoStore{})

	cases := []struct {
		name string
		body string
	}{
		{"empty message", `{"sourceCode":"x"}`},
		{"empty source", `{"message":"m"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost,
				"/api/v2/ontologies/northwind/functions/hello/commits",
				bytes.NewBufferString(c.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestCreateFunctionRepoCommit_FunctionNotFound(t *testing.T) {
	repo := us415SeedRepo()
	router := setupFunctionRepoRouter(repo, &fakeFuncRepoStore{})

	body := `{"message":"m","sourceCode":"x"}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/functions/missing/commits",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCreateFunctionRepoCommit_ReturnsServiceUnavailableWhenStoreUnwired(t *testing.T) {
	repo := us415SeedRepo()
	router := setupFunctionRepoRouter(repo, nil)

	body := `{"message":"m","sourceCode":"x"}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/functions/hello/commits",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["errorCode"] != "FunctionRepoNotConfigured" {
		t.Fatalf("expected FunctionRepoNotConfigured, got %v", resp)
	}
}

func TestListFunctionRepoCommits_LimitParameter(t *testing.T) {
	repo := us415SeedRepo()
	store := &fakeFuncRepoStore{}
	var seenLimit int
	store.logFn = func(rid string, limit int) ([]oms.FunctionRepoCommit, error) {
		seenLimit = limit
		return []oms.FunctionRepoCommit{{Hash: "h"}}, nil
	}
	router := setupFunctionRepoRouter(repo, store)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/northwind/functions/hello/log?limit=7", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if seenLimit != 7 {
		t.Fatalf("limit not propagated: got %d", seenLimit)
	}
}

func TestListFunctionRepoCommits_EmptyRepoReturns200(t *testing.T) {
	repo := us415SeedRepo()
	store := &fakeFuncRepoStore{}
	router := setupFunctionRepoRouter(repo, store)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/northwind/functions/hello/log", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data []oms.FunctionRepoCommit `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 0 {
		t.Fatalf("want empty array, got %v", resp.Data)
	}
}

func TestGetFunctionRepoCommit_RoundtripsSource(t *testing.T) {
	repo := us415SeedRepo()
	store := &fakeFuncRepoStore{}
	router := setupFunctionRepoRouter(repo, store)

	createBody := `{"message":"first","sourceCode":"function v1() { return 1; }"}`
	createReq := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/functions/hello/commits",
		bytes.NewBufferString(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	router.ServeHTTP(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("create: %d body=%s", createW.Code, createW.Body.String())
	}
	var created oms.FunctionRepoCommit
	if err := json.Unmarshal(createW.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	getReq := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/northwind/functions/hello/commits/"+created.Hash, nil)
	getW := httptest.NewRecorder()
	router.ServeHTTP(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("get: want 200, got %d body=%s", getW.Code, getW.Body.String())
	}
	var got oms.FunctionRepoCommitWithSource
	if err := json.Unmarshal(getW.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if got.Hash != created.Hash {
		t.Fatalf("hash mismatch: %s vs %s", got.Hash, created.Hash)
	}
	if got.SourceCode != "function v1() { return 1; }" {
		t.Fatalf("source mismatch: %q", got.SourceCode)
	}
}

func TestGetFunctionRepoCommit_NotFound(t *testing.T) {
	repo := us415SeedRepo()
	router := setupFunctionRepoRouter(repo, &fakeFuncRepoStore{})

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/northwind/functions/hello/commits/deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestGetFunctionRepoCommit_NoCommitsReturns404(t *testing.T) {
	repo := us415SeedRepo()
	store := &fakeFuncRepoStore{
		getCommitFn: func(rid, hash string) (oms.FunctionRepoCommitWithSource, error) {
			return oms.FunctionRepoCommitWithSource{}, oms.ErrFunctionRepoNoCommits
		},
	}
	router := setupFunctionRepoRouter(repo, store)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/northwind/functions/hello/commits/abc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["errorName"] != "FunctionRepoNoCommits" {
		t.Fatalf("expected errorName FunctionRepoNoCommits, got %v", resp)
	}
}

func TestGetFunctionRepoCommit_StoreUnwiredReturns503(t *testing.T) {
	repo := us415SeedRepo()
	router := setupFunctionRepoRouter(repo, nil)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/northwind/functions/hello/commits/abc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestGetFunctionRepoCommit_FunctionNotFound(t *testing.T) {
	repo := us415SeedRepo()
	router := setupFunctionRepoRouter(repo, &fakeFuncRepoStore{})

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/northwind/functions/missing/commits/abc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCreateFunctionRepoCommit_StoreErrorMappedTo500(t *testing.T) {
	repo := us415SeedRepo()
	store := &fakeFuncRepoStore{
		commitFn: func(rid string, in oms.FunctionRepoCommitInput) (oms.FunctionRepoCommit, error) {
			return oms.FunctionRepoCommit{}, errors.New("disk full")
		},
	}
	router := setupFunctionRepoRouter(repo, store)

	body := `{"message":"m","sourceCode":"x"}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/functions/hello/commits",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d body=%s", w.Code, w.Body.String())
	}
}
