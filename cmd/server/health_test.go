package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// healthDeps is the test surface for the readiness handler. It mirrors the
// methods exposed on ServerDeps.
type healthDepsFake struct {
	pgErr     error
	natsErr   error
	bleveErr  error
	funnelErr error
	pgNil     bool // when true, ProbePG returns ErrProbeUnconfigured-style nil signaling no PG
	natsNil   bool
	bleveNil  bool
	funnelNil bool
}

func (h *healthDepsFake) ProbePG(_ context.Context) error {
	if h.pgNil {
		return ErrProbeUnconfigured
	}
	return h.pgErr
}

func (h *healthDepsFake) ProbeNATS() error {
	if h.natsNil {
		return ErrProbeUnconfigured
	}
	return h.natsErr
}

func (h *healthDepsFake) ProbeBleve() error {
	if h.bleveNil {
		return ErrProbeUnconfigured
	}
	return h.bleveErr
}

func (h *healthDepsFake) ProbeFunnel(_ context.Context) error {
	if h.funnelNil {
		return ErrProbeUnconfigured
	}
	return h.funnelErr
}

func TestHealth_Liveness_AlwaysOK(t *testing.T) {
	// Even with broken deps, /health (liveness) returns 200.
	deps := &healthDepsFake{
		pgErr:    errors.New("postgres down"),
		natsErr:  errors.New("nats down"),
		bleveErr: errors.New("bleve down"),
	}

	handler := LivenessHandler()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected liveness 200 even with deps broken, got %d", w.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if body["status"] != "alive" {
		t.Errorf("expected status=alive, got %q", body["status"])
	}
	// deps reference for compiler — used by other tests in this file.
	_ = deps
}

func TestHealthReady_AllHealthy_Returns200(t *testing.T) {
	deps := &healthDepsFake{} // all probes return nil → healthy
	handler := ReadinessHandler(deps)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 when all probes pass, got %d, body=%s", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if body["status"] != "ready" {
		t.Errorf("expected status=ready, got %v", body["status"])
	}
	if _, ok := body["checks"]; !ok {
		t.Error("expected checks object in body")
	}
}

func TestHealthReady_PGFails_Returns503(t *testing.T) {
	deps := &healthDepsFake{pgErr: errors.New("connection refused")}
	handler := ReadinessHandler(deps)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when PG probe fails, got %d", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if body["status"] != "unready" {
		t.Errorf("expected status=unready, got %v", body["status"])
	}
	checks, ok := body["checks"].(map[string]any)
	if !ok {
		t.Fatal("expected checks object in body")
	}
	pgVal, ok := checks["postgres"].(string)
	if !ok || !strings.Contains(pgVal, "connection refused") {
		t.Errorf("expected postgres check to mention error, got %v", checks["postgres"])
	}
}

func TestHealthReady_NATSFails_Returns503(t *testing.T) {
	deps := &healthDepsFake{natsErr: errors.New("disconnected")}
	handler := ReadinessHandler(deps)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when NATS probe fails, got %d", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	checks := body["checks"].(map[string]any)
	natsVal, ok := checks["nats"].(string)
	if !ok || !strings.Contains(natsVal, "disconnected") {
		t.Errorf("expected nats check to mention error, got %v", checks["nats"])
	}
}

func TestHealthReady_BleveFails_Returns503(t *testing.T) {
	deps := &healthDepsFake{bleveErr: errors.New("index corrupt")}
	handler := ReadinessHandler(deps)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when Bleve probe fails, got %d", w.Code)
	}
}

