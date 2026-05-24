package actions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestBDD_WebhookSideEffect_RetryWithBackoff covers PRD-V2 Gap-A4
// (partial): the webhook dispatcher previously fired exactly once
// and the failure path just logged. Foundry side-effect dispatch
// retries transient failures (network errors, 5xx, 408, 429) with
// exponential backoff before giving up. Non-retryable failures
// (other 4xx) fail fast — they're caller bugs that retrying won't
// fix.
//
// Acceptance criteria (Given → When → Then):
//
//   Given a webhook target that responds 200 on first try
//   When  executeWebhookEffect runs
//   Then  it returns nil and the target is hit EXACTLY once
//
//   Given a webhook target that returns 503 twice then 200
//   When  executeWebhookEffect runs with MaxRetries >= 2
//   Then  it returns nil after 3 total calls
//
//   Given a webhook target that always returns 500
//   When  executeWebhookEffect runs with MaxRetries = 2
//   Then  it returns an error mentioning the attempt count, AND
//         the target is hit (1 + MaxRetries) = 3 times total
//
//   Given a webhook target that returns 400 Bad Request
//   When  executeWebhookEffect runs
//   Then  it fails fast with NO retries (4xx is a caller bug)
//
//   Given a webhook target that returns 429 Too Many Requests
//   When  executeWebhookEffect runs with MaxRetries = 1
//   Then  it retries — 429 is retryable per Foundry contract
//
// DLQ + action_logs.side_effect_status persistence are deferred to
// follow-up rounds; this round just adds the retry loop.
func TestBDD_WebhookSideEffect_RetryWithBackoff(t *testing.T) {
	t.Run("1st-attempt success: no retry, exactly one call", func(t *testing.T) {
		var calls int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			atomic.AddInt32(&calls, 1)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		err := executeWebhookEffect(
			mustWebhookConfig(t, webhookConfig{
				URL: srv.URL, MaxRetries: 5, RetryBackoffMilliseconds: 1,
			}),
			ActionResult{ActionRID: "rid-1"},
		)
		if err != nil {
			t.Fatalf("expected nil, got: %v", err)
		}
		if got := atomic.LoadInt32(&calls); got != 1 {
			t.Errorf("calls = %d, want 1 (no retry on 1st success)", got)
		}
	})

	t.Run("recovers after 2 transient 503s: 3 total calls", func(t *testing.T) {
		var calls int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			n := atomic.AddInt32(&calls, 1)
			if n <= 2 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		err := executeWebhookEffect(
			mustWebhookConfig(t, webhookConfig{
				URL: srv.URL, MaxRetries: 3, RetryBackoffMilliseconds: 1,
			}),
			ActionResult{ActionRID: "rid-2"},
		)
		if err != nil {
			t.Fatalf("expected nil after retry recovery, got: %v", err)
		}
		if got := atomic.LoadInt32(&calls); got != 3 {
			t.Errorf("calls = %d, want 3 (2 failures + 1 success)", got)
		}
	})

	t.Run("exhausted retries on persistent 500: returns error after MaxRetries+1 attempts", func(t *testing.T) {
		var calls int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			atomic.AddInt32(&calls, 1)
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		err := executeWebhookEffect(
			mustWebhookConfig(t, webhookConfig{
				URL: srv.URL, MaxRetries: 2, RetryBackoffMilliseconds: 1,
			}),
			ActionResult{ActionRID: "rid-3"},
		)
		if err == nil {
			t.Fatal("expected error after exhausted retries, got nil")
		}
		if !strings.Contains(err.Error(), "after") || !strings.Contains(err.Error(), "attempt") {
			t.Errorf("error = %q, want it to mention attempt count", err.Error())
		}
		if got := atomic.LoadInt32(&calls); got != 3 {
			t.Errorf("calls = %d, want 3 (1 initial + 2 retries)", got)
		}
	})

	t.Run("non-retryable 400 Bad Request: fail-fast, no retries", func(t *testing.T) {
		var calls int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			atomic.AddInt32(&calls, 1)
			w.WriteHeader(http.StatusBadRequest)
		}))
		defer srv.Close()

		err := executeWebhookEffect(
			mustWebhookConfig(t, webhookConfig{
				URL: srv.URL, MaxRetries: 5, RetryBackoffMilliseconds: 1,
			}),
			ActionResult{ActionRID: "rid-4"},
		)
		if err == nil {
			t.Fatal("expected error on 400, got nil")
		}
		if got := atomic.LoadInt32(&calls); got != 1 {
			t.Errorf("calls = %d, want 1 (4xx is a caller bug — no retries)", got)
		}
	})

	t.Run("429 Too Many Requests IS retryable", func(t *testing.T) {
		var calls int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			n := atomic.AddInt32(&calls, 1)
			if n == 1 {
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		err := executeWebhookEffect(
			mustWebhookConfig(t, webhookConfig{
				URL: srv.URL, MaxRetries: 1, RetryBackoffMilliseconds: 1,
			}),
			ActionResult{ActionRID: "rid-5"},
		)
		if err != nil {
			t.Fatalf("expected nil after 429-then-200 recovery, got: %v", err)
		}
		if got := atomic.LoadInt32(&calls); got != 2 {
			t.Errorf("calls = %d, want 2 (1 retry on 429)", got)
		}
	})

	t.Run("408 Request Timeout IS retryable", func(t *testing.T) {
		var calls int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			n := atomic.AddInt32(&calls, 1)
			if n == 1 {
				w.WriteHeader(http.StatusRequestTimeout)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		err := executeWebhookEffect(
			mustWebhookConfig(t, webhookConfig{
				URL: srv.URL, MaxRetries: 1, RetryBackoffMilliseconds: 1,
			}),
			ActionResult{ActionRID: "rid-6"},
		)
		if err != nil {
			t.Fatalf("expected nil after 408-then-200 recovery, got: %v", err)
		}
		if got := atomic.LoadInt32(&calls); got != 2 {
			t.Errorf("calls = %d, want 2 (1 retry on 408)", got)
		}
	})

	t.Run("default MaxRetries = 3 when unconfigured", func(t *testing.T) {
		// Backwards-compat behavior shifts: previously a failing webhook hit
		// once. Now it hits 1 + DEFAULT_MAX_RETRIES (3) = 4 times before
		// giving up. ExecuteSideEffects still swallows the final error
		// (best-effort top level) so the existing
		// TestExecuteSideEffects_Webhook_NonSuccess_BestEffort test stays
		// green; the new call-count guarantee is what differs.
		var calls int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			atomic.AddInt32(&calls, 1)
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		// MaxRetries omitted; RetryBackoffMilliseconds set to 1 to keep test fast.
		_ = executeWebhookEffect(
			mustWebhookConfig(t, webhookConfig{URL: srv.URL, RetryBackoffMilliseconds: 1}),
			ActionResult{ActionRID: "rid-default"},
		)
		if got := atomic.LoadInt32(&calls); got != 4 {
			t.Errorf("calls = %d, want 4 (1 initial + default 3 retries)", got)
		}
	})
}

func mustWebhookConfig(t *testing.T, cfg webhookConfig) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal webhookConfig: %v", err)
	}
	return b
}
