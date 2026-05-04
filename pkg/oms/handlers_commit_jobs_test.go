package oms_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// fakeCommitJobStore is an in-memory CommitJobStore for handler tests.
// Not safe for cross-process use; tests assert behaviour against a single
// goroutine + a wait-for-status helper.
type fakeCommitJobStore struct {
	mu   sync.Mutex
	rows map[string]*oms.CommitJob // key = function_rid + "|" + commit_sha
	seq  int64
}

func newFakeCommitJobStore() *fakeCommitJobStore {
	return &fakeCommitJobStore{rows: make(map[string]*oms.CommitJob)}
}

func (s *fakeCommitJobStore) UpsertCommitJob(ctx context.Context, job *oms.CommitJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := job.FunctionRID + "|" + job.CommitSha
	if existing, ok := s.rows[key]; ok {
		existing.Status = job.Status
		existing.LintOutput = job.LintOutput
		existing.TestOutput = job.TestOutput
		existing.ErrorMessage = job.ErrorMessage
		existing.StartedAt = job.StartedAt
		existing.FinishedAt = job.FinishedAt
		existing.UpdatedAt = time.Now()
		// surface the persisted ID back to the caller so the goroutine
		// re-uses it on subsequent updates.
		job.ID = existing.ID
		job.CreatedAt = existing.CreatedAt
		job.UpdatedAt = existing.UpdatedAt
		return nil
	}
	s.seq++
	job.ID = s.seq
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
	}
	job.UpdatedAt = time.Now()
	clone := *job
	s.rows[key] = &clone
	return nil
}

func (s *fakeCommitJobStore) GetCommitJob(ctx context.Context, rid, sha string) (*oms.CommitJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[rid+"|"+sha]
	if !ok {
		return nil, oms.ErrCommitJobNotFound
	}
	clone := *row
	return &clone, nil
}

