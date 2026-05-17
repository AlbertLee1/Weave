package materialize

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// US-486 — Scheduler for materialized datasets.
//
// Where the existing Materializer is event-driven (one Parquet file per
// EditBatch) and the Retainer runs a wall-clock compact/archive sweep,
// production needs a third long-lived loop for materialized *datasets*
// whose recompute logic is operator-supplied and must execute on a
// schedule. The Scheduler is that loop: each named MaterializeJob owns
// an Interval, a Compute func, and a retry policy. State is recorded
// per-job so an operator can answer "when did dataset X last refresh,
// and is it healthy?" without having to mine logs.

// MaterializeJob describes one scheduled recompute target. Name is used
// for status lookups and onError reporting; it must be unique within a
// Scheduler. Interval is the cadence at which RunLoop invokes Compute.
// MaxAttempts and BaseBackoff control the per-run retry policy:
// MaxAttempts <= 0 falls back to DefaultMaxAttempts (3); BaseBackoff <=
// 0 falls back to DefaultBaseBackoff (1s).
type MaterializeJob struct {
	Name        string
	Interval    time.Duration
	Compute     func(ctx context.Context) error
	MaxAttempts int
	BaseBackoff time.Duration
}

const (
	// DefaultMaxAttempts is the per-run retry ceiling when a job leaves
	// MaxAttempts at zero. Three attempts (initial + 2 retries) covers
	// transient flakes while keeping the worst-case wall-clock cost of a
	// failing job bounded.
	DefaultMaxAttempts = 3
	// DefaultBaseBackoff is the per-run backoff base when a job leaves
	// BaseBackoff at zero. Exponential growth: 1s, 2s, 4s, …
	DefaultBaseBackoff = time.Second
	// maxBackoffShift caps the exponential-backoff shift count so a
	// pathological MaxAttempts can't overflow int64.
	maxBackoffShift = 16
)

// JobStatus is a snapshot of a job's persisted state. All Time values
// are UTC. LastSuccess is zero until at least one run succeeds;
// LastFailure is zero until at least one run exhausts its attempts.
// TotalRuns counts RunOnce invocations (a single RunOnce that retries
// internally still counts once); TotalFailures counts the subset of
// runs that exhausted MaxAttempts. ConsecutiveFailures is reset to 0
// on every success and is the field operators wire to alerting.
type JobStatus struct {
	Name                string
	Interval            time.Duration
	MaxAttempts         int
	LastRunStart        time.Time
	LastSuccess         time.Time
	LastFailure         time.Time
	ConsecutiveFailures int
	TotalRuns           int
	TotalFailures       int
	LastError           string
}

// Scheduler is the long-lived loop driver for materialized datasets.
// Safe for concurrent use; RunOnce / RunLoop / Status / ListStatus may
// all be invoked from independent goroutines.
type Scheduler struct {
	mu    sync.Mutex
	jobs  map[string]*scheduledJob
	nowFn func() time.Time
	sleep func(ctx context.Context, d time.Duration) error
}

type scheduledJob struct {
	job    MaterializeJob
	status JobStatus
}

// NewScheduler returns an empty Scheduler that uses wall-clock time and
// a context-aware sleep for retry backoff. Tests inject SetNowFunc /
// SetSleepFunc to drive the loop deterministically.
func NewScheduler() *Scheduler {
	return &Scheduler{
		jobs:  make(map[string]*scheduledJob),
		nowFn: func() time.Time { return time.Now().UTC() },
		sleep: defaultSleep,
	}
}

// SetNowFunc overrides the clock used to stamp JobStatus timestamps.
// Passing nil is a no-op.
func (s *Scheduler) SetNowFunc(fn func() time.Time) {
	if fn == nil {
		return
	}
	s.mu.Lock()
	s.nowFn = fn
	s.mu.Unlock()
}

// SetSleepFunc overrides the retry backoff sleep. The provided function
// must respect ctx so a cancellation aborts pending backoff promptly.
// Passing nil is a no-op.
func (s *Scheduler) SetSleepFunc(fn func(ctx context.Context, d time.Duration) error) {
	if fn == nil {
		return
	}
	s.mu.Lock()
	s.sleep = fn
	s.mu.Unlock()
}

// Add registers a job. Name must be non-blank, Compute must be non-nil,
// and Name must be unique within the Scheduler. Re-adding a job under
// the same Name returns an error rather than silently clobbering the
// existing status — operators reloading config should remove the old
// job first or call Status to confirm intent.
func (s *Scheduler) Add(job MaterializeJob) error {
	name := strings.TrimSpace(job.Name)
	if name == "" {
		return errors.New("materialize: scheduler job Name is blank")
	}
	if job.Compute == nil {
		return errors.New("materialize: scheduler job Compute is nil")
	}
	job.Name = name
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.jobs[name]; exists {
		return fmt.Errorf("materialize: scheduler job %q already registered", name)
	}
	s.jobs[name] = &scheduledJob{
		job: job,
		status: JobStatus{
			Name:        name,
			Interval:    job.Interval,
			MaxAttempts: effectiveMaxAttempts(job.MaxAttempts),
		},
	}
	return nil
}

