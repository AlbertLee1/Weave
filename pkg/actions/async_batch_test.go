package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
)

// recordingPublisher captures every PublishProgress call so async-batch tests
// can assert that progress fanout fired with the expected percentages.
type recordingPublisher struct {
	mu     sync.Mutex
	events []ProgressEvent
}

func (r *recordingPublisher) PublishProgress(_ string, data []byte) error {
	var evt ProgressEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return err
	}
	r.mu.Lock()
	r.events = append(r.events, evt)
	r.mu.Unlock()
	return nil
}

func (r *recordingPublisher) snapshot() []ProgressEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ProgressEvent, len(r.events))
	copy(out, r.events)
	return out
}

func setupBatchAsyncRouter(handler *Handler) *chi.Mux {
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/actions/{action}/applyBatch", handler.ApplyBatch)
	r.Get("/api/v2/ontologies/{ontologyApiName}/actions/jobs/{jobId}", handler.GetJob)
	r.Post("/api/v2/ontologies/{ontologyApiName}/actions/jobs/{jobId}/cancel", handler.CancelJob)
	return r
}

// TestHandler_ApplyBatch_AsyncQuery_RunsToCompletion verifies that
// ?async=true on /applyBatch returns 202 with a jobId, executes every action
// in the batch, and emits per-step progress events. US-318.
func TestHandler_ApplyBatch_AsyncQuery_RunsToCompletion(t *testing.T) {
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
	progress := &recordingPublisher{}
	exec.SetProgressPublisher(progress)
	handler := NewHandler(exec)
	router := setupBatchAsyncRouter(handler)

	body := mustJSON(map[string]interface{}{
		"actions": []map[string]interface{}{
			{"parameters": map[string]interface{}{"name": "Alice"}},
			{"parameters": map[string]interface{}{"name": "Bob"}},
			{"parameters": map[string]interface{}{"name": "Carol"}},
			{"parameters": map[string]interface{}{"name": "Dave"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/createEmployee/applyBatch?async=true",
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
		t.Fatal("expected non-empty jobId")
	}

	final := waitForJob(t, store, resp.JobID, time.Second)
	if final.Status != ActionJobStatusSucceeded {
		t.Fatalf("expected SUCCEEDED, got %q (err=%q)", final.Status, final.ErrorMessage)
	}
	if final.Progress != 100 {
		t.Fatalf("expected progress=100, got %d", final.Progress)
	}

	events := progress.snapshot()
	if len(events) == 0 {
		t.Fatal("expected at least one progress event")
	}
	// The terminal event must be 100% with the canonical "done" message.
	last := events[len(events)-1]
	if last.Percent != 100 || last.Message != "done" {
		t.Fatalf("last progress event = (%d%%, %q); want (100, done)", last.Percent, last.Message)
	}
	// Job ID is propagated onto every event.
	for i, e := range events {
		if e.JobID != resp.JobID {
			t.Fatalf("event %d jobId = %q; want %q", i, e.JobID, resp.JobID)
		}
	}
}

// TestHandler_ApplyBatch_AsyncNoStore_DegradesToSync verifies that with no
// ActionJobStore wired the ?async=true query falls through to the sync batch
// path (response = 200 with the regular BatchApplyActionResponseV2).
func TestHandler_ApplyBatch_AsyncNoStore_DegradesToSync(t *testing.T) {
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
	router := setupBatchAsyncRouter(handler)

	body := mustJSON(map[string]interface{}{
		"actions": []map[string]interface{}{
			{"parameters": map[string]interface{}{"name": "Alice"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/createEmployee/applyBatch?async=true",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 degraded-sync response, got %d: %s", w.Code, w.Body.String())
	}
}

// TestHandler_CancelJob_StopsRunner verifies that POST .../jobs/{id}/cancel
// signals the worker, which observes ctx cancellation at the next iteration
// boundary and marks the job CANCELED. The funnel publisher blocks on a
// release channel so the test can cancel BEFORE the worker enters its next
// Apply call, exercising the real ctx-error gate rather than racing the
// goroutine.
func TestHandler_CancelJob_StopsRunner(t *testing.T) {
	pub := newBlockingFunnelPublisher()
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
	exec := NewExecutor(repo, pub)
	store := newMemActionJobStore()
	exec.SetActionJobStore(store)
	handler := NewHandler(exec)
	router := setupBatchAsyncRouter(handler)

	body := mustJSON(map[string]interface{}{
		"actions": []map[string]interface{}{
			{"parameters": map[string]interface{}{"name": "Alice"}},
			{"parameters": map[string]interface{}{"name": "Bob"}},
			{"parameters": map[string]interface{}{"name": "Carol"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/createEmployee/applyBatch?async=true",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	var resp AsyncApplyResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	// Wait until the worker is blocked inside the first Publish call.
	select {
	case <-pub.entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not start applying within deadline")
	}

	// Fire the cancel BEFORE releasing the publisher — the worker is parked
	// on the publisher channel so it can't be racing to the next iteration.
	cancelReq := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/jobs/"+resp.JobID+"/cancel", nil)
	cancelW := httptest.NewRecorder()
	router.ServeHTTP(cancelW, cancelReq)
	if cancelW.Code != http.StatusAccepted {
		t.Fatalf("cancel: expected 202, got %d: %s", cancelW.Code, cancelW.Body.String())
	}

	// Release the in-flight publish so the worker can finish action 1 and
	// hit the ctx.Err() gate at the top of iteration 2 — that's where the
	// CANCELED transition lives.
	close(pub.release)

	final := waitForJobStatus(t, store, resp.JobID, ActionJobStatusCanceled, time.Second)
	if final.Status != ActionJobStatusCanceled {
		t.Fatalf("expected CANCELED, got %q (err=%q)", final.Status, final.ErrorMessage)
	}
}

// TestHandler_CancelJob_NotFound returns 404 for unknown job IDs.
func TestHandler_CancelJob_NotFound(t *testing.T) {
	exec := NewExecutor(&mockOmsRepo{}, nil)
	exec.SetActionJobStore(newMemActionJobStore())
	handler := NewHandler(exec)
	router := setupBatchAsyncRouter(handler)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/jobs/missing/cancel", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestHandler_CancelJob_TerminalConflict rejects cancel attempts on jobs that
// have already completed.
func TestHandler_CancelJob_TerminalConflict(t *testing.T) {
	exec := NewExecutor(&mockOmsRepo{}, nil)
	store := newMemActionJobStore()
	exec.SetActionJobStore(store)
	handler := NewHandler(exec)
	router := setupBatchAsyncRouter(handler)

	// Pre-populate a SUCCEEDED job.
	job := &ActionJob{
		JobID:    "done-1",
		Status:   ActionJobStatusSucceeded,
		Progress: 100,
	}
	if err := store.CreateActionJob(context.Background(), job); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/jobs/done-1/cancel", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

// TestPercentForStep covers the integer division boundaries used by the
// async-batch progress calculator.
func TestPercentForStep(t *testing.T) {
	cases := []struct {
		done, total int
		want        int
	}{
		{0, 4, 0},
		{1, 4, 25},
		{2, 4, 50},
		{3, 4, 75},
		{4, 4, 100},
		{0, 0, 100},
		{99, 100, 99},
		{100, 100, 100},
		// done < total but integer division rounds to 100 — should clamp to 99.
		{199, 200, 99},
	}
	for _, tc := range cases {
		got := percentForStep(tc.done, tc.total)
		if got != tc.want {
			t.Errorf("percentForStep(%d, %d) = %d; want %d", tc.done, tc.total, got, tc.want)
		}
	}
}

// gatedPublisher is a ProgressPublisher that signals on a channel every time
// a publish is observed. Used by cancel tests to wait for worker progress
// without flaky time.Sleeps.
type gatedPublisher struct {
	gate chan<- struct{}
}

func (g *gatedPublisher) PublishProgress(_ string, _ []byte) error {
	select {
	case g.gate <- struct{}{}:
	default:
	}
	return nil
}

// blockingFunnelPublisher implements actions.Publisher (funnel.Publisher
// shape) and BLOCKS on every Publish until release is closed. Used by the
// cancel test to park the worker mid-Apply and ensure the cancel-before-next
// -iteration ordering is deterministic.
type blockingFunnelPublisher struct {
	entered chan struct{} // signalled once on the first Publish call
	release chan struct{} // closed by the test to release every parked Publish
	once    bool
}

func newBlockingFunnelPublisher() *blockingFunnelPublisher {
	return &blockingFunnelPublisher{
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
}

func (b *blockingFunnelPublisher) Publish(_ *funnel.EditBatch) (uint64, error) {
	if !b.once {
		b.once = true
		select {
		case b.entered <- struct{}{}:
		default:
		}
	}
	<-b.release
	return 1, nil
}

// waitForJobStatus polls until the job reaches the requested status or the
// deadline hits. Distinct from waitForJob (which insists on a SUCCEEDED /
// FAILED terminal) because cancel tests target CANCELED specifically.
func waitForJobStatus(t *testing.T, store *memActionJobStore, id, want string, deadline time.Duration) *ActionJob {
	t.Helper()
	start := time.Now()
	for time.Since(start) < deadline {
		j, err := store.GetActionJob(context.Background(), id)
		if err == nil && j.Status == want {
			return j
		}
		time.Sleep(5 * time.Millisecond)
	}
	j, _ := store.GetActionJob(context.Background(), id)
	t.Fatalf("job %s did not reach status %q within %s (last status=%q)", id, want, deadline, func() string {
		if j == nil {
			return "<nil>"
		}
		return j.Status
	}())
	return nil
}
