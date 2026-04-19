// Package retention provides the nightly archive-and-delete pipeline for
// audit events (US-269).
//
// Operators opt in by setting AUDIT_RETENTION_DAYS > 0. The scheduler
// wakes up every RetentionInterval (default 24h) and — when enabled —
// streams rows older than `now - RetentionDays` through the configured
// Archiver in chain-order batches, then issues a single DeleteBefore
// covering the full cutoff. Archive-before-delete is mandatory when an
// Archiver is wired: if the archive step fails for any batch the sweep
// aborts and the DB rows are LEFT IN PLACE so the next run can retry.
//
// The retention sweep intentionally breaks the US-266 tamper-proof chain
// invariant — once old rows are deleted the chain's first live row has a
// prev_hash that doesn't correspond to any in-DB entry. That's a
// documented trade-off: chain integrity is preserved inside the
// retention window by the live PG store, and the archived NDJSON objects
// are the cold-path integrity record for evicted rows. Operators who
// need cryptographic proof of the full chain over long timescales should
// verify against the archive, not against PG.
package retention

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/liyang/weave/pkg/audit"
)

// DefaultInterval is the retention sweep cadence when none is set.
const DefaultInterval = 24 * time.Hour

// DefaultBatchSize bounds the archive-pagination page size. 1000 rows /
// page is a safe middle ground between PG statement memory and the
// number of round trips on a long backlog.
const DefaultBatchSize = 1000

// Store is the narrow persistence surface the retention service calls.
// *audit.PGStore implements it directly.
type Store interface {
	// ListBefore returns up to limit events whose timestamp is strictly
	// earlier than `before` AND whose chain_seq is strictly greater than
	// cursor, ORDER BY chain_seq ASC. Callers page by handing the last
	// returned ChainSeq back as the next cursor. cursor=0 starts from
	// the beginning.
	ListBefore(ctx context.Context, before time.Time, cursor int64, limit int) ([]audit.AuditEvent, error)

	// DeleteBefore removes every audit_events row whose timestamp is
	// strictly earlier than `before`. Returns the count removed.
	DeleteBefore(ctx context.Context, before time.Time) (int64, error)
}

// Archiver is the cold-storage sink that receives batches of expired
// events before they're deleted from PG. Implementations MUST be
// idempotent: a transient failure before DeleteBefore means the next
// retention cycle will re-enqueue the same batch.
type Archiver interface {
	Archive(ctx context.Context, batch []audit.AuditEvent) error
	Name() string
}

// Run describes a single retention sweep for logging / observability.
type Run struct {
	StartedAt  time.Time
	FinishedAt time.Time
	Cutoff     time.Time
	Archived   int64
	Deleted    int64
	Skipped    bool // true when RetentionDays <= 0
}

// Service executes one retention sweep. It's safe to invoke RunOnce
// concurrently with Insert / List on the underlying audit store — the
// store methods acquire their own locks where needed.
type Service struct {
	store     Store
	archiver  Archiver
	days      int
	batchSize int
	nowFunc   func() time.Time
	logger    func(format string, v ...any)
}

// NewService wraps store with a retention service for the given
// RetentionDays. Callers may set an Archiver via SetArchiver before
// Start; when no archiver is wired the service performs a delete-only
// sweep.
func NewService(store Store, days int) *Service {
	return &Service{
		store:     store,
		days:      days,
		batchSize: DefaultBatchSize,
		nowFunc:   time.Now,
		logger:    log.Printf,
	}
}

// SetArchiver attaches the cold-storage sink. Pass nil to clear.
func (s *Service) SetArchiver(a Archiver) {
	s.archiver = a
}

// SetBatchSize overrides the archive-pagination page size. Values <= 0
// leave the current size unchanged.
func (s *Service) SetBatchSize(n int) {
	if n > 0 {
		s.batchSize = n
	}
}

// SetNowFunc injects a deterministic clock for tests.
func (s *Service) SetNowFunc(fn func() time.Time) {
	if fn != nil {
		s.nowFunc = fn
	}
}

// SetLogger overrides the default log.Printf-backed logger.
func (s *Service) SetLogger(fn func(format string, v ...any)) {
	if fn != nil {
		s.logger = fn
	}
}

// Days returns the configured retention window (in days).
func (s *Service) Days() int { return s.days }

// BatchSize returns the archive-pagination page size.
func (s *Service) BatchSize() int { return s.batchSize }

// ArchiverName returns the wired archiver's Name() or "" when none.
func (s *Service) ArchiverName() string {
	if s.archiver == nil {
		return ""
	}
	return s.archiver.Name()
}

