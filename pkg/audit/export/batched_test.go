package export

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/audit"
)

func TestBatchedExporter_FlushAtBatchSize(t *testing.T) {
	inner := &flakyExporter{name: "test"}
	b := NewBatchedExporter(inner, BatchedOptions{BatchSize: 3})

	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := b.Enqueue(ctx, sampleEvent("e")); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
	}
	// Still under batch size.
	if inner.attempts != 0 {
		t.Fatalf("expected no flush yet, got %d attempts", inner.attempts)
	}
	if err := b.Enqueue(ctx, sampleEvent("e")); err != nil {
		t.Fatalf("Enqueue trigger: %v", err)
	}
	if inner.attempts != 1 {
		t.Fatalf("expected exactly 1 flush at batch size, got %d", inner.attempts)
	}
	if len(inner.lastBatch) != 3 {
		t.Fatalf("expected flushed batch of 3, got %d", len(inner.lastBatch))
	}
	// Buffer should be empty after flush.
	if got := b.Pending(); got != 0 {
		t.Fatalf("expected empty buffer, got %d pending", got)
	}
}

func TestBatchedExporter_FlushExplicit(t *testing.T) {
	inner := &flakyExporter{name: "test"}
	b := NewBatchedExporter(inner, BatchedOptions{BatchSize: 100})

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := b.Enqueue(ctx, sampleEvent("e")); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
	}
	if err := b.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if len(inner.lastBatch) != 5 {
		t.Fatalf("expected 5 events flushed, got %d", len(inner.lastBatch))
	}
	// Second Flush with empty buffer is a no-op.
	if err := b.Flush(ctx); err != nil {
		t.Fatalf("Flush empty: %v", err)
	}
	if inner.attempts != 1 {
		t.Fatalf("second Flush should not call inner; attempts=%d", inner.attempts)
	}
}

func TestBatchedExporter_RetriesTransientFailures(t *testing.T) {
	inner := &flakyExporter{name: "test", failures: 2}
	sleeps := []time.Duration{}
	b := NewBatchedExporter(inner, BatchedOptions{
		BatchSize: 1,
		Retry: RetryPolicy{
			MaxAttempts:    3,
			InitialBackoff: 10 * time.Millisecond,
			MaxBackoff:     1 * time.Second,
			Multiplier:     2,
		},
	})
	b.sleepFunc = func(d time.Duration) { sleeps = append(sleeps, d) }

	ctx := context.Background()
	if err := b.Enqueue(ctx, sampleEvent("a")); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if inner.attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", inner.attempts)
	}
	if len(sleeps) != 2 {
		t.Fatalf("expected 2 backoffs, got %d: %v", len(sleeps), sleeps)
	}
	if sleeps[0] != 10*time.Millisecond || sleeps[1] != 20*time.Millisecond {
		t.Fatalf("backoffs=%v want [10ms 20ms]", sleeps)
	}
	// Each attempt must receive the same batch content.
	if len(inner.batchCounts) != 3 || inner.batchCounts[0] != 1 {
		t.Fatalf("inconsistent batch counts: %v", inner.batchCounts)
	}
}

func TestBatchedExporter_ReturnsErrorAfterMaxAttempts(t *testing.T) {
	inner := &flakyExporter{name: "test", failures: 10}
	b := NewBatchedExporter(inner, BatchedOptions{
		BatchSize: 1,
		Retry: RetryPolicy{
			MaxAttempts:    3,
			InitialBackoff: time.Millisecond,
			MaxBackoff:     time.Millisecond,
		},
	})
	b.sleepFunc = func(d time.Duration) {}

	err := b.Enqueue(context.Background(), sampleEvent("a"))
	if err == nil {
		t.Fatalf("expected error after exhausting retries")
	}
	var ee *ExportError
	if !errors.As(err, &ee) {
		t.Fatalf("expected *ExportError, got %T: %v", err, err)
	}
	if ee.Attempts != 3 {
		t.Fatalf("Attempts=%d want 3", ee.Attempts)
	}
	// After failure, the buffer should be cleared so callers don't retry the
	// same bad batch indefinitely on the next Enqueue.
	if b.Pending() != 0 {
		t.Fatalf("expected buffer cleared after exhausted retries, got %d", b.Pending())
	}
}

