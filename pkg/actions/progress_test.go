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

	"github.com/liyang/weave/pkg/functions"
	"github.com/liyang/weave/pkg/oms"
)

// ---------------------------------------------------------------------------
// Unit tests: jobProgressReporter + ProgressSubject
// ---------------------------------------------------------------------------

// captureProgressPublisher records PublishProgress calls.
type captureProgressPublisher struct {
	mu    sync.Mutex
	calls []struct {
		subject string
		event   ProgressEvent
	}
	err error
}

func (p *captureProgressPublisher) PublishProgress(subject string, data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	var evt ProgressEvent
	_ = json.Unmarshal(data, &evt)
	p.calls = append(p.calls, struct {
		subject string
		event   ProgressEvent
	}{subject: subject, event: evt})
	return p.err
}

func (p *captureProgressPublisher) snapshot() []struct {
	subject string
	event   ProgressEvent
} {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]struct {
		subject string
		event   ProgressEvent
	}, len(p.calls))
	copy(out, p.calls)
	return out
}

func TestProgressSubject_Format(t *testing.T) {
	if got := ProgressSubject("abc-123"); got != "actions.progress.abc-123" {
		t.Fatalf("subject format drift: got %q", got)
	}
}

// TestJobProgressReporter_UpdatesStoreAndPublishes verifies the reporter
// writes into ActionJobStore AND fans out a NATS event when both are wired.
func TestJobProgressReporter_UpdatesStoreAndPublishes(t *testing.T) {
	store := newMemActionJobStore()
	_ = store.CreateActionJob(context.Background(), &ActionJob{
		JobID:          "job-1",
		OntologyAPI:    "ont-1",
		ActionTypeName: "longRunning",
		Status:         ActionJobStatusRunning,
		CreatedAt:      time.Now(),
	})
	pub := &captureProgressPublisher{}

	r := newJobProgressReporter(store, pub, "job-1", "ont-1", "longRunning")
	r.Report(context.Background(), 42, "halfway")

	got, err := store.GetActionJob(context.Background(), "job-1")
	if err != nil {
		t.Fatalf("GetActionJob: %v", err)
	}
	if got.Progress != 42 {
		t.Fatalf("expected progress=42 in store, got %d", got.Progress)
	}
	if got.Status != ActionJobStatusRunning {
		t.Fatalf("expected status=RUNNING, got %q", got.Status)
	}

	calls := pub.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected 1 publish, got %d", len(calls))
	}
	if calls[0].subject != "actions.progress.job-1" {
		t.Fatalf("subject: got %q", calls[0].subject)
	}
	if calls[0].event.Percent != 42 || calls[0].event.Message != "halfway" {
		t.Fatalf("event payload: %+v", calls[0].event)
	}
	if calls[0].event.JobID != "job-1" || calls[0].event.Ontology != "ont-1" || calls[0].event.ActionType != "longRunning" {
		t.Fatalf("event metadata: %+v", calls[0].event)
	}
	if calls[0].event.ReportedAt.IsZero() {
		t.Fatal("expected non-zero ReportedAt")
	}
}

// TestJobProgressReporter_PublisherFailureDoesNotPropagate — a misbehaving
// publisher should not make the reporter panic or error; the store update
// must still succeed. Matches the "telemetry is best effort" contract.
func TestJobProgressReporter_PublisherFailureDoesNotPropagate(t *testing.T) {
	store := newMemActionJobStore()
	_ = store.CreateActionJob(context.Background(), &ActionJob{JobID: "job-x", CreatedAt: time.Now()})
	pub := &captureProgressPublisher{err: errors.New("nats down")}

	r := newJobProgressReporter(store, pub, "job-x", "ont", "act")
	// Should not panic.
	r.Report(context.Background(), 10, "one")

	got, _ := store.GetActionJob(context.Background(), "job-x")
	if got == nil || got.Progress != 10 {
		t.Fatalf("expected progress=10 despite publisher error, got %+v", got)
	}
}

