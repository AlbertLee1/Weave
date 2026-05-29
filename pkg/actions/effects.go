package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// sideEffectTracerName is the instrumentation library name for the
// outbound side-effect dispatcher. Dashboards can group spans by this
// name to isolate webhook traffic from the function-dispatcher spans
// (round 52) that share the same package.
const sideEffectTracerName = "github.com/liyang/weave/pkg/actions/sideeffects"

// SideEffect defines an effect triggered after successful action execution.
type SideEffect struct {
	Type   string          `json:"type"` // "webhook", "log"
	Config json.RawMessage `json:"config"`
}

// ActionResult carries the result data passed to side effects.
type ActionResult struct {
	ActionRID string                 `json:"actionRid"`
	BatchID   string                 `json:"batchId"`
	Edits     interface{}            `json:"edits,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// webhookConfig is the config for the "webhook" side effect type.
type webhookConfig struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	// TimeoutSeconds controls the HTTP client timeout (default 10s).
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
	// MaxRetries caps the number of retry attempts AFTER the initial call
	// for transient failures (network errors, 5xx, 408, 429). Total call
	// count on persistent failure is 1 + MaxRetries. Zero or negative
	// values fall through to the package default (defaultMaxRetries = 3),
	// matching Foundry's "transient errors are retried" wire contract.
	MaxRetries int `json:"maxRetries,omitempty"`
	// RetryBackoffMilliseconds is the base delay between retry attempts.
	// Each subsequent attempt waits 2^attempt × base (exponential), capped
	// at maxRetryBackoffMilliseconds. Zero or negative falls back to
	// defaultRetryBackoffMilliseconds = 100.
	RetryBackoffMilliseconds int `json:"retryBackoffMilliseconds,omitempty"`
}

// Webhook retry defaults. Picked to match common production conventions:
// 3 retries gives ~700ms tail latency on persistent failure while almost
// always covering single-node restarts or transient blips.
const (
	defaultMaxRetries               = 3
	defaultRetryBackoffMilliseconds = 100
	maxRetryBackoffMilliseconds     = 5_000
)

// SideEffectOutcome is the per-effect result the dispatcher emits.
// Round-32 will marshal a []SideEffectOutcome into action_logs's
// side_effect_status JSONB column so the persisted action history
// surfaces side-effect status (success / retry-recovered / failed
// after retries / non-retryable / unknown effect type) without
// needing a parallel log scrape.
type SideEffectOutcome struct {
	// Type echoes back the effect's declared type. Even when the type
	// is unknown to this dispatcher the original string is preserved
	// so the persisted record explains what was misconfigured.
	Type string `json:"type"`
	// Status taxonomy — see the SideEffectStatus* constants below.
	Status string `json:"status"`
	// Attempts is the number of dispatch attempts performed. 0 when
	// the effect type was unrecognized and no dispatch happened. 1 for
	// log effects, 1..(MaxRetries+1) for webhooks.
	Attempts int `json:"attempts"`
	// Error is the final error message on a non-success outcome.
	// Empty when Status == success. Carries the wrapped
	// "gave up after N attempts" or "non-retryable failure" envelope
	// from executeWebhookEffect so downstream consumers can extract
	// retry context.
	Error string `json:"error,omitempty"`
	// DurationMs is the total dispatch wall-clock time, including
	// every retry's backoff sleep. Useful for per-effect SLO tracking
	// once Round 32 wires this into action_logs.
	DurationMs int64 `json:"durationMs"`
}

// Side-effect status taxonomy. Stored verbatim in the JSON outcome so
// downstream consumers (action_logs, future DLQ, dashboards) can route
// on stable strings instead of inspecting the error message.
const (
	SideEffectStatusSuccess      = "success"       // dispatched (possibly after retries)
	SideEffectStatusFailed       = "failed"        // retry budget exhausted on transient failure
	SideEffectStatusNonRetryable = "non_retryable" // 4xx other than 408/429 (caller bug)
	SideEffectStatusUnknownType  = "unknown_type"  // dispatcher does not recognize effect.Type
)

// ExecuteSideEffects executes side effects after a successful action.
// Execution is best-effort: per-effect failures are logged but not
// surfaced to the caller. An empty or nil effects JSON is a no-op.
//
// Round 31 keeps this signature for backwards compatibility — existing
// call sites in executor.go don't need to change. New callers that
// want the structured per-effect outcomes (for action_logs
// persistence, DLQ routing, or per-effect SLO tracking) should use
// `ExecuteSideEffectsWithOutcomes` instead.
func ExecuteSideEffects(effectsJSON json.RawMessage, result ActionResult) error {
	_, _, err := ExecuteSideEffectsWithOutcomes(effectsJSON, result)
	return err
}

// ExecuteSideEffectsCtx is the context-aware companion of
// ExecuteSideEffects (round 53). New callers should prefer this form
// so the outbound webhook span attaches under the upstream action
// span and the receiver sees a connected trace.
func ExecuteSideEffectsCtx(ctx context.Context, effectsJSON json.RawMessage, result ActionResult) error {
	_, _, err := ExecuteSideEffectsWithOutcomesCtx(ctx, effectsJSON, result)
	return err
}

// ExecuteSideEffectsWithOutcomes is the structured-outcome variant of
// ExecuteSideEffects. It returns one SideEffectOutcome per declared
// effect AND the parsed []SideEffect slice (so callers that need the
// original effect config — e.g. the round-33 DLQ writer — don't have
// to re-parse effectsJSON). Callers should persist the outcomes into
// action_logs.side_effect_status, route failed-after-retries effects
// to a DLQ, or surface per-effect status on a Foundry-style action
// detail page.
//
// Returns (nil, nil, nil) when effectsJSON is empty / null / [].
// Returns (nil, nil, error) only when the effects JSON itself is
// malformed (parse failure) — per-effect dispatch failures are
// recorded in the outcomes and DO NOT propagate as an error, so a
// single bad effect never aborts the rest of the array.
//
// effects[i] and outcomes[i] are index-aligned: callers can use the
// same index to attribute an outcome back to its source SideEffect.
func ExecuteSideEffectsWithOutcomes(effectsJSON json.RawMessage, result ActionResult) ([]SideEffectOutcome, []SideEffect, error) {
	return ExecuteSideEffectsWithOutcomesCtx(context.Background(), effectsJSON, result)
}

// ExecuteSideEffectsWithOutcomesCtx is the context-aware companion
// of ExecuteSideEffectsWithOutcomes (round 53). It threads ctx into
// the webhook dispatcher so the outbound HTTP attempt uses
// NewRequestWithContext (inherits cancellation from the calling
// action) and the global propagator injects the active span's
// TraceContext into the outbound headers. Legacy callers without a
// context can still use ExecuteSideEffectsWithOutcomes; that shim
// passes context.Background and the webhook spans become root spans.
func ExecuteSideEffectsWithOutcomesCtx(ctx context.Context, effectsJSON json.RawMessage, result ActionResult) ([]SideEffectOutcome, []SideEffect, error) {
	if len(effectsJSON) == 0 || string(effectsJSON) == "null" || string(effectsJSON) == "[]" {
		return nil, nil, nil
	}

	// Support either a single effect object or an array of effects.
	var effects []SideEffect
	if effectsJSON[0] == '[' {
		if err := json.Unmarshal(effectsJSON, &effects); err != nil {
			return nil, nil, fmt.Errorf("parse side effects: %w", err)
		}
	} else {
		var single SideEffect
		if err := json.Unmarshal(effectsJSON, &single); err != nil {
			return nil, nil, fmt.Errorf("parse side effects: %w", err)
		}
		effects = []SideEffect{single}
	}

	outcomes := make([]SideEffectOutcome, 0, len(effects))
	for _, e := range effects {
		outcome := dispatchSingleEffectCtx(ctx, e, result)
		outcomes = append(outcomes, outcome)
		if outcome.Status != SideEffectStatusSuccess {
			log.Printf("side effect %q %s (attempts=%d): %s",
				outcome.Type, outcome.Status, outcome.Attempts, outcome.Error)
		}
	}
	return outcomes, effects, nil
}

// ReplaySideEffect runs a single side effect and returns its outcome
// — the public entry point the admin replay handler (Gap-A4 round 35)
// uses to re-dispatch a DLQ row. Caller is responsible for passing
// the snapshotted effect_config (stored on the DLQ row) and a
// reconstructed ActionResult (built from the linked action_logs row).
// Outcome shape matches the round-31 contract so the same
// action_logs.side_effect_status + DLQ writer code paths can record
// the result.
func ReplaySideEffect(e SideEffect, result ActionResult) SideEffectOutcome {
	return dispatchSingleEffectCtx(context.Background(), e, result)
}

// dispatchSingleEffect runs a single effect and returns its outcome.
// Per-effect failures populate outcome.Error and outcome.Status — never
// returned as a Go error, because the caller's per-effect isolation
// guarantee depends on this method being non-panicking.
//
//nolint:unused // backward-compat wrapper retained for downstream callers
func dispatchSingleEffect(e SideEffect, result ActionResult) SideEffectOutcome {
	return dispatchSingleEffectCtx(context.Background(), e, result)
}

// dispatchSingleEffectCtx is the ctx-aware dispatch path used by the
// round-53 tracing wiring. The ctx flows into the webhook attempt
// loop so each outbound HTTP call uses NewRequestWithContext and the
// active span's TraceContext is propagated.
func dispatchSingleEffectCtx(ctx context.Context, e SideEffect, result ActionResult) SideEffectOutcome {
	started := time.Now()
	out := SideEffectOutcome{Type: e.Type}

	switch e.Type {
	case "webhook":
		attempts, classification, err := executeWebhookEffectTrackedCtx(ctx, e.Config, result)
		out.Attempts = attempts
		out.Status = classification
		if err != nil {
			out.Error = err.Error()
		}
	case "log":
		out.Attempts = 1
		if err := executeLogEffect(result); err != nil {
			out.Status = SideEffectStatusFailed
			out.Error = err.Error()
		} else {
			out.Status = SideEffectStatusSuccess
		}
	default:
		out.Status = SideEffectStatusUnknownType
		out.Error = fmt.Sprintf("unknown side effect type: %q", e.Type)
	}

	out.DurationMs = time.Since(started).Milliseconds()
	return out
}

// executeWebhookEffect is the legacy single-error entry point preserved
// for the round-30 retry BDD coverage and the existing round-30
// effects_retry_bdd_test.go. New callers (and round 31's outcome
// dispatcher) use executeWebhookEffectTracked, which additionally
// returns the attempt count and a classification string.
func executeWebhookEffect(configJSON json.RawMessage, result ActionResult) error {
	_, _, err := executeWebhookEffectTracked(configJSON, result)
	return err
}

// executeWebhookEffectTrackedCtx is the ctx-aware variant the round-
// 53 dispatcher uses. The ctx flows into doWebhookAttempt so the
// outbound HTTP request inherits cancellation AND carries the active
// span's W3C TraceContext.
func executeWebhookEffectTrackedCtx(ctx context.Context, configJSON json.RawMessage, result ActionResult) (int, string, error) {
	return executeWebhookEffectTrackedImpl(ctx, configJSON, result)
}

// executeWebhookEffectTracked runs the webhook retry loop and returns
// (attempts, status, err). status is one of:
//
//   - SideEffectStatusSuccess      — at least one attempt succeeded
//   - SideEffectStatusFailed       — retry budget exhausted on transient failure
//   - SideEffectStatusNonRetryable — fail-fast 4xx (caller bug)
//
// err is non-nil iff status != success. attempts is the actual number
// of HTTP calls made (1 on immediate success, up to 1+MaxRetries on
// exhaustion). Config-parse / URL-validation / body-marshal failures
// surface as attempts=0 status=non_retryable — these block dispatch
// before any HTTP call.
func executeWebhookEffectTracked(configJSON json.RawMessage, result ActionResult) (int, string, error) {
	return executeWebhookEffectTrackedImpl(context.Background(), configJSON, result)
}

// executeWebhookEffectTrackedImpl is the shared retry-loop body. ctx
// is threaded into each attempt so doWebhookAttempt can both bind
// the per-attempt request to ctx (inherits cancellation) AND let the
// global propagator inject the active span's TraceContext into the
// outbound headers.
func executeWebhookEffectTrackedImpl(ctx context.Context, configJSON json.RawMessage, result ActionResult) (int, string, error) {
	var cfg webhookConfig
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return 0, SideEffectStatusNonRetryable, fmt.Errorf("webhook: invalid config: %w", err)
	}
	if cfg.URL == "" {
		return 0, SideEffectStatusNonRetryable, fmt.Errorf("webhook: url is required")
	}

	body, err := json.Marshal(result)
	if err != nil {
		return 0, SideEffectStatusNonRetryable, fmt.Errorf("webhook: marshal result: %w", err)
	}

	timeout := 10 * time.Second
	if cfg.TimeoutSeconds > 0 {
		timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
	}
	client := &http.Client{Timeout: timeout}

	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}
	backoffMs := cfg.RetryBackoffMilliseconds
	if backoffMs <= 0 {
		backoffMs = defaultRetryBackoffMilliseconds
	}

	// Retry loop. Total attempts = 1 + maxRetries. Retryable failures
	// (network error, 5xx, 408, 429) trigger exponential backoff
	// (2^attempt × backoffMs, capped). Non-retryable failures (other 4xx
	// = caller bugs) fail immediately so misconfigured webhooks surface
	// to oncall instead of silently consuming retry budget.
	var lastErr error
	totalAttempts := 1 + maxRetries
	attemptsMade := 0
	for attempt := 0; attempt < totalAttempts; attempt++ {
		if attempt > 0 {
			delay := time.Duration(backoffMs*(1<<uint(attempt-1))) * time.Millisecond
			if delay > time.Duration(maxRetryBackoffMilliseconds)*time.Millisecond {
				delay = time.Duration(maxRetryBackoffMilliseconds) * time.Millisecond
			}
			time.Sleep(delay)
		}

		attemptsMade++
		statusCode, attemptErr := doWebhookAttemptCtx(ctx, client, cfg, body, attemptsMade)
		if attemptErr == nil {
			return attemptsMade, SideEffectStatusSuccess, nil
		}
		lastErr = attemptErr

		// 4xx other than 408/429 is a caller bug — no amount of retrying
		// will fix a bad URL, missing auth header, or malformed payload.
		if !isRetryableStatus(statusCode) {
			return attemptsMade, SideEffectStatusNonRetryable,
				fmt.Errorf("webhook: non-retryable failure on attempt 1: %w", attemptErr)
		}
	}
	return attemptsMade, SideEffectStatusFailed,
		fmt.Errorf("webhook: gave up after %d attempts: %w", totalAttempts, lastErr)
}

// doWebhookAttempt makes one HTTP call. Returns (statusCode, error).
// statusCode is 0 when the request failed before getting a response
// (network error / timeout) — treated as retryable. Legacy ctx-less
// shim retained so older tests / call sites that don't have a context
// keep compiling; it delegates to doWebhookAttemptCtx with
// context.Background and attempt=1.
//
//nolint:unused // backward-compat wrapper retained for downstream callers
func doWebhookAttempt(client *http.Client, cfg webhookConfig, body []byte) (int, error) {
	return doWebhookAttemptCtx(context.Background(), client, cfg, body, 1)
}

// doWebhookAttemptCtx is the ctx-aware single-attempt dispatcher.
// Round 53 added it so the per-attempt request can:
//   - be built via http.NewRequestWithContext (inherits cancellation
//     from the caller's ctx, e.g. action-level deadline),
//   - have the active span's W3C TraceContext injected into outbound
//     headers AFTER caller-supplied Headers (so callers cannot
//     accidentally clobber traceparent),
//   - run inside a per-attempt client-kind span named
//     "sideeffect.webhook" with http.method / http.url /
//     http.status_code / sideeffect.attempt attributes — letting
//     dashboards count attempts and flag retry storms.
//
// attempt is 1-based so the attribute matches how operators count
// (i.e. "the 3rd attempt failed", not "attempt 2").
func doWebhookAttemptCtx(ctx context.Context, client *http.Client, cfg webhookConfig, body []byte, attempt int) (int, error) {
	tracer := otel.GetTracerProvider().Tracer(sideEffectTracerName)
	spanCtx, span := tracer.Start(ctx, "sideeffect.webhook",
		oteltrace.WithSpanKind(oteltrace.SpanKindClient),
		oteltrace.WithAttributes(
			attribute.String("http.method", http.MethodPost),
			attribute.String("http.url", cfg.URL),
			attribute.Int("sideeffect.attempt", attempt),
		),
	)
	defer span.End()

	req, err := http.NewRequestWithContext(spanCtx, http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		span.SetStatus(codes.Error, "build request")
		span.RecordError(err)
		return 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}
	// Inject AFTER caller-supplied headers so traceparent always
	// reflects the live span context — matches the round-52
	// HTTPDispatcher invariant.
	otel.GetTextMapPropagator().Inject(spanCtx, propagation.HeaderCarrier(req.Header))

	resp, err := client.Do(req)
	if err != nil {
		span.SetStatus(codes.Error, "transport")
		span.RecordError(err)
		// Transport-level error — no status code yet. Always retryable.
		return 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	span.SetAttributes(attribute.String("http.status_code", strconv.Itoa(resp.StatusCode)))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 5xx / 408 / 429 are the transient set that retries will
		// likely fix — still report Error on the span so head-based
		// samplers keep the failing attempt visible. 4xx caller bugs
		// stay Unset per OTel semantic conventions.
		if resp.StatusCode >= 500 {
			span.SetStatus(codes.Error, http.StatusText(resp.StatusCode))
		}
		return resp.StatusCode, fmt.Errorf("non-2xx response: %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

// isRetryableStatus reports whether an HTTP status code (or 0 for a
// transport-level error) should be retried per Foundry's side-effect
// dispatch contract. 5xx, 408, and 429 are transient; other 4xx are
// caller bugs and fail fast.
func isRetryableStatus(statusCode int) bool {
	if statusCode == 0 {
		return true // network / timeout
	}
	if statusCode >= 500 && statusCode < 600 {
		return true
	}
	if statusCode == http.StatusRequestTimeout {
		return true // 408
	}
	if statusCode == http.StatusTooManyRequests {
		return true // 429
	}
	return false
}

func executeLogEffect(result ActionResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("log: marshal result: %w", err)
	}
	log.Printf("action side-effect log: %s", string(data))
	return nil
}