func TestBatchedExporter_BackoffCappedAtMax(t *testing.T) {
	inner := &flakyExporter{name: "test", failures: 10}
	sleeps := []time.Duration{}
	b := NewBatchedExporter(inner, BatchedOptions{
		BatchSize: 1,
		Retry: RetryPolicy{
			MaxAttempts:    5,
			InitialBackoff: 100 * time.Millisecond,
			MaxBackoff:     150 * time.Millisecond,
			Multiplier:     10,
		},
	})
	b.sleepFunc = func(d time.Duration) { sleeps = append(sleeps, d) }

	_ = b.Enqueue(context.Background(), sampleEvent("a"))
	// 4 backoffs between 5 attempts. First is 100ms, subsequent capped at 150ms.
	if len(sleeps) != 4 {
		t.Fatalf("expected 4 backoffs, got %d", len(sleeps))
	}
	if sleeps[0] != 100*time.Millisecond {
		t.Fatalf("first backoff=%v want 100ms", sleeps[0])
	}
	for i := 1; i < 4; i++ {
		if sleeps[i] != 150*time.Millisecond {
			t.Fatalf("backoff[%d]=%v want capped at 150ms", i, sleeps[i])
		}
	}
}

func TestBatchedExporter_ContextCancelDuringBackoff(t *testing.T) {
	inner := &flakyExporter{name: "test", failures: 10}
	b := NewBatchedExporter(inner, BatchedOptions{
		BatchSize: 1,
		Retry: RetryPolicy{
			MaxAttempts:    5,
			InitialBackoff: time.Hour,
			MaxBackoff:     time.Hour,
		},
	})
	// Real sleep path — cancel ctx and confirm we abort the backoff.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := b.Enqueue(ctx, sampleEvent("a"))
	if err == nil {
		t.Fatalf("expected error on cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestBatchedExporter_ZeroRetryDefaultsToSingleAttempt(t *testing.T) {
	inner := &flakyExporter{name: "test", failures: 0}
	b := NewBatchedExporter(inner, BatchedOptions{BatchSize: 1})

	if err := b.Enqueue(context.Background(), sampleEvent("a")); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if inner.attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", inner.attempts)
	}
}

// Enqueue of an empty batch should be a no-op (defensive — not typically hit).
func TestBatchedExporter_InvalidBatchSizeDefaultsToOne(t *testing.T) {
	inner := &flakyExporter{name: "test"}
	b := NewBatchedExporter(inner, BatchedOptions{BatchSize: 0})
	if err := b.Enqueue(context.Background(), sampleEvent("a")); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	// With batch size defaulting to 1, enqueue triggers immediate flush.
	if inner.attempts != 1 {
		t.Fatalf("expected 1 flush at default batch=1, got %d", inner.attempts)
	}
}

func TestBatchedExporter_InnerExporterName(t *testing.T) {
	inner := &flakyExporter{name: "inner"}
	b := NewBatchedExporter(inner, BatchedOptions{BatchSize: 10})
	if got := b.Name(); got != "batched(inner)" {
		t.Fatalf("Name()=%q want batched(inner)", got)
	}
}

func TestBatchedExporter_FlushThroughExporter(t *testing.T) {
	// Export is the composable form: wrap Enqueue+Flush semantics so a
	// BatchedExporter can satisfy Exporter itself.
	inner := &flakyExporter{name: "inner"}
	b := NewBatchedExporter(inner, BatchedOptions{BatchSize: 10})

	err := b.Export(context.Background(), []audit.AuditEvent{
		sampleEvent("a"), sampleEvent("b"),
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	// Export should flush immediately regardless of batch size so callers
	// that pre-batch events don't see them trapped in the buffer.
	if inner.attempts != 1 {
		t.Fatalf("expected 1 flush via Export, got %d", inner.attempts)
	}
	if len(inner.lastBatch) != 2 {
		t.Fatalf("expected 2 events flushed, got %d", len(inner.lastBatch))
	}
}
