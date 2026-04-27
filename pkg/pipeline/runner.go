package pipeline

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// FailurePolicy controls how the runner reacts when a node returns an
// error. The acceptance criterion for US-288 mandates the three values
// below.
type FailurePolicy string

const (
	// FailurePolicyAbort cancels every still-pending node on the first
	// failure and surfaces the failure error from RunDAG. In-flight
	// nodes receive a canceled context.
	FailurePolicyAbort FailurePolicy = "abort"
	// FailurePolicyContinue marks every transitive descendant of the
	// failed node as skipped but lets independent branches finish. The
	// run still ends with status="failed" and a non-nil error so callers
	// don't silently swallow failures.
	FailurePolicyContinue FailurePolicy = "continue"
	// FailurePolicyRetry retries the failing node up to MaxRetries
	// times with RetryBackoff between attempts. Once exhausted, the
	// run aborts (same blast radius as FailurePolicyAbort).
	FailurePolicyRetry FailurePolicy = "retry"
)

// IsKnownFailurePolicy reports whether p is one of the canonical three.
func IsKnownFailurePolicy(p FailurePolicy) bool {
	switch p {
	case FailurePolicyAbort, FailurePolicyContinue, FailurePolicyRetry:
		return true
	}
	return false
}

// NodeRunner executes a single DAG node. Implementations are typically
// connector-aware (objectset reader, JDBC writer, etc.) and stateless;
// concrete connectors land in later user stories.
type NodeRunner interface {
	Run(ctx context.Context, node DAGNode, attempt int) error
}

// NodeRunnerFunc adapts a plain function to NodeRunner.
type NodeRunnerFunc func(ctx context.Context, node DAGNode, attempt int) error

// Run satisfies NodeRunner.
func (f NodeRunnerFunc) Run(ctx context.Context, node DAGNode, attempt int) error {
	return f(ctx, node, attempt)
}

// RunOptions tunes RunDAG.
//
// Parallelism caps concurrent in-flight nodes. Non-positive values are
// treated as 1 (serial execution) so a misconfigured caller never gets
// unbounded concurrency.
//
// FailurePolicy defaults to FailurePolicyAbort.
//
// MaxRetries is honored only when FailurePolicy == retry. RetryBackoff
// pauses between attempts; ctx cancellation interrupts the wait.
//
// Now is an injection point for deterministic time in tests; defaults
// to time.Now.
type RunOptions struct {
	Parallelism   int
	FailurePolicy FailurePolicy
	MaxRetries    int
	RetryBackoff  time.Duration
	Runner        NodeRunner
	Now           func() time.Time
}

// NodeStatus is the per-node outcome captured on RunResult.Nodes.
type NodeStatus string

const (
	NodeStatusPending  NodeStatus = "pending"
	NodeStatusRunning  NodeStatus = "running"
	NodeStatusSuccess  NodeStatus = "success"
	NodeStatusFailed   NodeStatus = "failed"
	NodeStatusSkipped  NodeStatus = "skipped"
	NodeStatusCanceled NodeStatus = "canceled"
)

// NodeResult is the per-node row in RunResult.Nodes.
type NodeResult struct {
	Name     string        `json:"name"`
	Kind     NodeKind      `json:"kind"`
	Type     string        `json:"type"`
	Status   NodeStatus    `json:"status"`
	Attempts int           `json:"attempts"`
	Error    string        `json:"error,omitempty"`
	Duration time.Duration `json:"durationNs"`
}

// RunResult is the aggregate outcome of one RunDAG invocation.
type RunResult struct {
	Status     string                 `json:"status"`
	Order      []string               `json:"order"`
	Nodes      map[string]*NodeResult `json:"nodes"`
	StartedAt  time.Time              `json:"startedAt"`
	FinishedAt time.Time              `json:"finishedAt"`
	Error      string                 `json:"error,omitempty"`
}

// doneEvent is a worker-to-coordinator message reporting one node's
// terminal outcome.
type doneEvent struct {
	name     string
	attempts int
	duration time.Duration
	err      error
}

