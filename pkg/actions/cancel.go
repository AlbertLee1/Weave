package actions

import (
	"context"
	"sync"
	"time"
)

// jobCancelRegistry tracks in-flight async jobs so a remote
// POST .../actions/jobs/{jobId}/cancel can signal the runner to stop. US-318.
//
// Sync.Map is intentional — Register is called once per detached worker (low
// contention), Cancel is rare, and the runner's Unregister fires on every
// completion. The map keeps the cancel func alive only for the lifetime of
// the goroutine, so leaks are bounded by in-flight jobs.
type jobCancelRegistry struct {
	cancels sync.Map // jobID string → context.CancelFunc
}

// register stamps a cancel func for jobID. Must be paired with unregister via
// defer in the runner so a panic in the worker still releases the entry.
func (r *jobCancelRegistry) register(jobID string, cancel context.CancelFunc) {
	if jobID == "" || cancel == nil {
		return
	}
	r.cancels.Store(jobID, cancel)
}

// unregister drops the cancel func for jobID. Safe to call for unknown IDs.
func (r *jobCancelRegistry) unregister(jobID string) {
	if jobID == "" {
		return
	}
	r.cancels.Delete(jobID)
}

// cancel fires the cancel func for jobID and removes it from the registry.
// Returns true when a runner was signalled, false when no in-flight job
// exists for that ID (already finished, never started, or wrong host in a
// future multi-replica setup).
func (r *jobCancelRegistry) cancel(jobID string) bool {
	if jobID == "" {
		return false
	}
	v, ok := r.cancels.LoadAndDelete(jobID)
	if !ok {
		return false
	}
	if cancel, ok := v.(context.CancelFunc); ok && cancel != nil {
		cancel()
		return true
	}
	return false
}

// RegisterJobCancel binds a context.CancelFunc to a jobID so an inbound
// cancel request can interrupt the worker. Pair with UnregisterJobCancel via
// defer in the worker. US-318.
func (e *Executor) RegisterJobCancel(jobID string, cancel context.CancelFunc) {
	e.cancelRegistry.register(jobID, cancel)
}

// UnregisterJobCancel removes the binding installed by RegisterJobCancel.
// Safe to call for unknown jobIDs.
func (e *Executor) UnregisterJobCancel(jobID string) {
	e.cancelRegistry.unregister(jobID)
}

// CancelJob signals the in-flight async runner for jobID to stop. Returns
// true when a runner was signalled. The runner is expected to mark the job
// CANCELED in its own time; this call does not block on the persisted state.
func (e *Executor) CancelJob(jobID string) bool {
	return e.cancelRegistry.cancel(jobID)
}

// RunActionJobReaperLoop drives ReapOldActionJobs on a fixed interval until
// ctx is cancelled. Terminal-state rows (SUCCEEDED / FAILED / CANCELED) older
// than `retention` are dropped each tick. Failures are reported through
// `onError` (non-blocking) so a transient PG hiccup doesn't kill the loop.
// Per-tick reap counts go to `onReap` for observability when set. US-426.
//
// Modelled on PGSavedStore.RunReaperLoop (US-365) — same shape so operators
// reading /metrics or logs see consistent telemetry across the codebase.
func RunActionJobReaperLoop(ctx context.Context, store ActionJobStore, interval, retention time.Duration, onReap func(int64), onError func(error)) {
	if store == nil || interval <= 0 || retention <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := store.ReapOldActionJobs(ctx, time.Now().Add(-retention))
			if err != nil {
				if onError != nil {
					onError(err)
				}
				continue
			}
			if n > 0 && onReap != nil {
				onReap(n)
			}
		}
	}
}
