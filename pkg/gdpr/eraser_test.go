package gdpr

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestEraser_RunSucceedsWhenAllStepsSucceed(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryJobStore()
	job := &ErasureJob{JobID: "job-1", UserID: "user:alice", Status: JobStatusPending}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}

	steps := []Step{
		StepFunc{StepName: "s1", Fn: func(_ context.Context, uid string) (int, error) {
			if uid != "user:alice" {
				t.Errorf("unexpected userID: %q", uid)
			}
			return 3, nil
		}},
		StepFunc{StepName: "s2", Fn: func(context.Context, string) (int, error) {
			return 1, nil
		}},
	}
	e := NewEraser(store, steps)

	final, err := e.Run(ctx, "job-1", "user:alice")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if final.Status != JobStatusSucceeded {
		t.Errorf("status = %s, want SUCCEEDED", final.Status)
	}
	if final.Progress != 100 {
		t.Errorf("progress = %d, want 100", final.Progress)
	}
	if len(final.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(final.Steps))
	}
	if final.Steps[0].RowsAffected != 3 || final.Steps[1].RowsAffected != 1 {
		t.Errorf("rows affected wrong: %#v", final.Steps)
	}
	if final.ErrorMessage != "" {
		t.Errorf("err msg = %q, want empty", final.ErrorMessage)
	}

	stored, err := store.GetJob(ctx, "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != JobStatusSucceeded || stored.Progress != 100 {
		t.Errorf("stored job didn't match: %#v", stored)
	}
	if len(stored.Steps) != 2 {
		t.Errorf("stored steps len=%d", len(stored.Steps))
	}
}

func TestEraser_RunRecordsFailureButContinues(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryJobStore()
	if err := store.CreateJob(ctx, &ErasureJob{JobID: "j2", UserID: "u", Status: JobStatusPending}); err != nil {
		t.Fatal(err)
	}

	called := []string{}
	steps := []Step{
		StepFunc{StepName: "ok-1", Fn: func(context.Context, string) (int, error) {
			called = append(called, "ok-1")
			return 1, nil
		}},
		StepFunc{StepName: "fail", Fn: func(context.Context, string) (int, error) {
			called = append(called, "fail")
			return 0, errors.New("kaboom")
		}},
		StepFunc{StepName: "ok-2", Fn: func(context.Context, string) (int, error) {
			called = append(called, "ok-2")
			return 7, nil
		}},
	}
	e := NewEraser(store, steps)

	final, err := e.Run(ctx, "j2", "u")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := called, []string{"ok-1", "fail", "ok-2"}; !equal(got, want) {
		t.Errorf("call order wrong: got %v want %v", got, want)
	}
	if final.Status != JobStatusFailed {
		t.Errorf("status = %s, want FAILED", final.Status)
	}
	if !strings.Contains(final.ErrorMessage, "kaboom") {
		t.Errorf("error message = %q, want kaboom", final.ErrorMessage)
	}
	if final.Steps[2].RowsAffected != 7 {
		t.Errorf("ok-2 step did not run: %#v", final.Steps[2])
	}
}

func TestEraser_RunRejectsEmptyUserID(t *testing.T) {
	e := NewEraser(NewMemoryJobStore(), nil)
	_, err := e.Run(context.Background(), "j", "")
	if err == nil {
		t.Fatal("expected error on empty userID")
	}
}