func TestHealthReady_DegradedMode_Returns200(t *testing.T) {
	// All probes report unconfigured → considered healthy (degraded mode).
	deps := &healthDepsFake{pgNil: true, natsNil: true, bleveNil: true, funnelNil: true}
	handler := ReadinessHandler(deps)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 in fully degraded mode, got %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if body["status"] != "ready" {
		t.Errorf("expected status=ready in degraded mode, got %v", body["status"])
	}
	checks := body["checks"].(map[string]any)
	if checks["postgres"] != "skipped" {
		t.Errorf("expected postgres=skipped, got %v", checks["postgres"])
	}
	if checks["nats"] != "skipped" {
		t.Errorf("expected nats=skipped, got %v", checks["nats"])
	}
	if checks["bleve"] != "skipped" {
		t.Errorf("expected bleve=skipped, got %v", checks["bleve"])
	}
	if checks["funnel"] != "skipped" {
		t.Errorf("expected funnel=skipped, got %v", checks["funnel"])
	}
}

func TestHealthReady_PartialDegraded_StillRequiresHealthyConfigured(t *testing.T) {
	// PG configured and healthy; NATS skipped; Bleve configured but failing.
	deps := &healthDepsFake{natsNil: true, bleveErr: errors.New("io error")}
	handler := ReadinessHandler(deps)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when configured-but-failing probe present, got %d", w.Code)
	}
}

func TestHealthReady_NilDeps_Returns200(t *testing.T) {
	// Defensive: nil deps means there is nothing to check, treat as healthy.
	handler := ReadinessHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 when deps is nil, got %d", w.Code)
	}
}

// TestHealth_RoutesMounted verifies that NewFullRouter mounts /health,
// /health/live (alias for liveness), and /health/ready (readiness) and
// that the liveness paths return the alive payload.
func TestHealth_RoutesMounted(t *testing.T) {
	deps := &ServerDeps{}
	// Mark ready so /health/ready can be exercised end-to-end (degraded
	// mode probes return ErrProbeUnconfigured → "skipped" → ready).
	deps.ServerState.MarkReady()
	router := NewFullRouter(deps)

	// /health and /health/live both serve the liveness payload.
	for _, path := range []string{"/health", "/health/live"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", path, w.Code)
		}
		var live map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &live); err != nil {
			t.Fatalf("%s body not JSON: %v body=%s", path, err, w.Body.String())
		}
		if live["status"] != "alive" {
			t.Errorf("%s: expected status=alive, got %q", path, live["status"])
		}
	}

	// /health/ready
	req2 := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	// PRD-V2 §4.6 Gap-O4: a freshly-constructed ServerDeps reports the
	// funnel probe as unconfigured (degraded mode), so the readiness
	// summary stays at status=ready with funnel=skipped — the SPA must
	// not start surfacing a degraded banner just because no Funnel
	// consumer is wired up in the in-memory test router.
	var body2 map[string]any
	if err := json.Unmarshal(w2.Body.Bytes(), &body2); err != nil {
		t.Fatalf("/health/ready body not JSON: %v body=%s", err, w2.Body.String())
	}
	if got := body2["status"]; got != "ready" {
		t.Errorf("/health/ready status: got %v, want ready (funnel skipped should not trip degraded)", got)
	}
	if checks2, ok := body2["checks"].(map[string]any); !ok {
		t.Errorf("/health/ready checks missing")
	} else if checks2["funnel"] != "skipped" {
		t.Errorf("/health/ready checks.funnel: got %v, want skipped", checks2["funnel"])
	}
	if w2.Code != http.StatusOK {
		t.Errorf("/health/ready in degraded test mode should be 200, got %d body=%s",
			w2.Code, w2.Body.String())
	}
}