// TestJobProgressReporter_NilPublisherStillUpdatesStore verifies degraded
// mode (no NATS wiring): the store path must still run.
func TestJobProgressReporter_NilPublisherStillUpdatesStore(t *testing.T) {
	store := newMemActionJobStore()
	_ = store.CreateActionJob(context.Background(), &ActionJob{JobID: "job-y", CreatedAt: time.Now()})

	r := newJobProgressReporter(store, nil, "job-y", "ont", "act")
	r.Report(context.Background(), 55, "more than half")

	got, _ := store.GetActionJob(context.Background(), "job-y")
	if got == nil || got.Progress != 55 {
		t.Fatalf("expected progress=55 with nil publisher, got %+v", got)
	}
}

// ---------------------------------------------------------------------------
// End-to-end async apply: JS `weave.reportProgress` flows into action_jobs
// ---------------------------------------------------------------------------

// TestHandler_Apply_AsyncProgress_FlowsFromGoja wires a function-backed
// action whose JS body calls weave.reportProgress(50, "..."). After the
// async job completes the store must carry the intermediate progress
// (captured via the publisher snapshot) and the terminal SUCCEEDED/100.
func TestHandler_Apply_AsyncProgress_FlowsFromGoja(t *testing.T) {
	fnRID := "ri.ontology.main.function.slow-job"
	lookup := &mockFunctionLookup{
		fn: &oms.Function{
			RID:  fnRID,
			Name: "slowJob",
			SourceCode: `function main(input) {
				weave.reportProgress(25, "stage 1");
				weave.reportProgress(75, "stage 2");
				return {
					edits: [{
						type: "CREATE",
						objectType: "Report",
						primaryKey: "rep-1",
						properties: {name: input.parameters.name}
					}]
				};
			}`,
		},
	}
	at := oms.ActionType{
		RID:              "ri.ontology.main.actionType.slowJob",
		APIName:          "slowJob",
		FunctionRID:      fnRID,
		IsFunctionBacked: true,
		Parameters: mustMarshal([]ParameterDef{
			{ID: "name", Type: "string", Required: true},
		}),
	}
	repo := &mockOmsRepo{actionTypes: []oms.ActionType{at}}

	rt := functions.NewRuntime(functions.DefaultConfig())
	dispatcher := NewGojaDispatcher(rt, lookup)

	pub := &fakePublisher{offset: 1}
	exec := NewExecutor(repo, pub)
	exec.SetFunctionDispatcher(dispatcher)

	store := newMemActionJobStore()
	exec.SetActionJobStore(store)

	progressPub := &captureProgressPublisher{}
	exec.SetProgressPublisher(progressPub)

	handler := NewHandler(exec)
	router := setupAsyncRouter(handler)

	body := mustJSON(map[string]interface{}{
		"parameters": map[string]interface{}{"name": "r1"},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/slowJob/apply?async=true",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	var async AsyncApplyResponse
	_ = json.Unmarshal(w.Body.Bytes(), &async)

	// Wait for terminal state.
	final := waitForJob(t, store, async.JobID, 2*time.Second)
	if final.Status != ActionJobStatusSucceeded {
		t.Fatalf("expected SUCCEEDED, got %q (err=%q)", final.Status, final.ErrorMessage)
	}
	if final.Progress != 100 {
		t.Fatalf("expected terminal progress=100, got %d", final.Progress)
	}

	// NATS side: exactly two progress events fanned out, in order.
	calls := progressPub.snapshot()
	if len(calls) != 2 {
		t.Fatalf("expected 2 NATS progress events, got %d: %+v", len(calls), calls)
	}
	if calls[0].event.Percent != 25 || calls[0].event.Message != "stage 1" {
		t.Fatalf("event 0 drift: %+v", calls[0].event)
	}
	if calls[1].event.Percent != 75 || calls[1].event.Message != "stage 2" {
		t.Fatalf("event 1 drift: %+v", calls[1].event)
	}
	expectSubject := ProgressSubject(async.JobID)
	if calls[0].subject != expectSubject || calls[1].subject != expectSubject {
		t.Fatalf("subject drift: got %q and %q, want %q", calls[0].subject, calls[1].subject, expectSubject)
	}
	if calls[0].event.JobID != async.JobID || calls[0].event.Ontology != "ont-1" || calls[0].event.ActionType != "slowJob" {
		t.Fatalf("event envelope drift: %+v", calls[0].event)
	}
}

// mustMarshal marshals v for ActionType.Parameters helper.
func mustMarshal(v interface{}) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
