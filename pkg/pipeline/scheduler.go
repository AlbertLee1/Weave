package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/robfig/cron/v3"
)

// PipelineRunner is invoked once per cron tick. The Pipeline argument is
// the snapshot loaded from the Store at registration time (or at the
// most recent Reload / Register call), so changes to the persisted row
// take effect after the admin handler re-registers — same shape as
// automate.Scheduler. Implementations typically call RunDAG against an
// engine, persist a run row, and surface metrics. Errors returned here
// are logged but never crash the scheduler — a flaky pipeline does not
// take down healthy peers.
type PipelineRunner interface {
	RunPipeline(ctx context.Context, p *Pipeline) error
}

// PipelineRunnerFunc adapts a plain function to PipelineRunner.
type PipelineRunnerFunc func(ctx context.Context, p *Pipeline) error

// RunPipeline satisfies PipelineRunner.
func (f PipelineRunnerFunc) RunPipeline(ctx context.Context, p *Pipeline) error {
	return f(ctx, p)
}

// Scheduler is the cron-driven trigger surface for pipelines (US-289).
// It loads enabled pipelines that carry a non-empty Schedule from the
// Store at Start, registers each one with robfig/cron, and dispatches
// the configured PipelineRunner on every tick. Admin handlers keep the
// in-process registry in sync via Register / Unregister; Reload re-syncs
// the whole thing from the Store.
//
// Lifecycle mirrors pkg/audit/retention.Scheduler — Start is idempotent,
// Stop waits for the in-flight cron-internal jobs to drain, and the
// captured stop/done channels avoid the Stop / goroutine race noted in
// pattern 167.
type Scheduler struct {
	store  Store
	runner PipelineRunner
	parser cron.Parser
	logger func(format string, v ...any)

	mu      sync.Mutex
	cron    *cron.Cron
	entries map[string]cron.EntryID
	ctx     context.Context //nolint:containedctx // run-scoped ctx threaded by design
}

// NewScheduler wires a Scheduler around store and runner. Either may be
// nil so degraded-mode wiring (no PG, or no executor configured) can
// still construct the value — Start will then refuse with a clear error
// instead of panicking.
//
// The parser accepts standard 5-field cron expressions, optional 6-field
// expressions with leading seconds, robfig descriptors (@every, @hourly,
// …) — matches ValidateSchedule's "5 or 6 whitespace-separated fields"
// contract and the descriptor catalogue robfig/cron v3 ships with.
func NewScheduler(store Store, runner PipelineRunner) *Scheduler {
	return &Scheduler{
		store:  store,
		runner: runner,
		parser: cron.NewParser(
			cron.SecondOptional |
				cron.Minute |
				cron.Hour |
				cron.Dom |
				cron.Month |
				cron.Dow |
				cron.Descriptor,
		),
		logger:  log.Printf,
		entries: make(map[string]cron.EntryID),
	}
}

// SetLogger overrides the default log.Printf-backed logger. Passing nil
// restores the default — keeps degraded-mode wiring resilient against
// "main forgot to set the logger" callers.
func (s *Scheduler) SetLogger(fn func(format string, v ...any)) {
	if fn == nil {
		fn = log.Printf
	}
	s.logger = fn
}

// Start initializes the cron and loads the enabled, scheduled pipelines
// from the Store. The cron loop runs on its own goroutine; Start returns
// once the initial registration pass is complete. Idempotent — calling
// Start twice on a running scheduler is a no-op.
func (s *Scheduler) Start(ctx context.Context) error {
	if s.store == nil {
		return errors.New("pipeline.Scheduler: store must not be nil")
	}
	if s.runner == nil {
		return errors.New("pipeline.Scheduler: runner must not be nil")
	}
	s.mu.Lock()
	if s.cron != nil {
		s.mu.Unlock()
		return nil
	}
	s.cron = cron.New(cron.WithParser(s.parser))
	s.ctx = ctx
	s.mu.Unlock()

	if err := s.loadAll(ctx); err != nil {
		// loadAll already attempts each pipeline independently; the only
		// way it returns non-nil is if the store-level List failed. Wipe
		// the half-built scheduler so the next Start call retries cleanly.
		s.mu.Lock()
		s.cron = nil
		s.entries = make(map[string]cron.EntryID)
		s.mu.Unlock()
		return err
	}

	s.mu.Lock()
	c := s.cron
	s.mu.Unlock()
	if c != nil {
		c.Start()
	}
	return nil
}

// Stop halts the cron and waits for any in-flight tick callbacks to
// return. Idempotent — Stop on a never-started scheduler, or two
// back-to-back Stops, both no-op without panicking.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	c := s.cron
	s.cron = nil
	s.entries = make(map[string]cron.EntryID)
	s.mu.Unlock()
	if c == nil {
		return
	}
	// cron.Stop returns a context that closes once every running job has
	// returned — wait on it so callers see strict happens-before
	// semantics between Stop returning and runner callbacks finishing.
	doneCtx := c.Stop()
	<-doneCtx.Done()
}

