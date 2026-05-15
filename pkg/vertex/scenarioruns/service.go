package scenarioruns

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/liyang/weave/pkg/rid"
)

// ServiceOptions tunes the in-process worker pool that backs Service.
// Zero values fall back to safe defaults (Policy = MaxAttempts 3,
// MaxConcurrentWorkflows = unbounded). Sleep is forwarded into the
// embedded Workflow so tests can disable real wall-clock waits.
type ServiceOptions struct {
	Policy                 RetryPolicy
	MaxConcurrentWorkflows int
	Sleep                  func(d time.Duration)
}

// Service owns the in-process workflow lifecycle: it stamps a fresh
// run RID, persists the Run row, kicks off a goroutine that drives
// Workflow.Execute, and tracks the cancel handle so a later
// Cancel(rid) can interrupt the in-flight activity.
//
// Stop drains all running goroutines (cancelling them as a side
// effect) and is safe to call multiple times. The worker pool cap is
// enforced via a buffered semaphore — Run() blocks until a slot is
// available. This is intentional: the BDD acceptance demands "goroutine
// pool", not unbounded fan-out.
type Service struct {
	repo    Repo
	reader  ScenarioReader
	exec    ActivityExecutor
	policy  RetryPolicy
	sleep   func(time.Duration)

	sem chan struct{}

	mu      sync.Mutex
	cancels map[string]context.CancelFunc
	wg      sync.WaitGroup

	stopped bool
}

// NewService wires a Service. Repo and reader are required; exec is
// the per-activity dispatcher (the wiring story plugs in the modelmesh
// + function-action union). Panics on nil required deps — these are
// always programmer errors.
func NewService(repo Repo, reader ScenarioReader, exec ActivityExecutor, opts ServiceOptions) *Service {
	if repo == nil {
		panic("scenarioruns: nil repo")
	}
	if reader == nil {
		panic("scenarioruns: nil reader")
	}
	if exec == nil {
		panic("scenarioruns: nil exec")
	}
	policy := opts.Policy.Normalize()
	sleep := opts.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	var sem chan struct{}
	if opts.MaxConcurrentWorkflows > 0 {
		sem = make(chan struct{}, opts.MaxConcurrentWorkflows)
	}
	return &Service{
		repo:    repo,
		reader:  reader,
		exec:    exec,
		policy:  policy,
		sleep:   sleep,
		sem:     sem,
		cancels: map[string]context.CancelFunc{},
	}
}

// NewRunRID generates a fresh Vertex-namespaced run RID. Exported so
// the wiring layer can use it before Run() is called (e.g. to stamp
// audit log rows that need to reference the run).
func NewRunRID() string {
	return rid.New("vertex", "main", "scenario-run")
}

// Run resolves the scenario's activities, persists a fresh Run row in
// pending status, and spawns a goroutine that drives the workflow to
// completion. Returns the run RID immediately — clients poll GetRun
// (or the HTTP GET endpoint) for terminal state. A nil-activities
// scenario (no models, no actions) is rejected with InvalidScenario
// rather than silently succeeding.
func (s *Service) Run(ctx context.Context, scenarioRID string) (string, error) {
	if s.isStopped() {
		return "", errors.New("scenarioruns: service stopped")
	}
	activities, err := s.reader.ListActivities(ctx, scenarioRID)
	if err != nil {
		return "", err
	}
	runRID := NewRunRID()
	run := Run{
		RID:         runRID,
		ScenarioRID: scenarioRID,
		Status:      RunStatusPending,
		StartedAt:   time.Now(),
		CreatedAt:   time.Now(),
		Checkpoint: RunCheckpoint{
			RunRID:       runRID,
			ScenarioRID:  scenarioRID,
			Status:       RunStatusPending,
			AttemptsByID: map[string]int{},
			UpdatedAt:    time.Now(),
		},
	}
	if err := s.repo.CreateRun(ctx, run); err != nil {
		return "", err
	}
	s.spawn(runRID, scenarioRID, activities, nil)
	return runRID, nil
}

