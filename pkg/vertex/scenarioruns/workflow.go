package scenarioruns

import (
	"context"
	"errors"
	"sort"
	"time"
)

// ActivityExecutor is the per-activity hook the workflow invokes. The
// real wiring lives in cmd/server/main.go and dispatches to either the
// modelmesh executor (model activities) or the function-backed action
// runner (action activities). Returning a non-nil error triggers the
// retry loop; returning context.Canceled / context.DeadlineExceeded
// short-circuits retries and marks the run canceled.
type ActivityExecutor func(ctx context.Context, a Activity) error

// Persister is the slim slice of Repo the Workflow needs. Splitting it
// out keeps the workflow tests stub-friendly — they don't have to
// implement CreateRun / GetRun / ListResumable just to assert that the
// checkpoint progresses correctly.
type Persister interface {
	SaveCheckpoint(ctx context.Context, cp RunCheckpoint) error
}

// Workflow executes a list of activities with per-activity retry +
// cancellation + checkpoint persistence. The zero value is usable;
// Policy.Normalize() is applied per-call so callers can leave the
// field blank for the default 3-attempt retry.
type Workflow struct {
	Policy  RetryPolicy
	Persist Persister
	// Sleep is invoked between retries; injectable so tests can
	// drive timing without real wall-clock waits. Defaults to
	// time.Sleep when nil.
	Sleep func(d time.Duration)
}

// Execute runs activities layer-by-layer. Within a layer, activities
// run sequentially in deterministic ID order — concurrency-within-a-
// layer is a future story (the Model Mesh runner already does it for
// pure-model meshes via pkg/vertex/modelmesh.Runner; mixing actions in
// adds tx + edit-batch interleaving concerns we defer).
//
// Failure semantics: an activity that exhausts its retry budget marks
// the whole run failed; downstream layers do not run. Cancellation via
// ctx.Done short-circuits and marks the run canceled. On success the
// checkpoint is rewritten with Status=Succeeded so a follow-up
// ResumeAll skips it.
func (w *Workflow) Execute(
	ctx context.Context,
	runRID, scenarioRID string,
	activities []Activity,
	exec ActivityExecutor,
) (RunCheckpoint, []ActivityResult, error) {
	if exec == nil {
		return RunCheckpoint{}, nil, errors.New("scenarioruns: executor is required")
	}
	policy := w.Policy.Normalize()
	sleep := w.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}

	cp := RunCheckpoint{
		RunRID:       runRID,
		ScenarioRID:  scenarioRID,
		Status:       RunStatusRunning,
		AttemptsByID: map[string]int{},
		UpdatedAt:    time.Now(),
	}
	w.persistIgnoreErr(ctx, cp)

	layers := groupByLayer(activities)
	var results []ActivityResult

	for _, layer := range layers {
		for _, a := range layer {
			if err := ctx.Err(); err != nil {
				cp.Status = RunStatusCanceled
				cp.Error = err.Error()
				cp.UpdatedAt = time.Now()
				w.persistIgnoreErr(ctx, cp)
				return cp, results, err
			}
			r := w.runActivity(ctx, a, policy, sleep, exec)
			cp.AttemptsByID[a.ID] = r.Attempts
			cp.LastActivity = a.ID
			cp.UpdatedAt = time.Now()
			results = append(results, r)

			if r.Err != nil {
				if errors.Is(r.Err, context.Canceled) || errors.Is(r.Err, context.DeadlineExceeded) {
					cp.Status = RunStatusCanceled
				} else {
					cp.Status = RunStatusFailed
				}
				cp.Error = r.Err.Error()
				w.persistIgnoreErr(ctx, cp)
				return cp, results, r.Err
			}
			cp.Completed = append(cp.Completed, a.ID)
			w.persistIgnoreErr(ctx, cp)
		}
	}

	cp.Status = RunStatusSucceeded
	cp.Error = ""
	cp.UpdatedAt = time.Now()
	w.persistIgnoreErr(ctx, cp)
	return cp, results, nil
}

// runActivity executes one activity with retry. Each attempt observes
// ctx — a cancel mid-attempt propagates as context.Canceled and skips
// any remaining retries. The returned ActivityResult.Err carries the
// final outcome (nil on success, the last attempt's error otherwise).
func (w *Workflow) runActivity(
	ctx context.Context,
	a Activity,
	policy RetryPolicy,
	sleep func(time.Duration),
	exec ActivityExecutor,
) ActivityResult {
	r := ActivityResult{ActivityID: a.ID, StartedAt: time.Now()}
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		r.Attempts = attempt
		if err := ctx.Err(); err != nil {
			r.Err = err
			r.ErrMsg = err.Error()
			r.EndedAt = time.Now()
			return r
		}
		err := exec(ctx, a)
		if err == nil {
			r.Err = nil
			r.ErrMsg = ""
			r.EndedAt = time.Now()
			return r
		}
		r.Err = err
		r.ErrMsg = err.Error()
		// Cancellation or deadline propagates immediately; no point
		// burning the rest of the retry budget against a dead ctx.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			r.EndedAt = time.Now()
			return r
		}
		if attempt < policy.MaxAttempts && policy.BackoffMs > 0 {
			sleep(time.Duration(policy.BackoffMs) * time.Millisecond)
		}
	}
	r.EndedAt = time.Now()
	return r
}

// groupByLayer slices activities into layers in ascending Layer order.
// Within each layer, activities are sorted by ID for deterministic
// execution order (matches modelmesh.TopologicalLayers).
func groupByLayer(activities []Activity) [][]Activity {
	if len(activities) == 0 {
		return nil
	}
	byLayer := map[int][]Activity{}
	for _, a := range activities {
		byLayer[a.Layer] = append(byLayer[a.Layer], a)
	}
	keys := make([]int, 0, len(byLayer))
	for k := range byLayer {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	out := make([][]Activity, 0, len(keys))
	for _, k := range keys {
		layer := byLayer[k]
		sort.Slice(layer, func(i, j int) bool { return layer[i].ID < layer[j].ID })
		out = append(out, layer)
	}
	return out
}

func (w *Workflow) persistIgnoreErr(ctx context.Context, cp RunCheckpoint) {
	if w.Persist == nil {
		return
	}
	// We deliberately swallow the persist error: the caller already
	// has the in-memory checkpoint. A persist failure means the resume
	// path may re-run an activity, which is acceptable for the v1
	// idempotent-activity contract; logging is the wiring layer's job.
	_ = w.Persist.SaveCheckpoint(ctx, cp)
}
