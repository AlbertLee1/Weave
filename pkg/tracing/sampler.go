// US-439: head-based sampling with force-sample carve-outs for slow and
// errored spans. The implementation is a SpanProcessor wrapper rather
// than an OTel SDK Sampler because the sampling carve-outs key off
// runtime-derived properties (status code, span duration) that the
// standard Sampler interface decides BEFORE the span has run. Recording
// every span and filtering at OnEnd is the canonical pattern for
// "tail-sampling within a single process" in the OTel ecosystem.
//
// Hot-path cost is bounded:
//   - rate=1.0 (the default in dev) bypasses the random-decision branch
//   - rate=0.0 still keeps slow + error spans, so production deployments
//     paying the export cost only on outliers stay observable
package tracing

import (
	"context"
	"encoding/binary"
	"math"
	"time"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// samplingProcessor wraps an underlying sdktrace.SpanProcessor and
// forwards OnEnd only for spans the sampling rules accept.
type samplingProcessor struct {
	next sdktrace.SpanProcessor

	// rate is the head-sample probability for non-error / non-slow
	// spans, clamped to [0, 1].
	rate float64

	// slow is the duration above which a span is force-sampled
	// regardless of rate. Zero disables the slow-request carve-out.
	slow time.Duration

	// threshold is the precomputed uint64 ceiling derived from rate so
	// the OnEnd hot path is one uint64 comparison.
	threshold uint64
}

// newSamplingProcessor wraps next with the head-based sampler. Negative
// rates clamp to 0; rates above 1 clamp to 1 (always sample).
func newSamplingProcessor(next sdktrace.SpanProcessor, rate float64, slow time.Duration) *samplingProcessor {
	if rate < 0 {
		rate = 0
	}
	if rate > 1 {
		rate = 1
	}
	if slow < 0 {
		slow = 0
	}
	return &samplingProcessor{
		next:      next,
		rate:      rate,
		slow:      slow,
		threshold: uint64(math.Floor(rate * float64(math.MaxUint64))),
	}
}

// OnStart forwards every start so attributes / events are still
// recorded — the filter only kicks in on OnEnd.
func (p *samplingProcessor) OnStart(ctx context.Context, s sdktrace.ReadWriteSpan) {
	p.next.OnStart(ctx, s)
}

// OnEnd applies the head-sample rule, with force-keep for slow / error
// spans. Spans rejected by the filter are silently dropped (the parent
// processor never sees them, so no exporter bandwidth is spent).
func (p *samplingProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	if !p.shouldExport(s) {
		return
	}
	p.next.OnEnd(s)
}

func (p *samplingProcessor) Shutdown(ctx context.Context) error {
	return p.next.Shutdown(ctx)
}

func (p *samplingProcessor) ForceFlush(ctx context.Context) error {
	return p.next.ForceFlush(ctx)
}

func (p *samplingProcessor) shouldExport(s sdktrace.ReadOnlySpan) bool {
	// rate=1.0 is the dev / debug default — keep every span, no
	// per-span work.
	if p.rate >= 1.0 {
		return true
	}
	// Force-keep error spans so a 1% sample rate in production still
	// surfaces every 5xx for the on-call dashboard.
	if s.Status().Code == codes.Error {
		return true
	}
	// Force-keep slow spans (>p.slow) for the same reason — latency
	// outliers must be observable even when the steady-state sample
	// rate is fractional.
	if p.slow > 0 && s.EndTime().Sub(s.StartTime()) > p.slow {
		return true
	}
	if p.rate <= 0 {
		return false
	}
	return traceIDPasses(s.SpanContext().TraceID(), p.threshold)
}

// traceIDPasses returns true when the trace ID falls below the
// precomputed sample threshold. The upper 64 bits of the trace ID are
// uniformly distributed (W3C requires the SDK to populate them with
// crypto-random bytes), which is why this is the canonical OTel
// trace-ID-ratio shape.
func traceIDPasses(tid trace.TraceID, threshold uint64) bool {
	if threshold == 0 {
		return false
	}
	if threshold == math.MaxUint64 {
		return true
	}
	return binary.BigEndian.Uint64(tid[0:8]) < threshold
}
