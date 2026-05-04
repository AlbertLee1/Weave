package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// memActionJobStore is an in-memory ActionJobStore used by async-job tests.
// Real wiring (PG) is covered by integration tests; the narrow interface keeps
// the unit path lightweight.
type memActionJobStore struct {
	mu   sync.Mutex
	jobs map[string]*ActionJob
}

func newMemActionJobStore() *memActionJobStore {
	return &memActionJobStore{jobs: make(map[string]*ActionJob)}
}

func (m *memActionJobStore) CreateActionJob(_ context.Context, job *ActionJob) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.jobs[job.JobID]; ok {
		return errors.New("duplicate job id")
	}
	copy := *job
	m.jobs[job.JobID] = &copy
	return nil
}

func (m *memActionJobStore) GetActionJob(_ context.Context, id string) (*ActionJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return nil, oms.ErrNotFound
	}
	copy := *j
	return &copy, nil
}

func (m *memActionJobStore) ReapOldActionJobs(_ context.Context, olderThan time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for id, j := range m.jobs {
		if !isTerminalJobStatus(j.Status) {
			continue
		}
		if !j.UpdatedAt.Before(olderThan) {
			continue
		}
		delete(m.jobs, id)
		n++
	}
	return n, nil
}

func (m *memActionJobStore) UpdateActionJob(_ context.Context, id string, upd ActionJobUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return oms.ErrNotFound
	}
	if upd.Status != "" {
		j.Status = upd.Status
	}
	if upd.Progress != nil {
		j.Progress = *upd.Progress
	}
	if upd.Result != nil {
		j.Result = upd.Result
	}
	if upd.ErrorMessage != nil {
		j.ErrorMessage = *upd.ErrorMessage
	}
	j.UpdatedAt = time.Now()
	return nil
}

// waitForJob polls the in-memory store until the job reaches a terminal state
// or the deadline hits. Avoids hard sleeps that make async tests flaky.
func waitForJob(t *testing.T, store *memActionJobStore, id string, deadline time.Duration) *ActionJob {
	t.Helper()
	start := time.Now()
	for time.Since(start) < deadline {
		j, err := store.GetActionJob(context.Background(), id)
		if err == nil && (j.Status == ActionJobStatusSucceeded || j.Status == ActionJobStatusFailed) {
			return j
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach terminal state within %s", id, deadline)
	return nil
}

func setupAsyncRouter(handler *Handler) *chi.Mux {
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/actions/{action}/apply", handler.Apply)
	r.Get("/api/v2/ontologies/{ontologyApiName}/actions/jobs/{jobId}", handler.GetJob)
	return r
}

// TestHandler_Apply_AsyncQuery_ReturnsJobId verifies that ?async=true returns
// a 202 with {jobId} and does not block on the underlying Apply.
func TestHandler_Apply_AsyncQuery_ReturnsJobId(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee", []ParameterDef{
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{Type: "createObject", ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					}},
			}),
		},
	}
	pub := &fakePublisher{offset: 1}
	exec := NewExecutor(repo, pub)
	store := newMemActionJobStore()
	exec.SetActionJobStore(store)
	handler := NewHandler(exec)
	router := setupAsyncRouter(handler)

	body := mustJSON(map[string]interface{}{
		"parameters": map[string]interface{}{"name": "Alice"},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/createEmployee/apply?async=true",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	var resp AsyncApplyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.JobID == "" {
		t.Fatal("expected non-empty jobId in response")
	}
	if resp.Status != ActionJobStatusPending && resp.Status != ActionJobStatusRunning && resp.Status != ActionJobStatusSucceeded {
		t.Fatalf("unexpected initial status %q", resp.Status)
	}

	// Wait for the job to finish, then confirm the row was updated.
	final := waitForJob(t, store, resp.JobID, time.Second)
	if final.Status != ActionJobStatusSucceeded {
		t.Fatalf("expected terminal SUCCEEDED status, got %q (err=%q)", final.Status, final.ErrorMessage)
	}
	if final.Progress != 100 {
		t.Fatalf("expected progress=100, got %d", final.Progress)
	}
}

