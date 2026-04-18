package export

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/liyang/weave/pkg/audit"
)

// RetryPolicy configures the retry-with-backoff loop BatchedExporter runs
// around each Export call. A zero RetryPolicy means MaxAttempts=1 (no
// retry) so mis-configured wirings don't silently hang on transient
// failures. Backoff grows as InitialBackoff * Multiplier^n, capped at
// MaxBackoff; Multiplier<=1 effectively disables exponential growth.
type RetryPolicy struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Multiplier     float64
}

// BatchedOptions controls the buffer size and retry behaviour of a
// BatchedExporter. BatchSize<=0 defaults to 1 (immediate flush on every
// Enqueue) so a missing configuration still makes forward progress.
type BatchedOptions struct {
	BatchSize int
	Retry     RetryPolicy
}

// BatchedExporter buffers events in memory and flushes to an underlying
// Exporter when BatchSize is reached OR Flush is called explicitly. Each
// flush goes through a bounded retry loop governed by RetryPolicy. The
// struct is safe for concurrent Enqueue/Flush calls.
type BatchedExporter struct {
	inner     Exporter
	batchSize int
	retry     RetryPolicy

	mu  sync.Mutex
	buf []audit.AuditEvent

	// sleepFunc is injectable for tests that want to observe the backoff
	// schedule without actually sleeping. nil = use a ctx-aware timer so
	// cancellation interrupts the wait.
	sleepFunc func(time.Duration)
}

// NewBatchedExporter wraps inner with a buffer + retry pipeline.
func NewBatchedExporter(inner Exporter, opts BatchedOptions) *BatchedExporter {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 1
	}
	if opts.Retry.MaxAttempts <= 0 {
		opts.Retry.MaxAttempts = 1
	}
	if opts.Retry.MaxBackoff < 0 {
		opts.Retry.MaxBackoff = 0
	}
	return &BatchedExporter{
		inner:     inner,
		batchSize: opts.BatchSize,
		retry:     opts.Retry,
	}
}

// Name identifies the batched wrapper plus its inner exporter so log /
// metric labels disambiguate "stdout vs batched-stdout".
func (b *BatchedExporter) Name() string {
	if b.inner == nil {
		return "batched"
	}
	return fmt.Sprintf("batched(%s)", b.inner.Name())
}

// Pending reports the number of buffered events not yet flushed. Test /
// observability hook — not part of the Exporter interface.
func (b *BatchedExporter) Pending() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.buf)
}

// Enqueue buffers a single event, flushing automatically once the buffer
// reaches BatchSize. Returns any error from the flush attempt (or nil if
// no flush was triggered).
func (b *BatchedExporter) Enqueue(ctx context.Context, evt audit.AuditEvent) error {
	b.mu.Lock()
	b.buf = append(b.buf, evt)
	shouldFlush := len(b.buf) >= b.batchSize
	b.mu.Unlock()
	if shouldFlush {
		return b.Flush(ctx)
	}
	return nil
}

// Flush drains the internal buffer through the underlying exporter. An
// empty buffer is a no-op. The buffer is cleared regardless of whether
// the flush succeeds or exhausts its retry budget, so callers won't
// hammer a misbehaving sink indefinitely with the same batch on the next
// Enqueue.
func (b *BatchedExporter) Flush(ctx context.Context) error {
	b.mu.Lock()
	if len(b.buf) == 0 {
		b.mu.Unlock()
		return nil
	}
	batch := b.buf
	b.buf = nil
	b.mu.Unlock()
	return b.exportWithRetry(ctx, batch)
}

// Export satisfies the Exporter interface so BatchedExporter can be
// composed with other middleware (multi-fanout, tee-exporter, ...). It
// flushes any pending buffered events first, then exports the provided
// batch through the retry pipeline without buffering (callers that
// pre-batched expect the batch to land atomically, not to be split at
// BatchSize).
func (b *BatchedExporter) Export(ctx context.Context, batch []audit.AuditEvent) error {
	if err := b.Flush(ctx); err != nil {
		return err
	}
	if len(batch) == 0 {
		return nil
	}
	return b.exportWithRetry(ctx, batch)
}

func (b *BatchedExporter) exportWithRetry(ctx context.Context, batch []audit.AuditEvent) error {
	var lastErr error
	attempts := 0
	for attempts < b.retry.MaxAttempts {
		attempts++
		err := b.inner.Export(ctx, batch)
		if err == nil {
			return nil
		}
		lastErr = err
		if attempts >= b.retry.MaxAttempts {
			break
		}
		if werr := b.waitBackoff(ctx, b.nextBackoff(attempts)); werr != nil {
			return werr
		}
	}
	name := ""
	if b.inner != nil {
		name = b.inner.Name()
	}
	return &ExportError{Exporter: name, Attempts: attempts, Err: lastErr}
}

// waitBackoff sleeps for d before the next retry. When a test supplied a
// sleepFunc we hand the duration straight to it (tests typically record
// without actually sleeping); otherwise we use a ctx-aware timer so
// cancellation interrupts the wait.
func (b *BatchedExporter) waitBackoff(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	if b.sleepFunc != nil {
		b.sleepFunc(d)
		return ctx.Err()
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

// nextBackoff returns the delay BEFORE the attempt-th retry. attempt=1
// means "first retry" i.e. after the initial failure.
func (b *BatchedExporter) nextBackoff(attempt int) time.Duration {
	base := b.retry.InitialBackoff
	if base <= 0 {
		return 0
	}
	d := base
	mult := b.retry.Multiplier
	if mult <= 1 {
		mult = 1
	}
	for i := 1; i < attempt; i++ {
		d = time.Duration(float64(d) * mult)
		if b.retry.MaxBackoff > 0 && d > b.retry.MaxBackoff {
			d = b.retry.MaxBackoff
			break
		}
	}
	if b.retry.MaxBackoff > 0 && d > b.retry.MaxBackoff {
		d = b.retry.MaxBackoff
	}
	return d
}
