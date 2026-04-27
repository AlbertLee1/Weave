package pipeline

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// trace pipeline factory shared by runner tests.
//
//	in  ─► t1 ─►
//	            join ─► sink
//	in2 ─► t2 ─►
func dagPipeline() *Pipeline {
	return &Pipeline{
		ID:   "p",
		Name: "p",
		Inputs: []Input{
			{Name: "in1", Type: "objectset"},
			{Name: "in2", Type: "objectset"},
		},
		Transforms: []Transform{
			{Name: "t1", Type: "filter", Inputs: []string{"in1"}},
			{Name: "t2", Type: "filter", Inputs: []string{"in2"}},
			{Name: "join", Type: "join", Inputs: []string{"t1", "t2"}},
		},
		Outputs: []Output{
			{Name: "sink", Type: "jdbc", Input: "join"},
		},
	}
}

// recordingRunner captures execution order + per-node attempt counts.
type recordingRunner struct {
	mu       sync.Mutex
	order    []string
	attempts map[string]int
	fn       func(ctx context.Context, node DAGNode, attempt int) error
}

func newRecordingRunner(fn func(ctx context.Context, node DAGNode, attempt int) error) *recordingRunner {
	return &recordingRunner{attempts: map[string]int{}, fn: fn}
}

func (r *recordingRunner) Run(ctx context.Context, node DAGNode, attempt int) error {
	r.mu.Lock()
	r.attempts[node.Name]++
	if attempt == 1 {
		r.order = append(r.order, node.Name)
	}
	r.mu.Unlock()
	if r.fn != nil {
		return r.fn(ctx, node, attempt)
	}
	return nil
}

func TestRunDAG_HappyPath(t *testing.T) {
	p := dagPipeline()
	rec := newRecordingRunner(nil)
	res, err := RunDAG(context.Background(), p, RunOptions{Runner: rec})
	if err != nil {
		t.Fatalf("RunDAG: %v", err)
	}
	if res.Status != "success" {
		t.Fatalf("Status = %q, want success", res.Status)
	}
	for _, name := range []string{"in1", "in2", "t1", "t2", "join", "sink"} {
		nr := res.Nodes[name]
		if nr == nil {
			t.Fatalf("missing node result for %q", name)
		}
		if nr.Status != NodeStatusSuccess {
			t.Errorf("%q.Status = %q, want success", name, nr.Status)
		}
		if nr.Attempts != 1 {
			t.Errorf("%q.Attempts = %d, want 1", name, nr.Attempts)
		}
	}
	pos := map[string]int{}
	for i, n := range rec.order {
		pos[n] = i
	}
	if pos["join"] < pos["t1"] || pos["join"] < pos["t2"] {
		t.Fatalf("join must be after t1+t2: %v", rec.order)
	}
	if pos["sink"] < pos["join"] {
		t.Fatalf("sink must be after join: %v", rec.order)
	}
}

func TestRunDAG_AbortOnFailure(t *testing.T) {
	p := dagPipeline()
	failErr := errors.New("boom")
	rec := newRecordingRunner(func(ctx context.Context, node DAGNode, attempt int) error {
		if node.Name == "t1" {
			return failErr
		}
		return nil
	})
	res, err := RunDAG(context.Background(), p, RunOptions{
		Runner:        rec,
		FailurePolicy: FailurePolicyAbort,
		Parallelism:   1,
	})
	if err == nil {
		t.Fatal("expected non-nil error on abort")
	}
	if !errors.Is(err, failErr) {
		t.Fatalf("expected error to wrap failErr, got %v", err)
	}
	if res.Status != "failed" {
		t.Fatalf("Status = %q, want failed", res.Status)
	}
	if got := res.Nodes["t1"].Status; got != NodeStatusFailed {
		t.Fatalf("t1.Status = %q, want failed", got)
	}
	// join + sink should NOT have run: marked canceled (or skipped).
	for _, name := range []string{"join", "sink"} {
		s := res.Nodes[name].Status
		if s == NodeStatusSuccess || s == NodeStatusFailed {
			t.Errorf("%q.Status = %q under abort, expected canceled/skipped", name, s)
		}
	}
}