// RunOnce performs a single archive-and-delete sweep. Returns a non-nil
// Run describing what happened AND an error if the sweep aborted
// mid-flight. When the archiver fails the DB rows are left in place;
// the next call will retry. Callers may safely ignore the error for
// fire-and-forget scheduling but should LOG it so operators notice
// persistent archive failures.
func (s *Service) RunOnce(ctx context.Context) (*Run, error) {
	start := s.nowFunc().UTC()
	run := &Run{StartedAt: start}
	if s.days <= 0 {
		run.Skipped = true
		run.FinishedAt = s.nowFunc().UTC()
		return run, nil
	}
	cutoff := start.Add(-time.Duration(s.days) * 24 * time.Hour)
	run.Cutoff = cutoff

	if s.archiver != nil {
		var cursor int64
		for {
			batch, err := s.store.ListBefore(ctx, cutoff, cursor, s.batchSize)
			if err != nil {
				run.FinishedAt = s.nowFunc().UTC()
				return run, fmt.Errorf("retention: list expired: %w", err)
			}
			if len(batch) == 0 {
				break
			}
			if err := s.archiver.Archive(ctx, batch); err != nil {
				run.FinishedAt = s.nowFunc().UTC()
				return run, fmt.Errorf("retention: archive via %s: %w",
					s.archiver.Name(), err)
			}
			run.Archived += int64(len(batch))
			cursor = batch[len(batch)-1].ChainSeq
			if len(batch) < s.batchSize {
				break
			}
		}
	}

	deleted, err := s.store.DeleteBefore(ctx, cutoff)
	if err != nil {
		run.FinishedAt = s.nowFunc().UTC()
		return run, fmt.Errorf("retention: delete expired: %w", err)
	}
	run.Deleted = deleted
	run.FinishedAt = s.nowFunc().UTC()
	return run, nil
}

// Scheduler runs RunOnce on a fixed interval. Follows the same
// Start(ctx)/Stop() lifecycle shape as RootHashPublisher and
// LDAPSyncScheduler — initial cycle fires immediately on Start so the
// backlog on a freshly-enabled deployment drains without waiting a
// full interval first.
type Scheduler struct {
	svc      *Service
	interval time.Duration

	mu     sync.Mutex
	stopCh chan struct{}
	doneCh chan struct{}
}

// NewScheduler wraps svc with a periodic loop. interval clamps to
// DefaultInterval when <= 0.
func NewScheduler(svc *Service, interval time.Duration) *Scheduler {
	if interval <= 0 {
		interval = DefaultInterval
	}
	return &Scheduler{svc: svc, interval: interval}
}

// Interval returns the loop's tick interval. Surfaced for boot-time
// log lines and admin status endpoints.
func (s *Scheduler) Interval() time.Duration { return s.interval }

// Start launches the periodic loop. Returns immediately; the loop
// exits when ctx is cancelled OR Stop is called. Idempotent — calling
// Start twice is a no-op once the loop is running.
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	if s.stopCh != nil {
		s.mu.Unlock()
		return
	}
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	s.stopCh = stopCh
	s.doneCh = doneCh
	s.mu.Unlock()

	// Capture stopCh/doneCh by value so Stop()'s nil-out of s.stopCh
	// can't race with the goroutine's select read — if Stop runs
	// before the goroutine's first select iteration, a shared-field
	// read would observe nil and the goroutine would hang forever.
	go func() {
		defer close(doneCh)
		s.runOnce(ctx)
		t := time.NewTicker(s.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stopCh:
				return
			case <-t.C:
				s.runOnce(ctx)
			}
		}
	}()
}

// Stop cancels the loop and waits for the in-flight sweep to drain.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	stopCh, doneCh := s.stopCh, s.doneCh
	s.stopCh = nil
	s.mu.Unlock()
	if stopCh != nil {
		close(stopCh)
	}
	if doneCh != nil {
		<-doneCh
	}
}

func (s *Scheduler) runOnce(ctx context.Context) {
	run, err := s.svc.RunOnce(ctx)
	if err != nil {
		s.svc.logger("[audit retention] sweep failed (archived=%d deleted=%d cutoff=%s): %v",
			run.Archived, run.Deleted, run.Cutoff.Format(time.RFC3339), err)
		return
	}
	if run.Skipped {
		return
	}
	s.svc.logger("[audit retention] sweep ok: archived=%d deleted=%d cutoff=%s via=%s",
		run.Archived, run.Deleted, run.Cutoff.Format(time.RFC3339), s.svc.ArchiverName())
}