// TestHandler_GetJob_ReturnsRow verifies the polling endpoint returns the
// expected shape after a job finishes.
func TestHandler_GetJob_ReturnsRow(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee", []ParameterDef{
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{Type: "createObject", ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					}},
			}),
		},
	}
	pub := &fakePublisher{offset: 5}
	exec := NewExecutor(repo, pub)
	store := newMemActionJobStore()
	exec.SetActionJobStore(store)
	handler := NewHandler(exec)
	router := setupAsyncRouter(handler)

	body := mustJSON(map[string]interface{}{
		"parameters": map[string]interface{}{"name": "Bob"},
	})
	submitReq := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/createEmployee/apply?async=true",
		bytes.NewReader(body))
	submitReq.Header.Set("Content-Type", "application/json")
	submitW := httptest.NewRecorder()
	router.ServeHTTP(submitW, submitReq)
	if submitW.Code != http.StatusAccepted {
		t.Fatalf("submit expected 202, got %d: %s", submitW.Code, submitW.Body.String())
	}
	var submit AsyncApplyResponse
	_ = json.Unmarshal(submitW.Body.Bytes(), &submit)

	_ = waitForJob(t, store, submit.JobID, time.Second)

	pollReq := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/ont-1/actions/jobs/"+submit.JobID, nil)
	pollW := httptest.NewRecorder()
	router.ServeHTTP(pollW, pollReq)
	if pollW.Code != http.StatusOK {
		t.Fatalf("poll expected 200, got %d: %s", pollW.Code, pollW.Body.String())
	}
	var poll ActionJobResponse
	if err := json.Unmarshal(pollW.Body.Bytes(), &poll); err != nil {
		t.Fatalf("unmarshal poll response: %v", err)
	}
	if poll.JobID != submit.JobID {
		t.Fatalf("poll.JobID mismatch: want %q got %q", submit.JobID, poll.JobID)
	}
	if poll.Status != ActionJobStatusSucceeded {
		t.Fatalf("poll.Status: expected SUCCEEDED, got %q", poll.Status)
	}
	if poll.Progress != 100 {
		t.Fatalf("poll.Progress: expected 100, got %d", poll.Progress)
	}
	if poll.Result == nil {
		t.Fatal("expected non-nil result payload on SUCCEEDED job")
	}
}

// TestHandler_GetJob_NotFound returns 404 for an unknown id.
func TestHandler_GetJob_NotFound(t *testing.T) {
	repo := &mockOmsRepo{}
	exec := NewExecutor(repo, nil)
	exec.SetActionJobStore(newMemActionJobStore())
	handler := NewHandler(exec)
	router := setupAsyncRouter(handler)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/ont-1/actions/jobs/does-not-exist", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestHandler_Apply_AsyncQuery_FailureSurfacedOnJob verifies that when the
// underlying Apply fails (e.g. validation error), the async job transitions
// to FAILED and carries the error message. The initial 202 still succeeds —
// failures surface via the polling endpoint, matching the async contract.
func TestHandler_Apply_AsyncQuery_FailureSurfacedOnJob(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee", []ParameterDef{
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{Type: "createObject", ObjectType: "Employee"},
			}),
		},
	}
	exec := NewExecutor(repo, nil)
	store := newMemActionJobStore()
	exec.SetActionJobStore(store)
	handler := NewHandler(exec)
	router := setupAsyncRouter(handler)

	// Missing required "name" — prepare must fail inside the async goroutine.
	body := mustJSON(map[string]interface{}{
		"parameters": map[string]interface{}{},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/createEmployee/apply?async=true",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202 accepted even for bad params, got %d: %s", w.Code, w.Body.String())
	}
	var resp AsyncApplyResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	final := waitForJob(t, store, resp.JobID, time.Second)
	if final.Status != ActionJobStatusFailed {
		t.Fatalf("expected FAILED status, got %q", final.Status)
	}
	if final.ErrorMessage == "" {
		t.Fatal("expected non-empty error message on FAILED job")
	}
}

// TestHandler_Apply_AsyncNoStore_DegradesToSync verifies that when no
// ActionJobStore is wired the ?async=true query degrades to synchronous
// behavior (the route still serves a 200 with sync payload). Matches the
// "no PG catalog" degraded-mode pattern used by other US-2xx stories.
func TestHandler_Apply_AsyncNoStore_DegradesToSync(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee", []ParameterDef{
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{Type: "createObject", ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					}},
			}),
		},
	}
	pub := &fakePublisher{offset: 1}
	exec := NewExecutor(repo, pub)
	// Intentionally NOT calling SetActionJobStore.
	handler := NewHandler(exec)
	router := setupAsyncRouter(handler)

	body := mustJSON(map[string]interface{}{
		"parameters": map[string]interface{}{"name": "Carol"},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/createEmployee/apply?async=true",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 degraded-sync response, got %d: %s", w.Code, w.Body.String())
	}
	var sync SyncApplyActionResponseV2
	if err := json.Unmarshal(w.Body.Bytes(), &sync); err != nil {
		t.Fatalf("unmarshal sync response: %v", err)
	}
	if sync.Edits == nil || sync.Edits.AddedObjectCount != 1 {
		t.Fatalf("expected sync 1 added object fallback, got %+v", sync.Edits)
	}
}