// runState carries per-run mutable state through the coordinator loop;
// keeps RunDAG's cyclomatic complexity low by hiding the bookkeeping.
type runState struct {
	opts       RunOptions
	nodeByName map[string]DAGNode
	dependents map[string][]string
	remaining  map[string]int
	results    map[string]*NodeResult
	order      []string
	doneCh     chan doneEvent
	sem        chan struct{}
	wg         *sync.WaitGroup
	runCtx     context.Context //nolint:containedctx // run-scoped ctx threaded by design
	cancel     context.CancelFunc
	inflight   int
	aborted    bool
	firstErr   error
}

// RunDAG executes p as a directed acyclic graph honoring opts. The
// returned RunResult is fully populated even when the function returns
// an error so callers may persist the run row regardless of outcome.
func RunDAG(ctx context.Context, p *Pipeline, opts RunOptions) (*RunResult, error) {
	nodes, order, err := prepareDAG(p)
	if err != nil {
		return nil, err
	}
	opts, err = normalizeRunOptions(opts)
	if err != nil {
		return nil, err
	}

	st := newRunState(ctx, nodes, order, opts)
	defer st.cancel()

	startedAt := opts.Now()
	st.scheduleReady()
	st.coordinate()
	st.wg.Wait()
	st.finalizeStatuses()

	return st.buildResult(startedAt, opts.Now()), st.firstErr
}

// prepareDAG builds the DAG and computes the topological order. Pulled
// out of RunDAG so the coordinator function stays small.
func prepareDAG(p *Pipeline) ([]DAGNode, []string, error) {
	nodes, err := BuildDAG(p)
	if err != nil {
		return nil, nil, err
	}
	order, err := TopoOrder(nodes)
	if err != nil {
		return nil, nil, err
	}
	return nodes, order, nil
}