// Reload re-syncs the in-process registry with the persisted state.
// Pipelines that have been deleted or had Enabled flipped off / the
// Schedule cleared are unregistered; new or modified rows replace the
// existing entry. Errors loading individual pipelines are logged but do
// not abort the reload — a misconfigured row mustn't take down siblings.
func (s *Scheduler) Reload(ctx context.Context) error {
	if s.cron == nil {
		return errors.New("pipeline.Scheduler: not started")
	}
	return s.loadAll(ctx)
}

// Register adds (or replaces) the cron entry for p. Disabled pipelines
// or pipelines with an empty Schedule cause the existing entry — if any
// — to be unregistered, so callers can use Register as the single
// "this pipeline changed, sync the scheduler" entry point regardless of
// the change shape.
func (s *Scheduler) Register(p *Pipeline) error {
	if p == nil {
		return errors.New("pipeline.Scheduler: pipeline is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cron == nil {
		return errors.New("pipeline.Scheduler: not started")
	}
	return s.registerLocked(p)
}

// Unregister drops the cron entry for id. No-op when the id is unknown.
func (s *Scheduler) Unregister(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cron == nil {
		return
	}
	s.unregisterLocked(id)
}

// Entries returns a snapshot of the current id → cron.EntryID map. The
// caller may freely mutate the returned map. Surfaced for admin /
// debugging endpoints + tests; production code should not branch on the
// concrete EntryID values.
func (s *Scheduler) Entries() map[string]cron.EntryID {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]cron.EntryID, len(s.entries))
	for id, eid := range s.entries {
		out[id] = eid
	}
	return out
}

// loadAll lists every pipeline from the store and reconciles the
// scheduler entries against the result.
func (s *Scheduler) loadAll(ctx context.Context) error {
	pipelines, err := s.store.ListPipelines(ctx, "")
	if err != nil {
		return fmt.Errorf("pipeline.Scheduler: list pipelines: %w", err)
	}
	seen := make(map[string]struct{}, len(pipelines))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cron == nil {
		// Stopped while listing — bail out gracefully.
		return nil
	}
	for _, p := range pipelines {
		seen[p.ID] = struct{}{}
		if regErr := s.registerLocked(p); regErr != nil {
			s.logger("[pipeline scheduler] register %q failed: %v", p.ID, regErr)
		}
	}
	// Unregister anything that disappeared from the store.
	for id := range s.entries {
		if _, ok := seen[id]; !ok {
			s.unregisterLocked(id)
		}
	}
	return nil
}

// registerLocked installs (or replaces) the cron entry for p. Caller
// must hold s.mu and s.cron must be non-nil.
func (s *Scheduler) registerLocked(p *Pipeline) error {
	id := p.ID
	if !p.Enabled || p.Schedule == "" {
		s.unregisterLocked(id)
		return nil
	}
	if _, err := s.parser.Parse(p.Schedule); err != nil {
		// Drop any stale entry so a freshly-broken schedule stops firing.
		s.unregisterLocked(id)
		return fmt.Errorf("pipeline.Scheduler: invalid schedule %q for pipeline %q: %w", p.Schedule, id, err)
	}
	// Snapshot the pipeline by value so the cron callback runs on stable
	// state regardless of subsequent in-place mutations elsewhere.
	snapshot := ClonePipeline(p)
	job := cron.FuncJob(func() {
		s.dispatch(snapshot)
	})
	entryID, err := s.cron.AddJob(p.Schedule, job)
	if err != nil {
		// AddJob calls the parser internally — Parse above should make
		// this branch unreachable, but the belt-and-braces unregister
		// keeps state consistent if a future cron version changes.
		s.unregisterLocked(id)
		return fmt.Errorf("pipeline.Scheduler: register %q: %w", id, err)
	}
	// Replace any prior entry for the same id under the same lock so
	// callers never see a window with two live entries firing for one
	// pipeline.
	if old, ok := s.entries[id]; ok {
		s.cron.Remove(old)
	}
	s.entries[id] = entryID
	return nil
}

// unregisterLocked drops the cron entry for id. Caller must hold s.mu
// and s.cron must be non-nil.
func (s *Scheduler) unregisterLocked(id string) {
	entryID, ok := s.entries[id]
	if !ok {
		return
	}
	s.cron.Remove(entryID)
	delete(s.entries, id)
}

// dispatch is invoked by the cron goroutine on every tick. Errors from
// the runner are logged but never propagated — see the type comment on
// PipelineRunner.
func (s *Scheduler) dispatch(p *Pipeline) {
	s.mu.Lock()
	ctx := s.ctx
	s.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.runner.RunPipeline(ctx, p); err != nil {
		s.logger("[pipeline scheduler] pipeline %q failed: %v", p.ID, err)
	}
}