// TestBDD_HealthReady_FunnelLag_Degraded covers PRD-V2 §4.6 Gap-O4 — when
// the NATS JetStream Funnel consumer falls behind the published stream tip
// by more than the operator-configured threshold, /health/ready must
// transparently surface that lag as a "degraded" signal so the SPA can
// render a banner and oncall can correlate read-side staleness with
// ingest backlog. The probe MUST NOT trip /health/ready to 503 — k8s
// readiness pulls the pod from rotation on 503, and lag is a steady-state
// backpressure signal, not a "this pod is broken" signal. The wire
// contract therefore is:
//
//  1. ok: HTTP 200, status=ready,    checks.funnel=ok
//  2. degraded: HTTP 200, status=degraded, checks.funnel starts with "degraded"
//  3. probe error (e.g. NATS StreamInfo failed): HTTP 503, status=unready
//  4. unconfigured (no Funnel wired): HTTP 200, status=ready, checks.funnel=skipped
//
// Existing PG/NATS/Bleve probe semantics are unchanged; their failure
// continues to trip status=unready and HTTP 503 like before.
func TestBDD_HealthReady_FunnelLag_Degraded(t *testing.T) {
	t.Run("funnel ok keeps status=ready", func(t *testing.T) {
		deps := &healthDepsFake{} // all probes nil → healthy
		handler := ReadinessHandler(deps)

		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 when funnel ok, got %d body=%s", w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body["status"] != "ready" {
			t.Errorf("status: got %v, want ready", body["status"])
		}
		checks := body["checks"].(map[string]any)
		if checks["funnel"] != "ok" {
			t.Errorf("checks.funnel: got %v, want ok", checks["funnel"])
		}
	})

	t.Run("funnel lag degraded keeps HTTP 200 but flips status=degraded", func(t *testing.T) {
		// ErrFunnelLagDegraded is the dedicated sentinel surfaced by
		// ProbeFunnel when the consumer's LastOffset is more than N
		// messages behind the JetStream stream tip. Wrapping with
		// fmt.Errorf carries the human-readable lag count so the SPA can
		// show "Funnel lag 2417 messages behind".
		deps := &healthDepsFake{
			funnelErr: fmt.Errorf("%w: lag=2417 threshold=1000", ErrFunnelLagDegraded),
		}
		handler := ReadinessHandler(deps)

		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 when funnel only degraded (not failed), got %d body=%s",
				w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body["status"] != "degraded" {
			t.Errorf("status: got %v, want degraded", body["status"])
		}
		checks := body["checks"].(map[string]any)
		fv, _ := checks["funnel"].(string)
		if !strings.HasPrefix(fv, "degraded") {
			t.Errorf("checks.funnel: got %q, want prefix 'degraded'", fv)
		}
		// Other healthy probes must still report ok so the operator can
		// distinguish "everything fine except backlog" from a partial
		// outage. Funnel-only degradation must not poison sibling probes.
		if checks["postgres"] != "ok" {
			t.Errorf("checks.postgres: got %v, want ok", checks["postgres"])
		}
	})

	t.Run("funnel probe hard error trips status=unready 503", func(t *testing.T) {
		deps := &healthDepsFake{funnelErr: errors.New("stream info: connection reset")}
		handler := ReadinessHandler(deps)

		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503 on funnel hard error, got %d body=%s", w.Code, w.Body.String())
		}
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if body["status"] != "unready" {
			t.Errorf("status: got %v, want unready", body["status"])
		}
		checks := body["checks"].(map[string]any)
		if got, _ := checks["funnel"].(string); !strings.Contains(got, "stream info") {
			t.Errorf("checks.funnel should mention probe error, got %q", got)
		}
	})

	t.Run("funnel degraded combined with PG failure still degrades to 503", func(t *testing.T) {
		// PG hard failure dominates: 503 wins over a soft degraded
		// signal. The funnel check still reports its lag string so
		// the operator can correlate, but the overall HTTP code is
		// 503 and status=unready.
		deps := &healthDepsFake{
			pgErr:     errors.New("connection refused"),
			funnelErr: fmt.Errorf("%w: lag=99", ErrFunnelLagDegraded),
		}
		handler := ReadinessHandler(deps)

		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503 when PG fails (even with degraded funnel), got %d", w.Code)
		}
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if body["status"] != "unready" {
			t.Errorf("status: got %v, want unready (PG fail dominates degraded)", body["status"])
		}
	})
}