// Remove drops a job from the scheduler. Unknown names are silently
// ignored — callers reloading config want idempotent removal.
func (s *Scheduler) Remove(name string) {
	s.mu.Lock()
	delete(s.jobs, strings.TrimSpace(name))
	s.mu.Unlock()
}

// Status returns the latest JobStatus for the named job. Returns
// (zero, false) when no job by that name is registered.
func (s *Scheduler) Status(name string) (JobStatus, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sj, ok := s.jobs[strings.TrimSpace(name)]
	if !ok {
		return JobStatus{}, false
	}
	return sj.status, true
}

// ListStatus returns a snapshot of every registered job's status,
// sorted by Name. Mutations to the returned slice never leak back into
// the scheduler.
func (s *Scheduler) ListStatus() []JobStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]JobStatus, 0, len(s.jobs))
	for _, sj := range s.jobs {
		out = append(out, sj.status)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// RunOnce invokes the named job's Compute synchronously, retrying up to
// MaxAttempts times with exponential backoff. State is updated whether
// the run succeeds or exhausts. Returns the last Compute error wrapped
// with the job name on exhaustion, or nil on success. An unknown name
// returns an error without touching state.
func (s *Scheduler) RunOnce(ctx context.Context, name string) error {
	s.mu.Lock()
	sj, ok := s.jobs[strings.TrimSpace(name)]
	nowFn := s.nowFn
	sleep := s.sleep
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("materialize: scheduler job %q not registered", name)
	}

	now := nowFn()
	s.mu.Lock()
	sj.status.LastRunStart = now
	sj.status.TotalRuns++
	s.mu.Unlock()

	job := sj.job
	maxAttempts := effectiveMaxAttempts(job.MaxAttempts)
	base := effectiveBaseBackoff(job.BaseBackoff)

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			lastErr = err
			break
		}
		err := safeInvoke(ctx, job.Compute)
		if err == nil {
			s.mu.Lock()
			sj.status.LastSuccess = nowFn()
			sj.status.LastError = ""
			sj.status.ConsecutiveFailures = 0
			s.mu.Unlock()
			return nil
		}
		lastErr = err
		if attempt == maxAttempts {
			break
		}
		if err := sleep(ctx, backoffFor(base, attempt)); err != nil {
			lastErr = err
			break
		}
	}

	s.mu.Lock()
	sj.status.LastFailure = nowFn()
	sj.status.ConsecutiveFailures++
	sj.status.TotalFailures++
	if lastErr != nil {
		sj.status.LastError = lastErr.Error()
	}
	s.mu.Unlock()

	if lastErr == nil {
		// Defensive: every fail path above sets lastErr, but keep a clear
		// sentinel rather than returning nil to surprise the caller.
		lastErr = errors.New("compute failed without an explicit error")
	}
	return fmt.Errorf("materialize: job %q failed: %w", job.Name, lastErr)
}

// RunLoop drives every registered job on its own goroutine. Each
// goroutine wakes on Interval, runs the job with retries, and reports
// any final failure via onError(jobName, err). RunLoop returns when ctx
// is cancelled and every per-job goroutine has stopped.
//
// onError is optional; pass nil to discard failures. Jobs with a
// non-positive Interval are skipped (operator-supplied "manual-only"
// datasets stay registered and can still be invoked via RunOnce).
func (s *Scheduler) RunLoop(ctx context.Context, onError func(jobName string, err error)) {
	s.mu.Lock()
	jobs := make([]*scheduledJob, 0, len(s.jobs))
	for _, sj := range s.jobs {
		jobs = append(jobs, sj)
	}
	s.mu.Unlock()

	var wg sync.WaitGroup
	for _, sj := range jobs {
		if sj.job.Interval <= 0 {
			continue
		}
		wg.Add(1)
		go func(name string, interval time.Duration) {
			defer wg.Done()
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := s.RunOnce(ctx, name); err != nil {
						if onError != nil {
							onError(name, err)
						}
					}
				}
			}
		}(sj.job.Name, sj.job.Interval)
	}
	wg.Wait()
}

func safeInvoke(ctx context.Context, fn func(context.Context) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("compute panicked: %v", r)
		}
	}()
	return fn(ctx)
}

func defaultSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func effectiveMaxAttempts(n int) int {
	if n <= 0 {
		return DefaultMaxAttempts
	}
	return n
}

func effectiveBaseBackoff(d time.Duration) time.Duration {
	if d <= 0 {
		return DefaultBaseBackoff
	}
	return d
}

// backoffFor returns the wait between attempt N and attempt N+1.
// attempt is 1-based (the first failed attempt is attempt=1 and the
// returned wait is the gap before attempt=2). Shift count is capped at
// maxBackoffShift so a pathological MaxAttempts never overflows int64.
func backoffFor(base time.Duration, attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}
	shift := attempt - 1
	if shift > maxBackoffShift {
		shift = maxBackoffShift
	}
	return base << shift
}