func TestEraser_RunHonoursContextCancellationBetweenSteps(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := NewMemoryJobStore()
	if err := store.CreateJob(ctx, &ErasureJob{JobID: "j3", UserID: "u", Status: JobStatusPending}); err != nil {
		t.Fatal(err)
	}

	called := []string{}
	steps := []Step{
		StepFunc{StepName: "first", Fn: func(_ context.Context, _ string) (int, error) {
			called = append(called, "first")
			cancel()
			return 1, nil
		}},
		StepFunc{StepName: "second", Fn: func(context.Context, string) (int, error) {
			called = append(called, "second")
			return 2, nil
		}},
	}
	e := NewEraser(store, steps)

	final, _ := e.Run(ctx, "j3", "u")
	if got := called; len(got) != 1 || got[0] != "first" {
		t.Errorf("expected only first to run, got %v", got)
	}
	if final.Status != JobStatusFailed {
		t.Errorf("expected FAILED after cancellation, got %s", final.Status)
	}
	if final.Steps[1].ErrorMessage == "" {
		t.Errorf("expected aborted message on second step, got %#v", final.Steps[1])
	}
}

func TestEraser_PersistsProgressAfterEachStep(t *testing.T) {
	// Verify that the progress bar advances incrementally — operators
	// polling mid-run should see step-by-step movement, not a single
	// 0→100 jump at the end.
	ctx := context.Background()
	store := NewMemoryJobStore()
	if err := store.CreateJob(ctx, &ErasureJob{JobID: "j4", UserID: "u", Status: JobStatusPending}); err != nil {
		t.Fatal(err)
	}

	// Capture progress after each step using a JobStore wrapper.
	var observed []int
	wrap := &recordingStore{inner: store, onUpdate: func(upd JobUpdate) {
		if upd.Progress != nil {
			observed = append(observed, *upd.Progress)
		}
	}}

	steps := []Step{
		StepFunc{StepName: "a", Fn: func(context.Context, string) (int, error) { return 0, nil }},
		StepFunc{StepName: "b", Fn: func(context.Context, string) (int, error) { return 0, nil }},
		StepFunc{StepName: "c", Fn: func(context.Context, string) (int, error) { return 0, nil }},
		StepFunc{StepName: "d", Fn: func(context.Context, string) (int, error) { return 0, nil }},
	}
	e := NewEraser(wrap, steps)
	if _, err := e.Run(ctx, "j4", "u"); err != nil {
		t.Fatal(err)
	}

	// Expect progressions: 0 (initial RUNNING), 25, 50, 75, 100, 100 (terminal).
	want := []int{0, 25, 50, 75, 100, 100}
	if !equalInt(observed, want) {
		t.Errorf("progress sequence: got %v want %v", observed, want)
	}
}

func TestEraser_ClockInjectionDeterministic(t *testing.T) {
	store := NewMemoryJobStore()
	if err := store.CreateJob(context.Background(), &ErasureJob{JobID: "j5", UserID: "u", Status: JobStatusPending}); err != nil {
		t.Fatal(err)
	}
	tick := time.Unix(1_000_000, 0)
	advance := time.Duration(0)
	steps := []Step{
		StepFunc{StepName: "x", Fn: func(context.Context, string) (int, error) { return 0, nil }},
	}
	e := NewEraser(store, steps)
	e.SetNowFunc(func() time.Time {
		t := tick.Add(advance)
		advance += 25 * time.Millisecond
		return t
	})
	final, err := e.Run(context.Background(), "j5", "u")
	if err != nil {
		t.Fatal(err)
	}
	if final.Steps[0].DurationMs != 25 {
		t.Errorf("step duration = %dms, want 25ms", final.Steps[0].DurationMs)
	}
}

// recordingStore wraps a JobStore so test code can observe the partial
// updates the orchestrator emits between steps.
type recordingStore struct {
	inner    JobStore
	onUpdate func(JobUpdate)
}

func (r *recordingStore) CreateJob(ctx context.Context, j *ErasureJob) error {
	return r.inner.CreateJob(ctx, j)
}
func (r *recordingStore) GetJob(ctx context.Context, id string) (*ErasureJob, error) {
	return r.inner.GetJob(ctx, id)
}
func (r *recordingStore) UpdateJob(ctx context.Context, id string, upd JobUpdate) error {
	if r.onUpdate != nil {
		r.onUpdate(upd)
	}
	return r.inner.UpdateJob(ctx, id, upd)
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalInt(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