// normalizeRunOptions validates opts and applies defaults.
func normalizeRunOptions(opts RunOptions) (RunOptions, error) {
	if opts.Runner == nil {
		return opts, errors.New("RunOptions.Runner must not be nil")
	}
	if opts.FailurePolicy == "" {
		opts.FailurePolicy = FailurePolicyAbort
	}
	if !IsKnownFailurePolicy(opts.FailurePolicy) {
		return opts, fmt.Errorf("unknown failure policy %q", opts.FailurePolicy)
	}
	if opts.Parallelism <= 0 {
		opts.Parallelism = 1
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return opts, nil
}

// newRunState wires up the coordinator's bookkeeping maps.
func newRunState(ctx context.Context, nodes []DAGNode, order []string, opts RunOptions) *runState {
	runCtx, cancel := context.WithCancel(ctx)
	st := &runState{
		opts:       opts,
		nodeByName: make(map[string]DAGNode, len(nodes)),
		dependents: make(map[string][]string, len(nodes)),
		remaining:  make(map[string]int, len(nodes)),
		results:    make(map[string]*NodeResult, len(nodes)),
		order:      order,
		doneCh:     make(chan doneEvent),
		sem:        make(chan struct{}, opts.Parallelism),
		wg:         &sync.WaitGroup{},
		runCtx:     runCtx,
		cancel:     cancel,
	}
	for _, n := range nodes {
		st.nodeByName[n.Name] = n
		st.remaining[n.Name] = len(n.Deps)
		for _, dep := range n.Deps {
			st.dependents[dep] = append(st.dependents[dep], n.Name)
		}
		st.results[n.Name] = &NodeResult{Name: n.Name, Kind: n.Kind, Type: n.Type, Status: NodeStatusPending}
	}
	return st
}

// scheduleReady dispatches every node that already has zero
// in-degree (i.e. all input nodes plus any transform/output without
// upstream deps).
func (s *runState) scheduleReady() {
	for _, name := range s.order {
		if s.remaining[name] == 0 {
			s.dispatch(name)
		}
	}
}

// dispatch starts a worker goroutine for one node.
func (s *runState) dispatch(name string) {
	s.results[name].Status = NodeStatusRunning
	s.inflight++
	s.wg.Add(1)
	go s.runWorker(name)
}

// runWorker is one node's lifecycle: acquire the semaphore, run with
// retries (when policy=retry), and post the doneEvent.
func (s *runState) runWorker(name string) {
	defer s.wg.Done()
	s.sem <- struct{}{}
	defer func() { <-s.sem }()
	node := s.nodeByName[name]
	start := s.opts.Now()
	attempts, err := s.executeWithRetries(node)
	s.doneCh <- doneEvent{
		name:     name,
		attempts: attempts,
		duration: s.opts.Now().Sub(start),
		err:      err,
	}
}

// executeWithRetries invokes Runner.Run, retrying per FailurePolicy.
// Returns the total attempt count (>=1) and the final error (nil on
// success).
func (s *runState) executeWithRetries(node DAGNode) (int, error) {
	attempts := 0
	for {
		attempts++
		err := s.opts.Runner.Run(s.runCtx, node, attempts)
		if err == nil {
			return attempts, nil
		}
		if s.opts.FailurePolicy != FailurePolicyRetry || attempts > s.opts.MaxRetries {
			return attempts, err
		}
		if !s.waitBeforeRetry() {
			return attempts, s.runCtx.Err()
		}
	}
}

// waitBeforeRetry sleeps for RetryBackoff or until ctx is done.
// Returns false if ctx was canceled (caller should stop retrying).
func (s *runState) waitBeforeRetry() bool {
	if s.opts.RetryBackoff <= 0 {
		return s.runCtx.Err() == nil
	}
	t := time.NewTimer(s.opts.RetryBackoff)
	defer t.Stop()
	select {
	case <-s.runCtx.Done():
		return false
	case <-t.C:
		return true
	}
}

// coordinate is the main loop: read worker events, update results, and
// dispatch newly-ready nodes (or skip downstream / abort per policy).
func (s *runState) coordinate() {
	for s.inflight > 0 {
		ev := <-s.doneCh
		s.inflight--
		s.handleEvent(ev)
	}
}

// handleEvent applies one worker outcome to the run state.
func (s *runState) handleEvent(ev doneEvent) {
	res := s.results[ev.name]
	res.Attempts = ev.attempts
	res.Duration = ev.duration

	if ev.err != nil {
		s.handleFailure(ev, res)
		return
	}

	res.Status = NodeStatusSuccess
	if s.aborted {
		return
	}
	s.dispatchNewlyReady(ev.name)
}

// handleFailure marks the failing node and applies the failure policy
// (abort/continue; retry exhaustion routes through abort here).
func (s *runState) handleFailure(ev doneEvent, res *NodeResult) {
	res.Status = NodeStatusFailed
	res.Error = ev.err.Error()
	if s.firstErr == nil {
		s.firstErr = fmt.Errorf("pipeline node %q failed: %w", ev.name, ev.err)
	}
	switch s.opts.FailurePolicy {
	case FailurePolicyAbort, FailurePolicyRetry:
		if !s.aborted {
			s.aborted = true
			s.cancel()
		}
	case FailurePolicyContinue:
		markDescendantsSkipped(ev.name, s.dependents, s.results)
	}
}

// dispatchNewlyReady decrements children's remaining-deps counters and
// dispatches any whose counter just hit zero.
func (s *runState) dispatchNewlyReady(parent string) {
	for _, child := range s.dependents[parent] {
		s.remaining[child]--
		if s.remaining[child] == 0 && s.results[child].Status == NodeStatusPending {
			s.dispatch(child)
		}
	}
}

// finalizeStatuses sweeps any node still marked pending/running into a
// terminal status: canceled when the run aborted, skipped otherwise.
func (s *runState) finalizeStatuses() {
	for _, res := range s.results {
		if res.Status == NodeStatusPending || res.Status == NodeStatusRunning {
			if s.aborted {
				res.Status = NodeStatusCanceled
			} else {
				res.Status = NodeStatusSkipped
			}
		}
	}
}

// buildResult assembles the externally-visible RunResult.
func (s *runState) buildResult(startedAt, finishedAt time.Time) *RunResult {
	rr := &RunResult{
		Status:     "success",
		Order:      s.order,
		Nodes:      s.results,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
	}
	if s.firstErr != nil {
		rr.Status = "failed"
		rr.Error = s.firstErr.Error()
	}
	return rr
}

// markDescendantsSkipped walks the dependents graph from start and
// marks every still-pending descendant as Skipped. Used by the
// continue policy so a failed branch's children don't dispatch.
func markDescendantsSkipped(start string, dependents map[string][]string, results map[string]*NodeResult) {
	visited := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, child := range dependents[cur] {
			if visited[child] {
				continue
			}
			visited[child] = true
			if r, ok := results[child]; ok && r.Status == NodeStatusPending {
				r.Status = NodeStatusSkipped
			}
			queue = append(queue, child)
		}
	}
}
