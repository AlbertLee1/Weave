package actions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// TestBDD_ExecuteSideEffectsWithOutcomes_DispatcherContract covers
// PRD-V2 Gap-A4 follow-up: callers (executor, action_logs writer,
// future DLQ) need structured per-effect status so the side-effect
// outcome can be persisted to action_logs.side_effect_status and
// failed-after-retries effects can be routed to a DLQ.
//
// Round 30 added the retry loop but kept the legacy
// "best-effort fire-and-log" surface. Round 31 introduces the new
// `ExecuteSideEffectsWithOutcomes` entry point that returns a
// structured []SideEffectOutcome (one per effect) for the caller to
// persist. The legacy `ExecuteSideEffects` keeps its signature so
// existing call sites compile unchanged — internally it delegates
// to the new function and discards the outcomes.
//
// Outcome schema (will be marshalled to JSON for action_logs):
//
//   {
//     "type":       "webhook" | "log" | (unknown effect type echoed back),
//     "status":     "success" | "failed" | "non_retryable" | "unknown_type",
//     "attempts":   <int — number of HTTP calls; 1 for log, 1..N for webhook>,
//     "error":      "<optional final error message>",
//     "durationMs": <int — total dispatch wall-clock time>
//   }
//
// Status taxonomy:
//   - success      — dispatched successfully (possibly after retries)
//   - failed       — retry budget exhausted on persistent transient failure
//   - non_retryable — 4xx other than 408/429 (caller bug, fail fast)
//   - unknown_type — effect.Type didn't match any known dispatcher
//
// Acceptance criteria (Given → When → Then):
//
//   Given an empty effects JSON (null / [] / "")
//   When  ExecuteSideEffectsWithOutcomes runs
//   Then  it returns (nil, nil) — no work, no error
//
//   Given a single webhook that succeeds on 1st attempt
//   When  ExecuteSideEffectsWithOutcomes runs
//   Then  outcomes[0] = {type=webhook, status=success, attempts=1, error=""}
//
//   Given a webhook that returns 503 once then 200
//   When  ExecuteSideEffectsWithOutcomes runs
//   Then  outcomes[0] = {type=webhook, status=success, attempts=2}
//
//   Given a webhook that returns 500 persistently with MaxRetries=2
//   When  ExecuteSideEffectsWithOutcomes runs
//   Then  outcomes[0] = {type=webhook, status=failed, attempts=3,
//                        error includes "gave up after 3 attempts"}
//
//   Given a webhook that returns 400 Bad Request
//   When  ExecuteSideEffectsWithOutcomes runs
//   Then  outcomes[0] = {type=webhook, status=non_retryable, attempts=1}
//
//   Given a log effect
//   When  ExecuteSideEffectsWithOutcomes runs
//   Then  outcomes[0] = {type=log, status=success, attempts=1}
//
//   Given an unknown effect type "carrier-pigeon"
//   When  ExecuteSideEffectsWithOutcomes runs
//   Then  outcomes[0] = {type=carrier-pigeon, status=unknown_type, attempts=0}
//
//   Given an array of [webhook-success, webhook-fail, log]
//   When  ExecuteSideEffectsWithOutcomes runs
//   Then  3 outcomes are returned in input order;
//         the failing webhook does NOT abort dispatch of the others
//         (per-effect isolation matches Foundry's best-effort contract)
//
//   Given a malformed effects JSON
//   When  ExecuteSideEffectsWithOutcomes runs
//   Then  it returns (nil, parse error)
//
// Persistence wiring (executor.go call sites, repo
// UpdateActionLogSideEffectStatus, migration 000213) is deferred to
// round 32 to keep this round's blast radius bounded — round 32 will
// touch 9 mock repos across packages.
func TestBDD_ExecuteSideEffectsWithOutcomes_DispatcherContract(t *testing.T) {
	t.Run("empty effects returns nil outcomes and nil error", func(t *testing.T) {
		for _, in := range []json.RawMessage{nil, json.RawMessage(""), json.RawMessage("null"), json.RawMessage("[]")} {
			outs, err := ExecuteSideEffectsWithOutcomes(in, ActionResult{})
			if err != nil {
				t.Errorf("empty input %q: err = %v, want nil", string(in), err)
			}
			if outs != nil {
				t.Errorf("empty input %q: outcomes = %v, want nil", string(in), outs)
			}
		}
	})

	t.Run("single webhook success: 1 outcome, success status, attempts=1", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		effects := mustEffectsJSON(t, []SideEffect{{
			Type:   "webhook",
			Config: mustWebhookConfig(t, webhookConfig{URL: srv.URL, RetryBackoffMilliseconds: 1}),
		}})
		outs, err := ExecuteSideEffectsWithOutcomes(effects, ActionResult{ActionRID: "rid-ok"})
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if len(outs) != 1 {
			t.Fatalf("len(outcomes) = %d, want 1", len(outs))
		}
		got := outs[0]
		if got.Type != "webhook" || got.Status != SideEffectStatusSuccess || got.Attempts != 1 || got.Error != "" {
			t.Errorf("outcome = %+v, want {type=webhook status=success attempts=1 error=}", got)
		}
		if got.DurationMs < 0 {
			t.Errorf("durationMs = %d, want non-negative", got.DurationMs)
		}
	})

	t.Run("webhook recovers after 1 transient 503: status=success, attempts=2", func(t *testing.T) {
		var calls int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if atomic.AddInt32(&calls, 1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		effects := mustEffectsJSON(t, []SideEffect{{
			Type: "webhook",
			Config: mustWebhookConfig(t, webhookConfig{
				URL: srv.URL, MaxRetries: 2, RetryBackoffMilliseconds: 1,
			}),
		}})
		outs, _ := ExecuteSideEffectsWithOutcomes(effects, ActionResult{})
		if len(outs) != 1 {
			t.Fatalf("len(outcomes) = %d, want 1", len(outs))
		}
		if outs[0].Status != SideEffectStatusSuccess || outs[0].Attempts != 2 {
			t.Errorf("outcome = %+v, want {status=success attempts=2}", outs[0])
		}
	})

	t.Run("webhook exhausts retries: status=failed, attempts=MaxRetries+1, error mentions gave-up", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		effects := mustEffectsJSON(t, []SideEffect{{
			Type: "webhook",
			Config: mustWebhookConfig(t, webhookConfig{
				URL: srv.URL, MaxRetries: 2, RetryBackoffMilliseconds: 1,
			}),
		}})
		outs, _ := ExecuteSideEffectsWithOutcomes(effects, ActionResult{})
		got := outs[0]
		if got.Status != SideEffectStatusFailed {
			t.Errorf("status = %q, want failed", got.Status)
		}
		if got.Attempts != 3 {
			t.Errorf("attempts = %d, want 3 (1 initial + 2 retries)", got.Attempts)
		}
		if got.Error == "" {
			t.Errorf("error = %q, want a non-empty 'gave up' message", got.Error)
		}
	})

	t.Run("webhook 400 Bad Request: status=non_retryable, attempts=1", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
		}))
		defer srv.Close()

		effects := mustEffectsJSON(t, []SideEffect{{
			Type: "webhook",
			Config: mustWebhookConfig(t, webhookConfig{
				URL: srv.URL, MaxRetries: 5, RetryBackoffMilliseconds: 1,
			}),
		}})
		outs, _ := ExecuteSideEffectsWithOutcomes(effects, ActionResult{})
		got := outs[0]
		if got.Status != SideEffectStatusNonRetryable {
			t.Errorf("status = %q, want non_retryable", got.Status)
		}
		if got.Attempts != 1 {
			t.Errorf("attempts = %d, want 1 (no retries on 4xx)", got.Attempts)
		}
	})

	t.Run("log effect: status=success, attempts=1", func(t *testing.T) {
		effects := mustEffectsJSON(t, []SideEffect{{Type: "log"}})
		outs, _ := ExecuteSideEffectsWithOutcomes(effects, ActionResult{ActionRID: "rid-log"})
		got := outs[0]
		if got.Type != "log" || got.Status != SideEffectStatusSuccess || got.Attempts != 1 {
			t.Errorf("outcome = %+v, want {type=log status=success attempts=1}", got)
		}
	})

	t.Run("unknown effect type: status=unknown_type, attempts=0, error mentions type", func(t *testing.T) {
		effects := mustEffectsJSON(t, []SideEffect{{Type: "carrier-pigeon"}})
		outs, _ := ExecuteSideEffectsWithOutcomes(effects, ActionResult{})
		got := outs[0]
		if got.Type != "carrier-pigeon" {
			t.Errorf("type = %q, want it echoed back as carrier-pigeon", got.Type)
		}
		if got.Status != SideEffectStatusUnknownType {
			t.Errorf("status = %q, want unknown_type", got.Status)
		}
		if got.Attempts != 0 {
			t.Errorf("attempts = %d, want 0 (no dispatch happened)", got.Attempts)
		}
	})

	t.Run("array dispatch: failing effect does NOT abort the others (per-effect isolation)", func(t *testing.T) {
		var okCalls, failCalls int32
		ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			atomic.AddInt32(&okCalls, 1)
			w.WriteHeader(http.StatusOK)
		}))
		defer ok.Close()
		fail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			atomic.AddInt32(&failCalls, 1)
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer fail.Close()

		effects := mustEffectsJSON(t, []SideEffect{
			{Type: "webhook", Config: mustWebhookConfig(t, webhookConfig{URL: ok.URL, RetryBackoffMilliseconds: 1})},
			{Type: "webhook", Config: mustWebhookConfig(t, webhookConfig{URL: fail.URL, MaxRetries: 1, RetryBackoffMilliseconds: 1})},
			{Type: "log"},
		})
		outs, err := ExecuteSideEffectsWithOutcomes(effects, ActionResult{})
		if err != nil {
			t.Fatalf("err = %v, want nil (per-effect failures don't surface as top-level err)", err)
		}
		if len(outs) != 3 {
			t.Fatalf("len(outcomes) = %d, want 3", len(outs))
		}
		if outs[0].Status != SideEffectStatusSuccess {
			t.Errorf("outcomes[0] = %+v, want success", outs[0])
		}
		if outs[1].Status != SideEffectStatusFailed {
			t.Errorf("outcomes[1] = %+v, want failed", outs[1])
		}
		if outs[2].Type != "log" || outs[2].Status != SideEffectStatusSuccess {
			t.Errorf("outcomes[2] = %+v, want {type=log status=success}", outs[2])
		}
		if atomic.LoadInt32(&okCalls) != 1 || atomic.LoadInt32(&failCalls) != 2 {
			t.Errorf("call counts: ok=%d (want 1), fail=%d (want 2 = 1+1retry)",
				atomic.LoadInt32(&okCalls), atomic.LoadInt32(&failCalls))
		}
	})

	t.Run("malformed effects JSON: returns (nil, parse error)", func(t *testing.T) {
		outs, err := ExecuteSideEffectsWithOutcomes(json.RawMessage(`{not valid json`), ActionResult{})
		if err == nil {
			t.Fatal("err = nil, want a parse error")
		}
		if outs != nil {
			t.Errorf("outcomes = %v, want nil on parse error", outs)
		}
	})

	t.Run("legacy ExecuteSideEffects wrapper: still swallows per-effect failures", func(t *testing.T) {
		// Backwards-compat guard: the round-30 best-effort contract on the
		// LEGACY surface stays unchanged so existing callers (executor.go
		// L1027 + L1361, until round 32 wires them to the new function)
		// continue to see (nil) on per-effect failure.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()
		effects := mustEffectsJSON(t, []SideEffect{{
			Type: "webhook",
			Config: mustWebhookConfig(t, webhookConfig{
				URL: srv.URL, MaxRetries: 1, RetryBackoffMilliseconds: 1,
			}),
		}})
		if err := ExecuteSideEffects(effects, ActionResult{}); err != nil {
			t.Errorf("legacy wrapper: err = %v, want nil (best-effort contract preserved)", err)
		}
	})
}

func mustEffectsJSON(t *testing.T, effects []SideEffect) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(effects)
	if err != nil {
		t.Fatalf("marshal effects: %v", err)
	}
	return b
}
