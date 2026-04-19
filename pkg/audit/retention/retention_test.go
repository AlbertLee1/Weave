package retention

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/audit"
)

// stubStore implements Store for unit tests. All methods honour the
// chain-seq-ordered semantics the real PG store guarantees.
type stubStore struct {
	mu     sync.Mutex
	events []audit.AuditEvent

	listErr   error
	deleteErr error

	listCalls   int
	deleteCalls int
}

func (s *stubStore) ListBefore(_ context.Context, before time.Time, cursor int64, limit int) ([]audit.AuditEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listCalls++
	if s.listErr != nil {
		return nil, s.listErr
	}
	var out []audit.AuditEvent
	for _, e := range s.events {
		if !e.Timestamp.Before(before) {
			continue
		}
		if e.ChainSeq <= cursor {
			continue
		}
		out = append(out, e)
		if len(out) >= limit && limit > 0 {
			break
		}
	}
	return out, nil
}

func (s *stubStore) DeleteBefore(_ context.Context, before time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteCalls++
	if s.deleteErr != nil {
		return 0, s.deleteErr
	}
	kept := s.events[:0]
	var removed int64
	for _, e := range s.events {
		if e.Timestamp.Before(before) {
			removed++
			continue
		}
		kept = append(kept, e)
	}
	s.events = kept
	return removed, nil
}

// recordingArchiver captures every batch for assertions.
type recordingArchiver struct {
	mu       sync.Mutex
	batches  [][]audit.AuditEvent
	name     string
	failOn   int // fail the Nth call (1-based); 0 = never fail
	failWith error
	calls    int
}

func (a *recordingArchiver) Archive(_ context.Context, batch []audit.AuditEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	if a.failOn > 0 && a.calls == a.failOn {
		return a.failWith
	}
	cp := make([]audit.AuditEvent, len(batch))
	copy(cp, batch)
	a.batches = append(a.batches, cp)
	return nil
}

func (a *recordingArchiver) Name() string {
	if a.name == "" {
		return "recording"
	}
	return a.name
}

func seedEvents(n int, base time.Time) []audit.AuditEvent {
	out := make([]audit.AuditEvent, n)
	for i := 0; i < n; i++ {
		out[i] = audit.AuditEvent{
			ID:        fmt.Sprintf("evt-%03d", i+1),
			Action:    "test.action",
			ChainSeq:  int64(i + 1),
			Timestamp: base.Add(time.Duration(i) * time.Hour),
		}
	}
	return out
}

func TestRunOnce_DaysZero_Skipped(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := &stubStore{events: seedEvents(5, base)}

	svc := NewService(store, 0)
	svc.SetNowFunc(func() time.Time { return base.Add(100 * time.Hour) })

	run, err := svc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce error = %v", err)
	}
	if !run.Skipped {
		t.Fatalf("expected Skipped=true when RetentionDays<=0")
	}
	if store.deleteCalls != 0 {
		t.Fatalf("expected no DeleteBefore calls, got %d", store.deleteCalls)
	}
}

func TestRunOnce_NoArchiver_DeletesExpired(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	events := seedEvents(10, base) // ts: base, base+1h, ..., base+9h
	store := &stubStore{events: events}

	svc := NewService(store, 1)
	// now = base + 50h ⇒ cutoff = base + 50h - 24h = base + 26h
	svc.SetNowFunc(func() time.Time { return base.Add(50 * time.Hour) })

	run, err := svc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce error = %v", err)
	}
	if run.Archived != 0 {
		t.Fatalf("expected 0 archived with no archiver, got %d", run.Archived)
	}
	if run.Deleted != 10 {
		t.Fatalf("expected 10 deleted (all older than cutoff), got %d", run.Deleted)
	}
	if store.listCalls != 0 {
		t.Fatalf("expected no ListBefore calls without archiver, got %d", store.listCalls)
	}
}