func TestRunDAG_ContinueOnFailure_SiblingBranchSucceeds(t *testing.T) {
	// Two parallel branches feeding a shared join. Continue policy should
	// let the healthy branch finish while the failed branch + its
	// downstream get marked skipped.
	p := dagPipeline()
	rec := newRecordingRunner(func(ctx context.Context, node DAGNode, attempt int) error {
		if node.Name == "t1" {
			return errors.New("branch 1 failed")
		}
		return nil
	})
	res, err := RunDAG(context.Background(), p, RunOptions{
		Runner:        rec,
		FailurePolicy: FailurePolicyContinue,
		Parallelism:   2,
	})
	if err == nil {
		t.Fatal("RunDAG must surface an error when a node fails (even on continue)")
	}
	if res.Status != "failed" {
		t.Fatalf("Status = %q, want failed", res.Status)
	}
	if got := res.Nodes["t1"].Status; got != NodeStatusFailed {
		t.Fatalf("t1.Status = %q, want failed", got)
	}
	// t2 is independent of t1 → must succeed under continue.
	if got := res.Nodes["t2"].Status; got != NodeStatusSuccess {
		t.Fatalf("t2.Status = %q, want success (independent branch)", got)
	}
	// join depends on t1 (transitively) → must be skipped.
	if got := res.Nodes["join"].Status; got != NodeStatusSkipped {
		t.Fatalf("join.Status = %q, want skipped", got)
	}
	// sink depends on join → must be skipped.
	if got := res.Nodes["sink"].Status; got != NodeStatusSkipped {
		t.Fatalf("sink.Status = %q, want skipped", got)
	}
	// in1 + in2 ran successfully (they have no deps).
	if got := res.Nodes["in1"].Status; got != NodeStatusSuccess {
		t.Fatalf("in1.Status = %q, want success", got)
	}
}

func TestRunDAG_RetryUntilSuccess(t *testing.T) {
	p := dagPipeline()
	var t1Calls atomic.Int32
	rec := newRecordingRunner(func(ctx context.Context, node DAGNode, attempt int) error {
		if node.Name == "t1" {
			n := t1Calls.Add(1)
			if n < 3 {
				return fmt.Errorf("transient failure %d", n)
			}
		}
		return nil
	})
	res, err := RunDAG(context.Background(), p, RunOptions{
		Runner:        rec,
		FailurePolicy: FailurePolicyRetry,
		MaxRetries:    3,
		RetryBackoff:  0,
	})
	if err != nil {
		t.Fatalf("RunDAG: %v", err)
	}
	if res.Status != "success" {
		t.Fatalf("Status = %q, want success", res.Status)
	}
	if got, want := res.Nodes["t1"].Attempts, 3; got != want {
		t.Fatalf("t1.Attempts = %d, want %d (2 failures + 1 success)", got, want)
	}
	if got := res.Nodes["sink"].Status; got != NodeStatusSuccess {
		t.Fatalf("sink.Status = %q, want success after retry recovery", got)
	}
}

func TestRunDAG_RetryExhaustedAborts(t *testing.T) {
	p := dagPipeline()
	failErr := errors.New("permanent")
	rec := newRecordingRunner(func(ctx context.Context, node DAGNode, attempt int) error {
		if node.Name == "t1" {
			return failErr
		}
		return nil
	})
	res, err := RunDAG(context.Background(), p, RunOptions{
		Runner:        rec,
		FailurePolicy: FailurePolicyRetry,
		MaxRetries:    2,
	})
	if err == nil {
		t.Fatal("expected error after retry exhaustion")
	}
	if !errors.Is(err, failErr) {
		t.Fatalf("expected error to wrap failErr, got %v", err)
	}
	if got, want := res.Nodes["t1"].Attempts, 3; got != want {
		t.Fatalf("t1.Attempts = %d, want %d (1 initial + 2 retries)", got, want)
	}
	// Retry policy aborts after exhaustion → join must not have run.
	if s := res.Nodes["join"].Status; s == NodeStatusSuccess || s == NodeStatusFailed {
		t.Errorf("join ran after retry exhaustion: status=%q", s)
	}
}

