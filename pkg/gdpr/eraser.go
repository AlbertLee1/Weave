package gdpr

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
)

// Step is one unit of erase work. Steps are independent: a failure in
// one step is recorded in StepResult.ErrorMessage and the orchestrator
// MOVES ON to the next one. This is deliberate — partial erasure is
// strictly better than no erasure (legal compliance > clean state).
//
// Erase MUST be idempotent: callers may retry an aborted job, in which
// case rows already removed by a prior run should silently be no-ops.
type Step interface {
	Name() string
	Erase(ctx context.Context, userID string) (rowsAffected int, err error)
}

// StepFunc adapts a function into a Step. The name is captured at
// construction time.
type StepFunc struct {
	StepName string
	Fn       func(ctx context.Context, userID string) (int, error)
}

func (s StepFunc) Name() string { return s.StepName }
func (s StepFunc) Erase(ctx context.Context, userID string) (int, error) {
	return s.Fn(ctx, userID)
}

// Eraser orchestrates a sequence of Steps for a single user.
//
// Run is the entry point invoked by the detached goroutine. It walks
// the registered Steps in order, persisting per-step progress so
// pollers see forward motion. The job is marked SUCCEEDED iff EVERY
// step succeeded; any failure marks it FAILED with the first error
// message — but all subsequent steps still run so partial cleanup
// happens.
type Eraser struct {
	store   JobStore
	steps   []Step
	nowFunc func() time.Time
}

// NewEraser returns an Eraser that walks the supplied steps in order.
// store is used to persist per-step progress; supply nil to run "blind"
// (still walks every step, returns the result inline — useful for
// CLI / one-shot use cases).
func NewEraser(store JobStore, steps []Step) *Eraser {
	return &Eraser{
		store:   store,
		steps:   steps,
		nowFunc: time.Now,
	}
}

// SetNowFunc overrides the clock for deterministic tests. Mirrors the
// convention used by oms.CachedRepository.nowFunc and the audit
// rolling-window limiter.
func (e *Eraser) SetNowFunc(fn func() time.Time) {
	if fn != nil {
		e.nowFunc = fn
	}
}

// Steps returns the registered steps in declaration order. Test helper.
func (e *Eraser) Steps() []Step { return e.steps }

// Run executes every Step in order against userID and updates the job
// row at jobID. Returns the final job state. ctx cancellation is
// observed BETWEEN steps — a step that has already started will run to
// completion.
func (e *Eraser) Run(ctx context.Context, jobID, userID string) (*ErasureJob, error) {
	if userID == "" {
		return nil, errors.New("gdpr: userID is required")
	}
	// Mark RUNNING with a small initial progress nudge so SDK pollers
	// see the worker pick up the job. Mirrors actions.runAsyncApply.
	if e.store != nil {
		startProg := 0
		if err := e.store.UpdateJob(ctx, jobID, JobUpdate{
			Status:   JobStatusRunning,
			Progress: &startProg,
			Steps:    []StepResult{},
		}); err != nil {
			log.Printf("gdpr: job %s: mark RUNNING failed: %v", jobID, err)
		}
	}

	results := make([]StepResult, 0, len(e.steps))
	var firstErr string

	for i, step := range e.steps {
		// Honour ctx cancellation between steps; a step in flight will
		// finish naturally.
		if err := ctx.Err(); err != nil {
			res := StepResult{
				Name:         step.Name(),
				ErrorMessage: fmt.Sprintf("aborted: %v", err),
			}
			results = append(results, res)
			if firstErr == "" {
				firstErr = res.ErrorMessage
			}
			continue
		}

		start := e.nowFunc()
		rows, stepErr := step.Erase(ctx, userID)
		dur := e.nowFunc().Sub(start)
		res := StepResult{
			Name:         step.Name(),
			RowsAffected: rows,
			DurationMs:   dur.Milliseconds(),
		}
		if stepErr != nil {
			res.ErrorMessage = stepErr.Error()
			if firstErr == "" {
				firstErr = res.ErrorMessage
			}
		}
		results = append(results, res)

		// Persist progress after each step so pollers see the bar move.
		if e.store != nil {
			prog := percent(i+1, len(e.steps))
			stepsCopy := make([]StepResult, len(results))
			copy(stepsCopy, results)
			if err := e.store.UpdateJob(ctx, jobID, JobUpdate{
				Progress: &prog,
				Steps:    stepsCopy,
			}); err != nil {
				log.Printf("gdpr: job %s: progress write failed: %v", jobID, err)
			}
		}
	}

	finalStatus := JobStatusSucceeded
	if firstErr != "" {
		finalStatus = JobStatusFailed
	}
	doneProg := 100
	finalErr := firstErr
	job := &ErasureJob{
		JobID:        jobID,
		UserID:       userID,
		Status:       finalStatus,
		Progress:     doneProg,
		Steps:        results,
		ErrorMessage: finalErr,
		RequestedBy:  e.lookupRequestedBy(ctx, jobID),
		UpdatedAt:    e.nowFunc(),
	}
	// US-443: stamp a deterministic proof-of-erasure hash on the job
	// row so auditors can later verify the recorded outcome without
	// re-fetching every step result. Computed AFTER all per-step
	// fields are finalised so the hash commits to the terminal state.
	job.ProofHash = ComputeProofHash(BuildProofPayload(job))
	if e.store != nil {
		proofHash := job.ProofHash
		if err := e.store.UpdateJob(ctx, jobID, JobUpdate{
			Status:       finalStatus,
			Progress:     &doneProg,
			Steps:        results,
			ErrorMessage: &finalErr,
			ProofHash:    &proofHash,
		}); err != nil {
			log.Printf("gdpr: job %s: terminal write failed: %v", jobID, err)
		}
	}
	return job, nil
}

// lookupRequestedBy resolves the RequestedBy actor for jobID by reading
// the job row out of the store. Returning an empty string when the
// store is absent or the row can't be read is intentional — the proof
// hash already commits to whatever value lands here, so a missing
// requester just means the proof excludes that field rather than
// surfacing a hash mismatch.
func (e *Eraser) lookupRequestedBy(ctx context.Context, jobID string) string {
	if e.store == nil {
		return ""
	}
	j, err := e.store.GetJob(ctx, jobID)
	if err != nil || j == nil {
		return ""
	}
	return j.RequestedBy
}

// percent computes 0..100 step-progress, rounded down. Always returns
// 100 for the last step so the job row's progress matches the terminal
// write (avoids "99% then 100%" flicker).
func percent(done, total int) int {
	if total <= 0 {
		return 100
	}
	if done >= total {
		return 100
	}
	return (done * 100) / total
}