func TestRunOnce_WithArchiver_ArchivesThenDeletes(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	events := seedEvents(7, base)
	store := &stubStore{events: events}
	arch := &recordingArchiver{}

	svc := NewService(store, 1)
	svc.SetArchiver(arch)
	svc.SetBatchSize(3)
	// cutoff includes all 7 events (last ts = base + 6h < base + 48h - 24h)
	svc.SetNowFunc(func() time.Time { return base.Add(48 * time.Hour) })

	run, err := svc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce error = %v", err)
	}
	if run.Archived != 7 {
		t.Fatalf("Archived=%d want 7", run.Archived)
	}
	if run.Deleted != 7 {
		t.Fatalf("Deleted=%d want 7", run.Deleted)
	}
	if len(arch.batches) != 3 {
		t.Fatalf("expected 3 batches (3+3+1), got %d", len(arch.batches))
	}
	// Every event is archived exactly once, preserving chain_seq order.
	var seen []int64
	for _, b := range arch.batches {
		for _, e := range b {
			seen = append(seen, e.ChainSeq)
		}
	}
	if len(seen) != 7 {
		t.Fatalf("archived total=%d want 7", len(seen))
	}
	for i := 0; i < len(seen); i++ {
		if seen[i] != int64(i+1) {
			t.Fatalf("archive order broken at %d: got %d want %d", i, seen[i], i+1)
		}
	}
}

func TestRunOnce_ArchiverFailure_SkipsDelete(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	events := seedEvents(5, base)
	store := &stubStore{events: events}
	arch := &recordingArchiver{failOn: 1, failWith: errors.New("s3 flaky")}

	svc := NewService(store, 1)
	svc.SetArchiver(arch)
	svc.SetBatchSize(10)
	svc.SetNowFunc(func() time.Time { return base.Add(100 * time.Hour) })

	run, err := svc.RunOnce(context.Background())
	if err == nil {
		t.Fatalf("expected error when archiver fails")
	}
	if !errors.Is(err, arch.failWith) {
		t.Fatalf("error = %v, want wrapped %v", err, arch.failWith)
	}
	if run.Deleted != 0 {
		t.Fatalf("Deleted=%d, want 0 — archive failure MUST NOT trigger delete", run.Deleted)
	}
	if store.deleteCalls != 0 {
		t.Fatalf("expected 0 DeleteBefore calls on archive failure, got %d", store.deleteCalls)
	}
	// DB rows must still be present for retry on the next sweep.
	if len(store.events) != 5 {
		t.Fatalf("expected events to remain after failed archive, got %d", len(store.events))
	}
}

func TestRunOnce_PartialCutoff_OnlyExpiredEvicted(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// 10 events: ts = base, base+12h, ..., base+108h (each 12h apart).
	events := make([]audit.AuditEvent, 10)
	for i := range events {
		events[i] = audit.AuditEvent{
			ID:        fmt.Sprintf("evt-%02d", i),
			ChainSeq:  int64(i + 1),
			Timestamp: base.Add(time.Duration(i) * 12 * time.Hour),
		}
	}
	store := &stubStore{events: events}
	arch := &recordingArchiver{}

	svc := NewService(store, 2) // retain last 48h
	svc.SetArchiver(arch)
	svc.SetBatchSize(100)
	// now = base + 108h + 1h ⇒ cutoff = base + 109h - 48h = base + 61h
	// So events at ts < base+61h are expired: indices 0..5 (ts 0, 12, 24, 36, 48, 60).
	svc.SetNowFunc(func() time.Time { return base.Add(109 * time.Hour) })

	run, err := svc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce error = %v", err)
	}
	if run.Archived != 6 || run.Deleted != 6 {
		t.Fatalf("Archived=%d Deleted=%d, want 6/6", run.Archived, run.Deleted)
	}
	if len(store.events) != 4 {
		t.Fatalf("remaining store size=%d, want 4", len(store.events))
	}
	// Surviving rows are the 4 most-recent.
	for i, e := range store.events {
		if e.ChainSeq != int64(7+i) {
			t.Fatalf("surviving[%d].ChainSeq=%d, want %d", i, e.ChainSeq, 7+i)
		}
	}
}