// Cancel signals the in-flight workflow goroutine to stop. The
// goroutine's ctx.Done fires; the executor sees it on the next attempt
// boundary and returns context.Canceled, after which the workflow
// rewrites the checkpoint with Status=Canceled. Cancel is non-blocking
// — callers that want to wait for terminal state should poll GetRun.
func (s *Service) Cancel(ctx context.Context, runRID string) error {
	r, err := s.repo.GetRun(ctx, runRID)
	if err != nil {
		return err
	}
	if IsTerminal(r.Status) {
		return ErrAlreadyTerminal
	}
	s.mu.Lock()
	cancel, ok := s.cancels[runRID]
	s.mu.Unlock()
	if !ok {
		// Run row exists and is non-terminal but no in-process
		// goroutine owns it. This happens when a different process
		// owns the run (out of scope for v1) or after a crash before
		// resume. Best-effort: mark canceled in the repo so the next
		// resume sweep skips it.
		r.Status = RunStatusCanceled
		r.Checkpoint.Status = RunStatusCanceled
		r.Checkpoint.UpdatedAt = time.Now()
		now := time.Now()
		r.CompletedAt = &now
		_ = s.repo.SaveCheckpoint(ctx, r.Checkpoint)
		return nil
	}
	cancel()
	return nil
}

// ResumeAll picks up every resumable run from the repo and spawns a
// fresh workflow goroutine that fast-forwards past activities the
// prior worker already completed. Returns the RIDs that were resumed.
//
// Intended to be called once during cmd/server/main.go startup, after
// the repo + executor are wired but before the HTTP listener accepts
// new runs — though running it concurrently with new Run() calls is
// safe (the resume goroutines compete for the same pool semaphore).
func (s *Service) ResumeAll(ctx context.Context) ([]string, error) {
	if s.isStopped() {
		return nil, errors.New("scenarioruns: service stopped")
	}
	runs, err := s.repo.ListResumable(ctx)
	if err != nil {
		return nil, err
	}
	resumed := make([]string, 0, len(runs))
	for _, r := range runs {
		activities, err := s.reader.ListActivities(ctx, r.ScenarioRID)
		if err != nil {
			continue
		}
		s.spawn(r.RID, r.ScenarioRID, activities, r.Checkpoint.Completed)
		resumed = append(resumed, r.RID)
	}
	return resumed, nil
}

// Stop signals every in-flight workflow to cancel and waits for the
// goroutines to drain. Safe to call multiple times. Stop does not
// mutate the repo — runs that were canceled mid-flight will land in
// Canceled status via the workflow's normal cancel path.
func (s *Service) Stop(_ context.Context) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	for _, cancel := range s.cancels {
		cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *Service) isStopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

// spawn launches the per-run goroutine. The goroutine registers a
// cancel handle for Cancel() to grab, then waits for a worker-pool
// slot (semaphore) before driving Workflow.Execute. The semaphore is
// acquired inside the goroutine — never on Run()'s caller stack — so
// Run() returns immediately and a saturated pool merely queues the
// new run instead of back-pressuring the HTTP request.
//
// A cancel that fires while the goroutine is queued aborts before any
// activity runs; the run is then marked canceled in the repo so the
// status endpoint reports it correctly.
func (s *Service) spawn(runRID, scenarioRID string, activities []Activity, completed []string) {
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.cancels[runRID] = cancel
	s.wg.Add(1)
	s.mu.Unlock()

	remaining := SkipCompleted(activities, completed)
	wf := &Workflow{
		Policy:  s.policy,
		Persist: s.repo,
		Sleep:   s.sleep,
	}
	go func() {
		defer s.wg.Done()
		defer func() {
			s.mu.Lock()
			delete(s.cancels, runRID)
			s.mu.Unlock()
			cancel()
		}()
		if s.sem != nil {
			select {
			case s.sem <- struct{}{}:
				defer func() { <-s.sem }()
			case <-ctx.Done():
				s.markCanceledBeforeStart(runRID, scenarioRID, ctx.Err())
				return
			}
		}
		// Re-check after acquiring the slot — Stop() can race with us.
		if err := ctx.Err(); err != nil {
			s.markCanceledBeforeStart(runRID, scenarioRID, err)
			return
		}
		// We deliberately ignore the error — the checkpoint already
		// captured the failure. Caller sees terminal state via GetRun.
		_, _, _ = wf.Execute(ctx, runRID, scenarioRID, remaining, s.exec)
	}()
}

// markCanceledBeforeStart writes a Canceled checkpoint for a run that
// was canceled (or stopped) before any activity ran. We use a fresh
// background ctx because the per-run ctx is already done.
func (s *Service) markCanceledBeforeStart(runRID, scenarioRID string, err error) {
	cp := RunCheckpoint{
		RunRID:       runRID,
		ScenarioRID:  scenarioRID,
		Status:       RunStatusCanceled,
		AttemptsByID: map[string]int{},
		UpdatedAt:    time.Now(),
	}
	if err != nil {
		cp.Error = err.Error()
	}
	_ = s.repo.SaveCheckpoint(context.Background(), cp)
}