func (s *fakeCommitJobStore) ListCommitJobs(ctx context.Context, rid string, limit int) ([]oms.CommitJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]oms.CommitJob, 0)
	for _, row := range s.rows {
		if row.FunctionRID == rid {
			out = append(out, *row)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// waitForJob polls the store until the job exists with the requested
// terminal status (or the deadline expires). Tests use this to bridge the
// async goroutine the handler fires.
func waitForJobStatus(t *testing.T, store *fakeCommitJobStore, rid, sha string, want oms.CommitJobStatus, timeout time.Duration) *oms.CommitJob {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		job, err := store.GetCommitJob(context.Background(), rid, sha)
		if err == nil && job.Status == want {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("commit job for %s/%s never reached status %q within %s", rid, sha, want, timeout)
	return nil
}

// stubCommitJobRunner returns a fixed result so handler tests don't have
// to care about the real Goja-based runner's behaviour.
type stubCommitJobRunner struct {
	mu     sync.Mutex
	result oms.CommitJobRunResult
	calls  []oms.CommitJobRunInput
}

func (r *stubCommitJobRunner) RunCommitJob(ctx context.Context, in oms.CommitJobRunInput) oms.CommitJobRunResult {
	r.mu.Lock()
	r.calls = append(r.calls, in)
	out := r.result
	r.mu.Unlock()
	return out
}

func setupCommitJobsRouter(repo oms.Repository, repoStore oms.FunctionRepoStore, jobStore oms.CommitJobStore, runner oms.CommitJobRunner) (*chi.Mux, *oms.OMSHandler) {
	handler := oms.NewOMSHandler(repo)
	if repoStore != nil {
		handler.SetFunctionRepoStore(repoStore)
	}
	if jobStore != nil {
		handler.SetCommitJobStore(jobStore)
	}
	if runner != nil {
		handler.SetCommitJobRunner(runner)
	}
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}/commits", handler.CreateFunctionRepoCommit)
	r.Get("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}/commits/{hash}/job", handler.GetFunctionRepoCommitJob)
	return r, handler
}

func TestCreateFunctionRepoCommit_DispatchesCIJobOnSuccess(t *testing.T) {
	repo := us415SeedRepo()
	store := &fakeFuncRepoStore{}
	jobStore := newFakeCommitJobStore()
	runner := &stubCommitJobRunner{result: oms.CommitJobRunResult{
		Status:     oms.CommitJobStatusSuccess,
		LintOutput: "no lint issues",
		TestOutput: "no tests declared — skipped",
	}}
	router, _ := setupCommitJobsRouter(repo, store, jobStore, runner)

	body := `{"message":"feat","sourceCode":"function v1() {}"}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/functions/hello/commits",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("commit: want 201, got %d body=%s", w.Code, w.Body.String())
	}
	var commit oms.FunctionRepoCommit
	if err := json.Unmarshal(w.Body.Bytes(), &commit); err != nil {
		t.Fatalf("decode commit: %v", err)
	}

	final := waitForJobStatus(t, jobStore, "ri.ontology.main.function.f1", commit.Hash, oms.CommitJobStatusSuccess, 2*time.Second)
	if final.LintOutput == "" {
		t.Fatalf("expected lint output to be persisted; got %+v", final)
	}
	if final.StartedAt == nil || final.FinishedAt == nil {
		t.Fatalf("expected started/finished timestamps to be populated; got %+v", final)
	}

	// The runner must have been called with the canonical RID + the
	// committed source code, NOT with the URL identifier.
	runner.mu.Lock()
	calls := runner.calls
	runner.mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("expected 1 runner call, got %d", len(calls))
	}
	if calls[0].FunctionRID != "ri.ontology.main.function.f1" {
		t.Fatalf("runner called with wrong rid: %q", calls[0].FunctionRID)
	}
	if calls[0].CommitSha != commit.Hash {
		t.Fatalf("runner called with wrong sha: got %q want %q", calls[0].CommitSha, commit.Hash)
	}
	if calls[0].SourceCode != "function v1() {}" {
		t.Fatalf("runner called with wrong source: %q", calls[0].SourceCode)
	}
}

func TestCreateFunctionRepoCommit_NoStoreSkipsCI(t *testing.T) {
	repo := us415SeedRepo()
	store := &fakeFuncRepoStore{}
	runner := &stubCommitJobRunner{result: oms.CommitJobRunResult{Status: oms.CommitJobStatusSuccess}}
	// Note: jobStore is nil — runner should never be called.
	router, _ := setupCommitJobsRouter(repo, store, nil, runner)

	body := `{"message":"m","sourceCode":"x"}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/functions/hello/commits",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("commit: want 201, got %d", w.Code)
	}
	// Give the goroutine (if any) a chance to run.
	time.Sleep(50 * time.Millisecond)
	runner.mu.Lock()
	calls := runner.calls
	runner.mu.Unlock()
	if len(calls) != 0 {
		t.Fatalf("runner should NOT be invoked when store is unwired; got %d calls", len(calls))
	}
}

func TestCreateFunctionRepoCommit_StoreWithoutRunnerLeavesQueued(t *testing.T) {
	repo := us415SeedRepo()
	store := &fakeFuncRepoStore{}
	jobStore := newFakeCommitJobStore()
	router, _ := setupCommitJobsRouter(repo, store, jobStore, nil)

	body := `{"message":"m","sourceCode":"x"}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/functions/hello/commits",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("commit: want 201, got %d", w.Code)
	}
	var commit oms.FunctionRepoCommit
	if err := json.Unmarshal(w.Body.Bytes(), &commit); err != nil {
		t.Fatalf("decode: %v", err)
	}
	job := waitForJobStatus(t, jobStore, "ri.ontology.main.function.f1", commit.Hash, oms.CommitJobStatusQueued, 1*time.Second)
	if job.Status != oms.CommitJobStatusQueued {
		t.Fatalf("expected queued status, got %q", job.Status)
	}
}

func TestGetFunctionRepoCommitJob_RoundTrip(t *testing.T) {
	repo := us415SeedRepo()
	jobStore := newFakeCommitJobStore()
	now := time.Now()
	if err := jobStore.UpsertCommitJob(context.Background(), &oms.CommitJob{
		FunctionRID: "ri.ontology.main.function.f1",
		CommitSha:   "deadbeef",
		Status:      oms.CommitJobStatusFailure,
		LintOutput:  "syntax error at line 1",
		StartedAt:   &now,
		FinishedAt:  &now,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	router, _ := setupCommitJobsRouter(repo, &fakeFuncRepoStore{}, jobStore, nil)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/northwind/functions/hello/commits/deadbeef/job", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var got oms.CommitJob
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != oms.CommitJobStatusFailure || got.LintOutput == "" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestGetFunctionRepoCommitJob_NotFound(t *testing.T) {
	repo := us415SeedRepo()
	router, _ := setupCommitJobsRouter(repo, &fakeFuncRepoStore{}, newFakeCommitJobStore(), nil)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/northwind/functions/hello/commits/missing/job", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["errorName"] != "CommitJobNotFound" {
		t.Fatalf("expected errorName CommitJobNotFound, got %v", resp)
	}
}

func TestGetFunctionRepoCommitJob_StoreUnwired(t *testing.T) {
	repo := us415SeedRepo()
	router, _ := setupCommitJobsRouter(repo, &fakeFuncRepoStore{}, nil, nil)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/northwind/functions/hello/commits/abc/job", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["errorCode"] != "CommitJobsNotConfigured" {
		t.Fatalf("expected errorCode CommitJobsNotConfigured, got %v", resp)
	}
}

func TestGetFunctionRepoCommitJob_FunctionNotFound(t *testing.T) {
	repo := us415SeedRepo()
	router, _ := setupCommitJobsRouter(repo, &fakeFuncRepoStore{}, newFakeCommitJobStore(), nil)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/northwind/functions/missing/commits/abc/job", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCreateFunctionRepoCommit_RunnerFailureRecorded(t *testing.T) {
	repo := us415SeedRepo()
	store := &fakeFuncRepoStore{}
	jobStore := newFakeCommitJobStore()
	runner := &stubCommitJobRunner{result: oms.CommitJobRunResult{
		Status:       oms.CommitJobStatusFailure,
		LintOutput:   "ParseError: unexpected token",
		ErrorMessage: "lint failed",
	}}
	router, _ := setupCommitJobsRouter(repo, store, jobStore, runner)

	body := `{"message":"bad","sourceCode":"function ("}` // intentionally malformed JS
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/functions/hello/commits",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		// The commit itself must succeed; the failure is in the CI job, not the commit.
		t.Fatalf("commit: want 201, got %d", w.Code)
	}
	var commit oms.FunctionRepoCommit
	if err := json.Unmarshal(w.Body.Bytes(), &commit); err != nil {
		t.Fatalf("decode: %v", err)
	}
	final := waitForJobStatus(t, jobStore, "ri.ontology.main.function.f1", commit.Hash, oms.CommitJobStatusFailure, 2*time.Second)
	if final.ErrorMessage == "" || final.LintOutput == "" {
		t.Fatalf("expected error + lint output to be persisted; got %+v", final)
	}
}

// Sanity: store sentinels match the documented errors.Is semantics.
func TestCommitJobStore_NotFoundSentinel(t *testing.T) {
	store := newFakeCommitJobStore()
	_, err := store.GetCommitJob(context.Background(), "rid", "sha")
	if !errors.Is(err, oms.ErrCommitJobNotFound) {
		t.Fatalf("expected ErrCommitJobNotFound, got %v", err)
	}
}