func TestRunOnce_EmptyStore_NoError(t *testing.T) {
	store := &stubStore{}
	svc := NewService(store, 1)
	svc.SetArchiver(&recordingArchiver{})
	svc.SetNowFunc(func() time.Time { return time.Now() })

	run, err := svc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce on empty store = %v", err)
	}
	if run.Archived != 0 || run.Deleted != 0 {
		t.Fatalf("empty sweep Archived=%d Deleted=%d, want 0/0", run.Archived, run.Deleted)
	}
}

func TestRunOnce_ListFailure_ReturnsError(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := &stubStore{events: seedEvents(3, base), listErr: errors.New("pg down")}

	svc := NewService(store, 1)
	svc.SetArchiver(&recordingArchiver{})
	svc.SetNowFunc(func() time.Time { return base.Add(100 * time.Hour) })

	_, err := svc.RunOnce(context.Background())
	if err == nil {
		t.Fatalf("expected error when ListBefore fails")
	}
	if store.deleteCalls != 0 {
		t.Fatalf("expected no DeleteBefore when list fails, got %d", store.deleteCalls)
	}
}

func TestRunOnce_DeleteFailure_ReturnsError(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := &stubStore{events: seedEvents(3, base), deleteErr: errors.New("pg down")}

	svc := NewService(store, 1)
	svc.SetNowFunc(func() time.Time { return base.Add(100 * time.Hour) })

	_, err := svc.RunOnce(context.Background())
	if err == nil {
		t.Fatalf("expected error when DeleteBefore fails")
	}
}

func TestNewService_Defaults(t *testing.T) {
	svc := NewService(&stubStore{}, 30)
	if svc.Days() != 30 {
		t.Errorf("Days()=%d want 30", svc.Days())
	}
	if svc.BatchSize() != DefaultBatchSize {
		t.Errorf("BatchSize()=%d want %d", svc.BatchSize(), DefaultBatchSize)
	}
	if svc.ArchiverName() != "" {
		t.Errorf("ArchiverName()=%q, want empty without wired archiver", svc.ArchiverName())
	}
}

func TestService_SetBatchSize_IgnoresNonPositive(t *testing.T) {
	svc := NewService(&stubStore{}, 1)
	svc.SetBatchSize(250)
	if svc.BatchSize() != 250 {
		t.Fatalf("BatchSize()=%d want 250", svc.BatchSize())
	}
	svc.SetBatchSize(0)
	if svc.BatchSize() != 250 {
		t.Fatalf("BatchSize()=%d after no-op set(0), want 250", svc.BatchSize())
	}
	svc.SetBatchSize(-5)
	if svc.BatchSize() != 250 {
		t.Fatalf("BatchSize()=%d after no-op set(-5), want 250", svc.BatchSize())
	}
}

func TestScheduler_StartStop_Idempotent(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := &stubStore{events: seedEvents(3, base)}
	svc := NewService(store, 1)
	svc.SetNowFunc(func() time.Time { return base.Add(100 * time.Hour) })
	svc.SetLogger(func(string, ...any) {}) // silence

	sched := NewScheduler(svc, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sched.Start(ctx)
	sched.Start(ctx) // second call should be a no-op
	// Wait up to 1s for the initial sweep to run.
	deadline := time.After(1 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("scheduler did not run within 1s")
		default:
		}
		store.mu.Lock()
		calls := store.deleteCalls
		store.mu.Unlock()
		if calls >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	sched.Stop()
	sched.Stop() // second Stop is idempotent.
}

func TestScheduler_NewScheduler_DefaultInterval(t *testing.T) {
	sched := NewScheduler(NewService(&stubStore{}, 1), 0)
	if sched.Interval() != DefaultInterval {
		t.Fatalf("default interval=%v, want %v", sched.Interval(), DefaultInterval)
	}
}
