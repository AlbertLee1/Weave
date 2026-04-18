package actions

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/liyang/weave/pkg/functions"
)

// ProgressPublisher is the narrow contract the async apply path uses to
// broadcast progress events over NATS. US-241.
//
// Kept separate from the JetStream funnel.Publisher on purpose — progress
// events are ephemeral (no replay semantics, no ordering guarantees) and
// should not land on OBJECT_EDITS. Concrete implementation is a plain
// `nats.Conn.Publish(subject, data)` wrapper that lives in cmd/server so
// pkg/actions stays free of a direct nats-io dependency.
type ProgressPublisher interface {
	PublishProgress(subject string, data []byte) error
}

// SetProgressPublisher attaches the NATS progress broadcaster used by the
// async apply path to fan out `weave.reportProgress` updates. When nil the
// executor still persists progress to ActionJobStore but skips the NATS
// publish — degraded-mode wiring (no NATS) keeps tests deterministic. Safe
// to call once at boot before the executor is shared with handlers.
func (e *Executor) SetProgressPublisher(p ProgressPublisher) {
	e.progressPub = p
}

// ProgressPublisher returns the wired progress broadcaster. Exported so the
// handler can detect "no publisher wired" without reaching into internals.
func (e *Executor) ProgressPublisher() ProgressPublisher {
	return e.progressPub
}

// ProgressEvent is the wire shape emitted to NATS every time a long-running
// action calls `weave.reportProgress(percent, message)`. JobID ties the event
// back to the action_jobs row so SDK consumers can correlate ephemeral NATS
// updates with the persisted polling endpoint.
type ProgressEvent struct {
	JobID      string    `json:"jobId"`
	Ontology   string    `json:"ontologyApiName"`
	ActionType string    `json:"actionType"`
	Percent    int       `json:"percent"`
	Message    string    `json:"message,omitempty"`
	ReportedAt time.Time `json:"reportedAt"`
}

// ProgressSubject returns the NATS subject a given jobID publishes progress on.
// Format: `actions.progress.<jobId>`. SDKs subscribe on this subject to
// receive live updates; the polling endpoint (GET /actions/jobs/{id}) is
// still the source of truth for the persisted state.
func ProgressSubject(jobID string) string {
	return "actions.progress." + jobID
}

// jobProgressReporter implements functions.ProgressReporter by forwarding
// every `weave.reportProgress` call to (a) the ActionJobStore so polling
// clients see forward motion and (b) the NATS progress broadcaster so
// subscribers see the event live. Both targets are best-effort — a failing
// store or publisher logs and moves on rather than failing the JS call.
type jobProgressReporter struct {
	store      ActionJobStore
	publisher  ProgressPublisher
	jobID      string
	ontology   string
	actionType string
	nowFunc    func() time.Time
}

// newJobProgressReporter builds the reporter wired into the async apply path.
// The publisher may be nil (NATS broadcast skipped silently), but the store
// is required — the whole point of US-241 is to surface progress to pollers.
func newJobProgressReporter(store ActionJobStore, publisher ProgressPublisher, jobID, ontology, actionType string) *jobProgressReporter {
	return &jobProgressReporter{
		store:      store,
		publisher:  publisher,
		jobID:      jobID,
		ontology:   ontology,
		actionType: actionType,
		nowFunc:    time.Now,
	}
}

// Report satisfies functions.ProgressReporter. Writes the new progress value
// into the action_jobs row (via UpdateActionJob with Progress pointer so the
// three-state semantics honours "unchanged" on unrelated fields) and fires
// a NATS event. Neither failure path is propagated back to JS — the goal is
// "tell the world how far along you are", not "gate execution on telemetry".
func (r *jobProgressReporter) Report(ctx context.Context, percent int, message string) {
	if r == nil {
		return
	}
	if r.store != nil {
		p := percent
		if err := r.store.UpdateActionJob(ctx, r.jobID, ActionJobUpdate{
			Status:   ActionJobStatusRunning,
			Progress: &p,
		}); err != nil {
			log.Printf("actions: progress reporter: job %s store update failed: %v", r.jobID, err)
		}
	}
	if r.publisher != nil {
		evt := ProgressEvent{
			JobID:      r.jobID,
			Ontology:   r.ontology,
			ActionType: r.actionType,
			Percent:    percent,
			Message:    message,
			ReportedAt: r.nowFunc(),
		}
		data, err := json.Marshal(&evt)
		if err != nil {
			log.Printf("actions: progress reporter: job %s marshal failed: %v", r.jobID, err)
			return
		}
		if err := r.publisher.PublishProgress(ProgressSubject(r.jobID), data); err != nil {
			log.Printf("actions: progress reporter: job %s publish failed: %v", r.jobID, err)
		}
	}
}

// compile-time interface assertion — jobProgressReporter is the executor's
// bridge to pkg/functions' goja-shim contract. If the interface drifts this
// line breaks the build before runtime does.
var _ functions.ProgressReporter = (*jobProgressReporter)(nil)