func TestRunDAG_ParallelismLimit(t *testing.T) {
	// fan-out: 4 independent transforms feeding a single output.
	p := &Pipeline{
		ID:   "fanout",
		Name: "fanout",
		Inputs: []Input{
			{Name: "src", Type: "objectset"},
		},
		Transforms: []Transform{
			{Name: "a", Type: "filter", Inputs: []string{"src"}},
			{Name: "b", Type: "filter", Inputs: []string{"src"}},
			{Name: "c", Type: "filter", Inputs: []string{"src"}},
			{Name: "d", Type: "filter", Inputs: []string{"src"}},
		},
		Outputs: []Output{
			{Name: "sink", Type: "jdbc", Input: "a"},
		},
	}
	var inflight atomic.Int32
	var maxObserved atomic.Int32
	rec := newRecordingRunner(func(ctx context.Context, node DAGNode, attempt int) error {
		if node.Kind != NodeKindTransform {
			return nil
		}
		cur := inflight.Add(1)
		// Update max in a CAS loop.
		for {
			old := maxObserved.Load()
			if cur <= old || maxObserved.CompareAndSwap(old, cur) {
				break
			}
		}
		// hold long enough that other goroutines can climb into parallel.
		time.Sleep(40 * time.Millisecond)
		inflight.Add(-1)
		return nil
	})
	res, err := RunDAG(context.Background(), p, RunOptions{
		Runner:      rec,
		Parallelism: 2,
	})
	if err != nil {
		t.Fatalf("RunDAG: %v", err)
	}
	if res.Status != "success" {
		t.Fatalf("Status = %q, want success", res.Status)
	}
	got := maxObserved.Load()
	if got > 2 {
		t.Fatalf("max observed concurrent transforms = %d, want ≤ 2", got)
	}
	if got < 2 {
		t.Fatalf("max observed concurrent transforms = %d, want ≥ 2 (parallelism not exercised)", got)
	}
}

func TestRunDAG_SerialWhenParallelismOne(t *testing.T) {
	p := dagPipeline()
	var inflight atomic.Int32
	var maxObserved atomic.Int32
	rec := newRecordingRunner(func(ctx context.Context, node DAGNode, attempt int) error {
		cur := inflight.Add(1)
		for {
			old := maxObserved.Load()
			if cur <= old || maxObserved.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		inflight.Add(-1)
		return nil
	})
	if _, err := RunDAG(context.Background(), p, RunOptions{Runner: rec, Parallelism: 1}); err != nil {
		t.Fatalf("RunDAG: %v", err)
	}
	if got := maxObserved.Load(); got != 1 {
		t.Fatalf("Parallelism=1 observed concurrent=%d, want 1", got)
	}
}

func TestRunDAG_ContextCancellation(t *testing.T) {
	p := dagPipeline()
	ctx, cancel := context.WithCancel(context.Background())
	// First node to run cancels the parent ctx; subsequent nodes (or the
	// node itself) see ctx.Done() and unblock. Whichever node acquires
	// the semaphore first triggers the cancel — order-independent.
	rec := newRecordingRunner(func(ctx context.Context, node DAGNode, attempt int) error {
		cancel()
		<-ctx.Done()
		return ctx.Err()
	})
	res, err := RunDAG(ctx, p, RunOptions{
		Runner:      rec,
		Parallelism: 1,
	})
	if err == nil {
		t.Fatal("expected error on context cancellation")
	}
	if res.Status != "failed" {
		t.Fatalf("Status = %q, want failed", res.Status)
	}
}

func TestRunDAG_RejectsBadOptions(t *testing.T) {
	p := dagPipeline()
	if _, err := RunDAG(context.Background(), p, RunOptions{Runner: nil}); err == nil {
		t.Fatal("RunDAG with nil Runner returned nil err")
	}
	rec := newRecordingRunner(nil)
	if _, err := RunDAG(context.Background(), p, RunOptions{Runner: rec, FailurePolicy: "garbage"}); err == nil {
		t.Fatal("RunDAG with unknown failure policy returned nil err")
	}
}
